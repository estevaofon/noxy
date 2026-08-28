package compiler

import (
	"errors"
	"fmt"
	"strings"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
)

// FuncTemplate guarda a declaracao original (nao compilada) de uma funcao
// generica, junto com o modulo onde foi declarada. Monomorfizacao (Tasks
// futuras) usa Decl para instanciar uma copia concreta por combinacao de
// tipos observada em sites de chamada.
type FuncTemplate struct {
	Decl   *ast.FunctionStatement
	Module string
}

// StructTemplate e o analogo de FuncTemplate para structs genericas.
type StructTemplate struct {
	Decl   *ast.StructStatement
	Module string
}

// GenericRegistry acumula os templates genericos vistos durante a compilacao
// de um programa (e, futuramente, de seus modulos). Declaracoes com
// TypeParams nao-vazio nao emitem bytecode: elas apenas populam este
// registro, para serem monomorfizadas sob demanda nos sites de uso.
type GenericRegistry struct {
	Funcs   map[string]*FuncTemplate
	Structs map[string]*StructTemplate
}

// NewGenericRegistry cria um GenericRegistry vazio, pronto para uso.
func NewGenericRegistry() *GenericRegistry {
	return &GenericRegistry{
		Funcs:   make(map[string]*FuncTemplate),
		Structs: make(map[string]*StructTemplate),
	}
}

// SetGenericState injeta um GenericRegistry existente no compilador. Usado
// pelo REPL (que cria um *Compiler novo a cada linha, mas precisa manter os
// templates genericos vistos em linhas anteriores) e por testes que
// precisam inspecionar/preparar o registro antes de compilar.
func (c *Compiler) SetGenericState(reg *GenericRegistry) {
	c.generics = reg
}

// registryOrInit retorna o GenericRegistry do compilador, inicializando-o
// lazily na primeira chamada. Todo acesso a c.generics dentro do pacote deve
// passar por aqui para nao lidar com nil.
func (c *Compiler) registryOrInit() *GenericRegistry {
	if c.generics == nil {
		c.generics = NewGenericRegistry()
	}
	return c.generics
}

// instanceQueue e a fila memoizada das declaracoes monomorfizadas descobertas
// no pass 1 (spec §4/§5).
//
//   - memo guarda os nomes qualificados ja instanciados. A entrada e criada
//     ANTES de clonar/compilar o corpo: e isso que faz recursao e referencia
//     mutua terminarem (a segunda visita encontra a entrada e para).
//   - ordered guarda as declaracoes sinteticas na ordem de CONCLUSAO
//     (pos-ordem): uma instancia so entra na fila depois das instancias que o
//     corpo dela exigiu, entao toda dependencia aparece antes de quem depende
//     dela — inclusive structs antes das funcoes que os usam, de graca.
//
// Tempo de vida: UMA COMPILACAO DE PROGRAMA, nao um compilador. A fila nasce
// na entrada do two-pass e e solta depois do merge (runGenericsPass1). O
// registry de templates, esse sim, pode persistir entre compilacoes (§5: o
// REPL guarda os templates da sessao) — mas memo e ordered nao podem: herdar o
// memo suprimiria a re-instanciacao de uma tupla cuja declaracao nao esta mais
// na lista de statements, e herdar ordered prependaria as instancias do
// programa anterior no programa atual. Re-instanciar por programa e tambem a
// semantica certa para o REPL: cada linha redefine seus globals de instancia,
// idempotentemente (§8).
//
// Dentro de uma compilacao o ponteiro e compartilhado entre o compilador real,
// o compilador descartavel do pass 1, os compiladores de corpo de instancia e
// todos os filhos (NewChild).
type instanceQueue struct {
	memo    map[string]bool
	ordered []ast.Statement
	// structInstances e o memo das instancias de STRUCT — e um mapa para a
	// declaracao, nao um set: no acerto de memo ainda precisamos registrar a
	// instancia em c.structs/c.globals do compilador atual (cada filho tem
	// copias proprias dessas tabelas), e para isso a declaracao tem de estar a
	// mao. Registrar a entrada aqui ANTES de resolver os campos e o que faz
	// auto-referencia (`next: ref Node<T>`) terminar.
	structInstances map[string]*ast.StructStatement
	// structKeys guarda a tupla de cada instancia de struct para o caminho
	// inverso da resolucao (expandInstanceNames): unificar a anotacao generica
	// do template contra um tipo concreto que ja carrega o nome qualificado.
	structKeys map[string]structInstanceKey
	// depth e a profundidade de instanciacao ANINHADA corrente (quantos
	// corpos/campos de instancia estao abertos na pilha). O memo faz recursao
	// COMUM terminar — a segunda visita reencontra a mesma tupla e para —, mas
	// nao faz nada por RECURSAO POLIMORFICA, onde cada nivel pede uma tupla
	// NOVA (`func f<T>(x: T)` com `let arr: T[] = [x]` e `f(arr, ...)`
	// instancia f<int>, f<int[]>, f<int[][]>, ...). O memo nunca acerta e o
	// compilador nao termina. maxInstantiationDepth corta com o erro do §9.
	depth int
}

// maxInstantiationDepth e o teto de instanciacoes generica-dentro-de-generica
// aninhadas. Codigo real fica em um digito (uma instancia que usa outra que
// usa outra); 64 e folgado o bastante para nunca aparecer num programa
// legitimo e apertado o bastante para o erro chegar em milissegundos.
const maxInstantiationDepth = 64

// instantiationDepthError e a mensagem do §9 para o teto acima. Chega ao
// usuario embrulhada na cadeia de instanciacao (instantiationChainError), que
// mostra exatamente a torre de tuplas divergentes.
func instantiationDepthError(line int) error {
	return fmt.Errorf(
		"[line %d] profundidade máxima de instanciação genérica excedida (%d) — recursão polimórfica?",
		line, maxInstantiationDepth,
	)
}

func newInstanceQueue() *instanceQueue {
	return &instanceQueue{
		memo:            make(map[string]bool),
		structInstances: make(map[string]*ast.StructStatement),
		structKeys:      make(map[string]structInstanceKey),
	}
}

// instancesOrInit devolve a fila da compilacao corrente. Dentro do pass 1 ela
// nunca e nil (runGenericsPass1 a instala antes de criar qualquer compilador
// de pass 1, e newPass1Compiler/NewChild propagam o ponteiro); a lazy-init
// aqui e so uma rede de seguranca para nao lidar com nil.
func (c *Compiler) instancesOrInit() *instanceQueue {
	if c.instances == nil {
		c.instances = newInstanceQueue()
	}
	return c.instances
}

// predeclareGenericTemplates registra os templates genericos do topo do
// programa ANTES de qualquer statement ser compilado. Sem isso, um site de
// chamada que aparece antes da declaracao do template (referencia adiante,
// que predeclareGlobalBindings ja suporta para funcoes comuns) escaparia da
// interceptacao e cairia no caminho normal com o tipo cru do template.
//
// Idempotente: o case de FunctionStatement/StructStatement registra de novo
// com o mesmo ponteiro quando a declaracao e efetivamente compilada.
// Declaracoes genericas ANINHADAS nao passam por aqui (a varredura e so no
// topo) e continuam sendo rejeitadas pelos cases do Compile.
func (c *Compiler) predeclareGenericTemplates(statements []ast.Statement) error {
	for _, statement := range statements {
		switch declaration := statement.(type) {
		case *ast.FunctionStatement:
			if len(declaration.TypeParams) > 0 {
				if err := c.registerFuncTemplate(declaration.Name, &FuncTemplate{Decl: declaration, Module: c.moduleName}, declaration.Token.Line); err != nil {
					return err
				}
			}
		case *ast.StructStatement:
			if len(declaration.TypeParams) > 0 {
				if err := c.registerStructTemplate(declaration.Name, &StructTemplate{Decl: declaration, Module: c.moduleName}, declaration.Token.Line); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// moduleQualifier normaliza o nome de um modulo para uso como qualificador
// de instanceName (o campo Module de FuncTemplate/StructTemplate). Hoje e
// identidade — existe como um unico ponto de acoplamento, para que uma
// normalizacao futura (aliases, caminhos de submodulo) nao precise caçar
// cada site que hoje escreve `n.Module`/`declaration.Module` cru.
func moduleQualifier(module string) string {
	return module
}

// registerFuncTemplate insere tpl no registro sob name, recusando a colisao
// de homonimo entre familias documentada na revisao da Task 9: instanceName
// (generics_substitute.go) qualifica so por modulo+base+args, sem marcar se
// a base veio de `func` ou de `struct` — um `func Foo<T>` e um `struct
// Foo<T>` com o MESMO nome (no mesmo modulo, ou um local e outro importado)
// produziriam o MESMO nome qualificado ao serem instanciados com a mesma
// tupla (`main::Foo<int>`), e um sobrescreveria o c.globals/c.structs do
// outro em silencio. Fechado aqui, na fronteira de registro (chamada tanto
// pelas declaracoes locais quanto pelo import de templates), com um erro de
// compilacao claro em vez de uma colisao confusa em runtime.
func (c *Compiler) registerFuncTemplate(name string, tpl *FuncTemplate, line int) error {
	if _, collides := c.registryOrInit().Structs[name]; collides {
		return fmt.Errorf(
			"[line %d] '%s' já nomeia um struct genérico — não pode também nomear uma função genérica (mesmo nome colidiria na instanciação)",
			line, name,
		)
	}
	c.registryOrInit().Funcs[name] = tpl
	return nil
}

// registerStructTemplate e o espelho de registerFuncTemplate para structs.
func (c *Compiler) registerStructTemplate(name string, tpl *StructTemplate, line int) error {
	if _, collides := c.registryOrInit().Funcs[name]; collides {
		return fmt.Errorf(
			"[line %d] '%s' já nomeia uma função genérica — não pode também nomear um struct genérico (mesmo nome colidiria na instanciação)",
			line, name,
		)
	}
	c.registryOrInit().Structs[name] = tpl
	return nil
}

// hasGenerics decide se o Program precisa do two-pass. A varredura das
// declaracoes ja aconteceu em predeclareGenericTemplates, entao a pergunta se
// reduz a "o registro tem algum template?" — o que tambem cobre o caso do
// REPL/modulos, onde o registro chega populado de fora (SetGenericState) sem
// que a linha atual declare nada generico. Programa sem genericos responde
// false e pula o pass 1 inteiro (custo exatamente zero, §5).
//
// Leitura nil-safe, nao registryOrInit: este gate roda em TODO Compile de
// Program (programa principal, cada modulo, cada linha do REPL) e nao pode
// alocar um GenericRegistry (dois mapas) so para descobrir que ele esta
// vazio — mesmo cuidado que rejectBareGenericTemplateIdentifier
// (generics_target.go) ja tomava no caminho por identificador.
func (c *Compiler) hasGenerics() bool {
	if c.generics == nil {
		return false
	}
	return len(c.generics.Funcs) > 0 || len(c.generics.Structs) > 0
}

// runGenericsPass1 executa o pass 1 (§5): compila o programa inteiro num
// compilador descartavel, jogando fora o bytecode e ficando com dois efeitos
// colaterais — o AST compartilhado reescrito para os nomes qualificados das
// instancias, e a fila de declaracoes monomorfizadas. As instancias sao
// prependadas ao Program para que o pass 2 as compile pelo caminho 100%
// normal.
//
// Erros do pass 1 sao propagados (nao engolidos): sao exatamente os erros de
// inferencia e de corpo de instancia do §9.
func (c *Compiler) runGenericsPass1(program *ast.Program) error {
	// Fila NOVA por compilacao de programa, e solta no fim (inclusive em erro):
	// nem o memo nem as declaracoes sinteticas podem atravessar dois
	// Compile(*ast.Program) no mesmo compilador — ver o comentario de
	// instanceQueue.
	queue := newInstanceQueue()
	c.instances = queue
	defer func() { c.instances = nil }()

	scratch := c.newPass1Compiler()
	if _, _, err := scratch.Compile(program); err != nil {
		return err
	}
	if len(queue.ordered) == 0 {
		return nil
	}
	merged := make([]ast.Statement, 0, len(queue.ordered)+len(program.Statements))
	merged = append(merged, queue.ordered...)
	merged = append(merged, program.Statements...)
	program.Statements = merged
	return nil
}

// newPass1Compiler cria um compilador descartavel que COMPARTILHA o registro
// de templates e a fila de instancias com c, mas trabalha sobre COPIAS de
// globals/structs — o pass 1 nao pode contaminar as tabelas que o pass 2 vai
// construir do zero. pass1=true e o guard de recursao: o Compile(*ast.Program)
// desse compilador nao reentra no two-pass.
func (c *Compiler) newPass1Compiler() *Compiler {
	globalsCopy := make(map[string]ast.NoxyType, len(c.globals))
	for name, bindingType := range c.globals {
		globalsCopy[name] = bindingType
	}
	structsCopy := make(map[string]*ast.StructStatement, len(c.structs))
	for name, definition := range c.structs {
		structsCopy[name] = definition
	}
	namespaceImportsCopy := make(map[string]string, len(c.namespaceImports))
	for name, module := range c.namespaceImports {
		namespaceImportsCopy[name] = module
	}
	scratch := NewWithStateAndRoot(globalsCopy, structsCopy, c.FileName, c.moduleRoot)
	scratch.moduleName = c.moduleName
	scratch.knownGlobals = c.knownGlobals
	scratch.generics = c.registryOrInit()
	scratch.instances = c.instancesOrInit()
	// discoveryState() (nao c.moduleDiscovery cru): o pass 1 tem de
	// COMPARTILHAR o cache de modulos com o pass 2, senao cada passada
	// recarrega do zero todo modulo importado — metade da amplificacao de
	// startup medida em `use http select *`.
	scratch.moduleDiscovery = c.discoveryState()
	scratch.namespaceImports = namespaceImportsCopy
	scratch.namespaceOrder = append([]string(nil), c.namespaceOrder...)
	scratch.pass1 = true
	return scratch
}

// compileGenericCallSite e o hook enumerado do §4 em compileCallExpression:
// callee e o nome de um template generico, entao a chamada NAO segue o
// caminho normal antes de ser monomorfizada. Infere a tupla de tipos a partir
// dos argumentos (compilados fora de ordem, em chunk descartavel), garante a
// instancia e REESCREVE callee.Value para o nome qualificado. Depois disso o
// caminho normal do compileCallExpression resolve um global comum.
func (c *Compiler) compileGenericCallSite(call *ast.CallExpression, callee *ast.Identifier, tpl *FuncTemplate) error {
	base := tpl.Decl.Name
	line := c.currentLine
	if !c.pass1 {
		// Defensivo: no pass 2 os nomes ja foram reescritos e nenhum nome de
		// template deveria alcancar um call site. Se alcancou, ha um caminho
		// de compilacao que nao passou pelo pass 1 — erro claro em vez de
		// bytecode com o tipo cru do template.
		return fmt.Errorf(
			"[line %d] chamada ao template genérico '%s' chegou ao pass 2 sem monomorfização — bug do compilador de genéricos",
			line, base,
		)
	}

	// O hint do `let` vale para ESTE call site e so para ele: consumimos
	// (e limpamos) antes de compilar argumentos, que podem conter outras
	// chamadas genericas aninhadas.
	hint := c.genericReturnHint
	c.genericReturnHint = nil

	params := tpl.Decl.Parameters
	if len(call.Arguments) != len(params) {
		return fmt.Errorf(
			"[line %d] function '%s' expects %d arguments, got %d",
			line, base, len(params), len(call.Arguments),
		)
	}

	bindings := make(map[string]ast.NoxyType, len(tpl.Decl.TypeParams))

	// §3, unificacao bidirecional (posicao 5): um argumento que e um
	// identificador NU nomeando OUTRO template de funcao (`aplica(nums,
	// identity)`) nao pode ser compilado pelo caminho normal — o template nao
	// existe em runtime, entao typeOfDiscardedExpression leria um global nunca
	// definido. Esses argumentos sao identificados aqui (pre-passada rasa,
	// so bareFunctionTemplateArgument) e desviados via skip da unificacao
	// posicional comum abaixo, que resolve os parametros de tipo do CALLER
	// pelos argumentos nao-genericos primeiro (ordem exigida pela spec) — a
	// segunda passada, mais abaixo, cuida deles de verdade.
	var templateArgs []int
	skipTemplateArg := make(map[int]bool, len(call.Arguments))
	for index, argument := range call.Arguments {
		if _, _, isTemplateArg := c.bareFunctionTemplateArgument(argument); isTemplateArg {
			templateArgs = append(templateArgs, index)
			skipTemplateArg[index] = true
		}
	}

	// nullOnlyParams (§9 "inferência só com null") e argIndexOf (§9
	// "conflito de unificação" com atribuição por argumento) sao povoados por
	// unifyPositionalArguments — maquinaria COMPARTILHADA com
	// compileGenericConstructorSite (generics_structs.go), documentada la.
	nullOnlyParams := make(map[string]bool)
	argIndexOf := make(map[string]int, len(tpl.Decl.TypeParams))
	if err := c.unifyPositionalArguments(
		line, base, call.Arguments,
		func(index int) ast.NoxyType { return params[index].Type },
		func(index int) bool { return skipTemplateArg[index] },
		tpl.Decl.TypeParams, bindings, nullOnlyParams, argIndexOf,
	); err != nil {
		return err
	}

	// Segunda passada: agora que os argumentos comuns ancoraram o que podiam,
	// cada argumento-template tem seu parametro esperado (ja parcialmente
	// concreto) unificado CONTRA A ASSINATURA DO PROPRIO TEMPLATE do
	// argumento, com bindings propagando nos dois sentidos (§3). Bindings do
	// argumento vivem num mapa PROPRIO por ocorrencia — duas passagens do
	// mesmo template neste call site podem instanciar tuplas diferentes.
	for _, index := range templateArgs {
		argIdent, argTpl, _ := c.bareFunctionTemplateArgument(call.Arguments[index])
		expected := substituteType(params[index].Type, bindings)
		expectedFn, ok := expected.(*ast.FunctionType)
		if !ok {
			return noConcreteTargetError(line, argIdent.Value)
		}
		templateSig := newFunctionType(argTpl.Decl.Parameters, argTpl.Decl.ReturnType)
		argBindings := make(map[string]ast.NoxyType, len(argTpl.Decl.TypeParams))
		if err := c.unifyBidirectionalAnnotated(expectedFn, templateSig, bindings, argBindings); err != nil {
			// Mesma atribuicao por argumento do §9 que unifyPositionalArguments
			// aplica no caminho comum (I5): um conflito estruturado sabe QUAL
			// parametro divergiu, e argIndexOf sabe qual argumento o bindou
			// primeiro. Sem isto, este caminho so sabia dizer "argumento N".
			var conflict *conflictError
			if errors.As(err, &conflict) {
				if existingIndex, known := argIndexOf[conflict.Param]; known {
					return fmt.Errorf(
						"[line %d] %s inferido como %s (argumento %d) e %s (argumento %d)",
						line, conflict.Param,
						conflict.Existing.String(), existingIndex,
						conflict.New.String(), index+1,
					)
				}
			}
			return fmt.Errorf("[line %d] argumento %d de '%s': %v", line, index+1, base, err)
		}
		instName, _, err := c.ensureFunctionInstance(argTpl, argBindings, line)
		if err != nil {
			return err
		}
		argIdent.Value = instName
	}

	// Hint do `let`: resolve o T que so aparece no retorno (`let xs: int[] =
	// vazio()`). Depois dos argumentos, por decisao da spec §7 — o argumento e
	// a ancora primaria, o alvo do target-typing e complemento.
	if hint != nil && containsTypeParam(tpl.Decl.ReturnType) {
		if err := c.unifyAnnotation(tpl.Decl.ReturnType, hint, bindings); err != nil {
			return fmt.Errorf("[line %d] retorno de '%s': %v", line, base, err)
		}
	}

	// §9 "inferência só com null": intercepta ANTES de ensureFunctionInstance
	// (cuja mensagem para parametro sem binding e a generica "não foi
	// possível inferir T em 'f'") sempre que o binding que falta e exatamente
	// um que so viu `null` como candidato — dá a mensagem dedicada do
	// catalogo. missingTypeParamNullError usa a MESMA ordem de
	// tpl.Decl.TypeParams que ensureFunctionInstance, entao o primeiro
	// parametro sem binding e sempre o mesmo nos dois caminhos.
	if err := missingTypeParamNullError(line, tpl.Decl.TypeParams, bindings, nullOnlyParams); err != nil {
		return err
	}

	name, _, err := c.ensureFunctionInstance(tpl, bindings, line)
	if err != nil {
		return err
	}
	callee.Value = name
	c.setLine(line)
	return nil
}

// unifyPositionalArguments e a maquinaria COMPARTILHADA entre os dois call
// sites que inferem uma tupla de tipos a partir de argumentos posicionais
// contra tipos esperados que podem conter TypeParamType —
// compileGenericCallSite (chamada de funcao generica, acima) e
// compileGenericConstructorSite (construtor de struct generico,
// generics_structs.go). Por argumento:
//
//   - pula quando skip(index) e verdadeiro (compileGenericCallSite usa isto
//     para desviar os argumentos-template, tratados numa segunda passada
//     bidirecional a parte; compileGenericConstructorSite nao tem
//     equivalente e passa skip nil);
//   - pula quando expectedType(index) nao contem parametro de tipo nenhum
//     (nada a unificar; nao ha razao para compilar o argumento so para
//     descobrir um tipo que nao sera usado);
//   - quando o tipo esperado e um `T` NU (nao aninhado — `x: T`, nao
//     `x: T[]`) e o argumento e `null`, marca nullOnlyParams[T] em vez de
//     unificar (§9 "inferência só com null" — unify trataria null como
//     "não contribui binding, não falha", indistinguível de um argumento
//     sem informação nenhuma; a marca e o que permite ao chamador, depois
//     do loop, escolher a mensagem dedicada do catálogo via
//     missingTypeParamNullError em vez da genérica "não foi possível
//     inferir T em 'f'");
//   - senao, unifica de verdade contra bindings; num conflito (unify devolve
//     um *conflictError encadeado — errors.As), compoe a mensagem do §9 com
//     atribuição por argumento ("T inferido como int (argumento 1) e string
//     (argumento 2)") usando argIndexOf; sem atribuição conhecida (conflito
//     vindo de uma posição sem argumento registrado — não deveria acontecer
//     no uso atual, mas e defensivo), cai para a mensagem antiga com o
//     `%v` cru;
//   - registra em argIndexOf, para cada parametro de tipo AINDA sem entrada,
//     o indice (1-based) deste argumento quando ele acabou de bindar esse
//     parametro pela primeira vez (um unico argumento composto — `map[K,V]`
//     — pode bindar mais de um de uma vez).
//
// bindings, nullOnlyParams e argIndexOf sao do CHAMADOR (mutados aqui,
// consultados depois — inclusive por missingTypeParamNullError e pelas
// passadas seguintes do chamador, como a segunda passada bidirecional de
// compileGenericCallSite). typeParams e a lista de nomes de parametro de
// tipo do template (tpl.Decl.TypeParams em ambos os chamadores) na ordem de
// declaração — a mesma ordem que ensure*Instance usa, o que faz
// missingTypeParamNullError e a checagem de aridade final apontarem sempre
// para o mesmo parametro quando ha mais de um em aberto.
func (c *Compiler) unifyPositionalArguments(
	line int,
	base string,
	arguments []ast.Expression,
	expectedType func(index int) ast.NoxyType,
	skip func(index int) bool,
	typeParams []string,
	bindings map[string]ast.NoxyType,
	nullOnlyParams map[string]bool,
	argIndexOf map[string]int,
) error {
	for index, argument := range arguments {
		if skip != nil && skip(index) {
			continue
		}
		expected := expectedType(index)
		if !containsTypeParam(expected) {
			continue
		}
		actual, err := c.typeOfDiscardedExpression(argument)
		if err != nil {
			return err
		}
		if typeParam, isBare := expected.(*ast.TypeParamType); isBare && isNullType(actual) {
			if _, bound := bindings[typeParam.Name]; !bound {
				nullOnlyParams[typeParam.Name] = true
			}
			continue
		}
		if err := c.unifyAnnotation(expected, actual, bindings); err != nil {
			var conflict *conflictError
			if errors.As(err, &conflict) {
				if existingIndex, known := argIndexOf[conflict.Param]; known {
					return fmt.Errorf(
						"[line %d] %s inferido como %s (argumento %d) e %s (argumento %d)",
						line, conflict.Param,
						conflict.Existing.String(), existingIndex,
						conflict.New.String(), index+1,
					)
				}
			}
			return fmt.Errorf("[line %d] argumento %d de '%s': %v", line, index+1, base, err)
		}
		for _, typeParam := range typeParams {
			if _, recorded := argIndexOf[typeParam]; recorded {
				continue
			}
			if _, bound := bindings[typeParam]; bound {
				argIndexOf[typeParam] = index + 1
			}
		}
	}
	return nil
}

// missingTypeParamNullError e o gate COMPARTILHADO do §9 "inferência só com
// null": percorre typeParams na ordem de declaração e, no primeiro parametro
// AINDA sem binding, devolve a mensagem dedicada quando nullOnlyParams o
// marcou — nil quando ou todos os parametros ja tem binding, ou o primeiro
// sem binding nao foi marcado (o chamador delega para a mensagem generica de
// ensure*Instance, que itera typeParams na MESMA ordem e portanto encontra o
// mesmo parametro).
func missingTypeParamNullError(line int, typeParams []string, bindings map[string]ast.NoxyType, nullOnlyParams map[string]bool) error {
	for _, typeParam := range typeParams {
		if _, bound := bindings[typeParam]; bound {
			continue
		}
		if nullOnlyParams[typeParam] {
			return nullOnlyInferenceError(line, typeParam)
		}
		break
	}
	return nil
}

// ensureFunctionInstance devolve (criando se preciso) a instancia
// monomorfizada de tpl para bindings. Ordem obrigatoria da §4:
// nome -> memo -> tipo em globals -> clone -> compila o clone em modo pass-1
// -> enfileira. Registrar o memo e o tipo ANTES de compilar o corpo e o que
// faz a recursao terminar: a chamada recursiva dentro do clone encontra a
// entrada no memo e o tipo no globals, e para.
func (c *Compiler) ensureFunctionInstance(tpl *FuncTemplate, bindings map[string]ast.NoxyType, line int) (string, *ast.FunctionType, error) {
	base := tpl.Decl.Name

	// A tupla do nome segue a ordem de DECLARACAO dos parametros de tipo
	// (`<A, B>`), nao a ordem em que a unificacao os descobriu — e o que faz
	// duas chamadas com a mesma tupla caírem no mesmo nome.
	arguments := make([]ast.NoxyType, 0, len(tpl.Decl.TypeParams))
	for _, typeParam := range tpl.Decl.TypeParams {
		bound, ok := bindings[typeParam]
		if !ok {
			return "", nil, fmt.Errorf(
				"[line %d] não foi possível inferir %s em '%s' — anote o tipo",
				line, typeParam, base,
			)
		}
		arguments = append(arguments, bound)
	}
	name := instanceName(tpl.Module, base, arguments)

	// A assinatura da instancia entra em globals RESOLVIDA (§4, terceira familia
	// de hooks): um parametro `Caixa<T>` vira `main::Caixa<int>`, nao
	// `Caixa<int>`. Sem isso o call site reescrito leria um tipo cuja identidade
	// nominal nao existe em c.structs, e a checagem de tipos do pass 1
	// divergiria da do pass 2 (que le a assinatura ja reescrita do clone).
	// Resolver aqui tambem garante que a instancia de struct entre na fila ANTES
	// da instancia de funcao que a menciona.
	parameterTypes := make([]ast.NoxyType, len(tpl.Decl.Parameters))
	for index, parameter := range tpl.Decl.Parameters {
		resolved, err := c.resolveAnnotation(substituteType(parameter.Type, bindings), line)
		if err != nil {
			return "", nil, err
		}
		parameterTypes[index] = resolved
	}
	resolvedReturn, err := c.resolveAnnotation(substituteType(tpl.Decl.ReturnType, bindings), line)
	if err != nil {
		return "", nil, err
	}
	instanceType := &ast.FunctionType{
		Params: parameterTypes,
		Return: resolvedReturn,
	}

	// Sempre registra o tipo no compilador ATUAL, inclusive no acerto de memo:
	// cada filho tem sua propria copia de globals, e quem chama a instancia
	// precisa enxergar a assinatura para o caminho normal resolver o global.
	c.globals[name] = instanceType

	queue := c.instancesOrInit()
	if queue.memo[name] {
		return name, instanceType, nil
	}
	queue.memo[name] = true

	// §8, regra de escopo de definicao: so entra em jogo para template
	// IMPORTADO (tpl.Module != c.moduleName — o importador nao e quem
	// define). Template local tem importador==definidor por construcao,
	// entao a regra e trivialmente satisfeita e este gate e um no-op para
	// todo o corpus de genericos anterior a Task 12.
	if err := c.validateImportedTemplateScope(tpl, arguments, line); err != nil {
		return "", nil, err
	}

	// Teto de aninhamento (§9): compilar o corpo e o passo que pode pedir
	// OUTRA instancia. Sem o teto, recursao polimorfica (tupla nova a cada
	// nivel, memo nunca acerta) trava o compilador em vez de errar.
	if queue.depth >= maxInstantiationDepth {
		return "", nil, instantiationDepthError(line)
	}

	instance := substituteFunction(tpl, bindings, name)
	queue.depth++
	err = c.compileInstanceBody(instance)
	queue.depth--
	if err != nil {
		return "", nil, instantiationChainError(displayInstanceName(base, arguments), line, err)
	}
	queue.ordered = append(queue.ordered, instance)
	return name, instanceType, nil
}

// compileInstanceBody compila o clone monomorfizado imediatamente, ainda em
// modo pass-1 (§4): e assim que a cascata generico->generico acontece antes do
// pass 2 comecar. O bytecode e descartado — o que interessa sao os efeitos
// colaterais: as chamadas genericas dentro do corpo ficam reescritas no clone
// e as instancias que elas exigem entram na fila.
//
// O corpo compila num compilador de TOPO (nao um filho de c): o clone e uma
// declaracao top-level e nao pode enxergar os locais do escopo onde o site de
// chamada estava — um filho de c resolveria esses locais como upvalues e
// inferiria tipos errados.
func (c *Compiler) compileInstanceBody(instance *ast.FunctionStatement) error {
	instanceCompiler := c.newPass1Compiler()
	// programBindings carrega as declaracoes do topo do programa (funcoes,
	// lets, structs) que applyProgramBindings injeta durante a compilacao de
	// um corpo de funcao — sem isso, o corpo da instancia nao enxergaria as
	// funcoes comuns do programa.
	instanceCompiler.programBindings = c.programBindings
	_, _, err := instanceCompiler.Compile(instance)
	return err
}

// typeOfDiscardedExpression compila expr num chunk descartavel so para
// descobrir seu tipo estatico, restaurando chunk e linha corrente — mesmo
// padrao de emissao especulativa de tryCompileFusedCondition (compiler.go),
// mas com um chunk inteiro no lugar de TruncateTo, porque aqui a expressao e
// compilada FORA DE ORDEM (antes do callee) e o caminho normal a compila de
// novo logo depois.
//
// Nasceu para o pass 1 (bytecode inteiro descartado); desde a inferencia de
// `let` (issue #41, inferGlobalLetTypes) roda tambem no compilador real, em
// qualquer passe. O invariante que sustenta os dois usos: a emissao vai
// INTEIRA para o chunk descartavel (constantes inclusive), addUpvalue
// deduplica, c.warn deduplica — nenhum efeito colateral alcanca o bytecode
// final (guardado por TestInferredLetEmitsSameBytecodeAsAnnotated).
func (c *Compiler) typeOfDiscardedExpression(expr ast.Expression) (ast.NoxyType, error) {
	savedChunk := c.currentChunk
	savedLine := c.currentLine
	throwaway := chunk.New()
	throwaway.FileName = savedChunk.FileName
	c.currentChunk = throwaway
	_, exprType, err := c.Compile(expr)
	c.currentChunk = savedChunk
	c.currentLine = savedLine
	if err != nil {
		return nil, err
	}
	return exprType, nil
}

// setGenericReturnHint arma o hint de target-typing do `let` (§7, uso 1) para
// o proximo call site generico: `let xs: int[] = vazio()` resolve o T que so
// aparece no retorno. Fica armado apenas quando o valor e, de fato, uma
// chamada direta a um template — assim nenhuma outra expressao consome o hint
// por engano.
//
// O mesmo hint serve aos dois tipos de site, com leituras diferentes: o site de
// FUNCAO unifica a anotacao contra o tipo de retorno do template; o site de
// CONSTRUTOR de struct le nela a tupla da instancia (applyStructHintBindings).
// Nenhum dos dois pode consumir o hint do outro — cada um limpa o campo na
// entrada, e o hint so e armado quando o valor do `let` e uma chamada direta ao
// template daquele mesmo tipo.
func (c *Compiler) setGenericReturnHint(target ast.NoxyType, valueExpr ast.Expression) {
	c.genericReturnHint = nil
	if !c.pass1 || target == nil {
		return
	}
	call, ok := valueExpr.(*ast.CallExpression)
	if !ok {
		return
	}
	callee, ok := call.Function.(*ast.Identifier)
	if !ok {
		return
	}
	registry := c.registryOrInit()
	_, isFunctionTemplate := registry.Funcs[callee.Value]
	_, isStructTemplate := registry.Structs[callee.Value]
	if !isFunctionTemplate && !isStructTemplate {
		return
	}
	// Mesma regra de sombreamento da interceptacao de call site: um local ou
	// upvalue com o nome do template vence, a chamada compila pelo caminho
	// normal e NAO consome o hint — que entao vazaria para a primeira chamada
	// generica aninhada nos argumentos, ancorando o T dela por uma anotacao
	// que nao e do site dela.
	if c.isShadowedByLocal(callee.Value) {
		return
	}
	c.genericReturnHint = target
}

// nullOnlyInferenceError e a mensagem dedicada do §9 para quando `null` e a
// UNICA razao de um parametro de tipo continuar sem binding (`identity(null)`
// sem outra ancora) — distinta da mensagem generica "não foi possível
// inferir T em 'f'" (usada quando T simplesmente nunca teve candidato
// nenhum, ex.: `vazio()` com T so no retorno).
func nullOnlyInferenceError(line int, typeParam string) error {
	return fmt.Errorf(
		"[line %d] não foi possível inferir %s de null — anote o tipo",
		line, typeParam,
	)
}

// displayInstanceName produz o nome de exibicao do §9 — `soma<Ponto>`, SEM o
// qualificador de modulo, que e identidade interna e nao interessa ao usuario.
func displayInstanceName(base string, arguments []ast.NoxyType) string {
	parts := make([]string, len(arguments))
	for index, argument := range arguments {
		parts[index] = argument.String()
	}
	return base + "<" + strings.Join(parts, ",") + ">"
}

// strippedLineError e um erro cuja mensagem perdeu o prefixo "[line N]" mas
// que continua encadeado ao original para errors.Is/As.
type strippedLineError struct {
	message string
	cause   error
}

func (e *strippedLineError) Error() string { return e.message }
func (e *strippedLineError) Unwrap() error { return e.cause }

// instantiationChainError monta a cadeia de instanciacao do §9:
//
//	[line 12] em soma<Ponto> (instanciado na linha 40): operador '+' não definido para Ponto
//
// A linha do prefixo e a linha do erro DENTRO do corpo do template (o
// compilador ja a carrega no proprio texto do erro), e a segunda linha e o
// site de chamada que pediu a instancia. O prefixo duplicado do erro interno e
// removido do texto — o encadeamento com %w e preservado via Unwrap.
func instantiationChainError(display string, instantiatedAt int, err error) error {
	line, rest, ok := splitLinePrefix(err.Error())
	if !ok {
		return fmt.Errorf("[line %d] em %s (instanciado na linha %d): %w", instantiatedAt, display, instantiatedAt, err)
	}
	stripped := &strippedLineError{message: rest, cause: err}
	return fmt.Errorf("[line %d] em %s (instanciado na linha %d): %w", line, display, instantiatedAt, stripped)
}

// splitLinePrefix separa o prefixo "[line N] " que todo erro do compilador
// carrega, devolvendo N e o resto da mensagem.
func splitLinePrefix(message string) (int, string, bool) {
	if !strings.HasPrefix(message, "[line ") {
		return 0, "", false
	}
	end := strings.Index(message, "]")
	if end < 0 {
		return 0, "", false
	}
	var line int
	if _, err := fmt.Sscanf(message[:end+1], "[line %d]", &line); err != nil {
		return 0, "", false
	}
	return line, strings.TrimLeft(message[end+1:], " "), true
}

// validateImportedTemplateScope implementa a regra de escopo de definicao do
// §8, EM DUAS PARTES, para um template IMPORTADO de outro modulo
// (tpl.Module != c.moduleName — chamada so a partir desse caso em
// ensureFunctionInstance; template local nunca chega aqui):
//
//   - (a) todo identificador livre do corpo ORIGINAL do template (antes da
//     substituicao — os TypeParams ainda sao TypeParamType, irrelevante
//     aqui, so nomes de VALOR importam) tem de resolver no top level do
//     modulo que o declara (discoverModuleExports do modulo definidor,
//     que enxerga suas proprias funcoes/lets/structs/templates
//     independente do que O IMPORTADOR selecionou) — ou ser um builtin;
//   - (b) quando resolve la, o TIPO DECLARADO visivel no IMPORTADOR
//     (c.globals, ja populado pelo predeclare tipado — Task 12) tem de
//     ser IDENTICO (String()) ao tipo declarado no modulo definidor —
//     senao um homonimo do importador seria capturado em silencio no
//     lugar do binding certo, exatamente o risco que o §8 documenta.
//
// Heuristica deliberada para "e builtin" na parte (a): o pacote compiler
// nao importa o pacote vm (evitaria um ciclo de import) e portanto nao tem
// como enumerar os nomes nativos (`length`, `contains`, os `probe_*` de
// teste, etc.) — e o compilador ja tolera qualquer global desconhecido em
// tempo de compilacao hoje (Identifier caindo no global sem checar
// c.globals — ver o case *ast.Identifier de Compile), delegando para o
// runtime. Em vez de duplicar (e deixar desatualizada) uma lista de
// builtins, esta funcao reproduz a MESMA tolerancia: um nome ausente do
// modulo definidor SO vira erro quando ele resolve para ALGO no
// importador — o UNICO caso onde existe risco real de captura silenciosa
// por homonimo. Um nome ausente dos dois lados (o caso comum de um
// builtin genuino) passa em silencio, como o resto do compilador já faz.
func (c *Compiler) validateImportedTemplateScope(tpl *FuncTemplate, arguments []ast.NoxyType, line int) error {
	if tpl.Module == "" || tpl.Module == c.moduleName {
		return nil
	}
	exports, loadable := c.discoverModuleExports(tpl.Module)
	if !loadable {
		// Modulo ja teria falhado a carregar em outro ponto (predeclare do
		// `use`, ou o proprio ensureFunctionInstance nao teria achado tpl no
		// registry) — nao ha o que reportar aqui de novo.
		return nil
	}
	display := displayInstanceName(tpl.Decl.Name, arguments)
	registry := c.registryOrInit()
	for name := range collectFreeIdentifiers(tpl.Decl) {
		if _, declaredInModule := exports[name]; !declaredInModule {
			if _, shadowedByImporter := c.globals[name]; shadowedByImporter {
				return fmt.Errorf(
					"[line %d] '%s' referencia '%s', não declarado no módulo '%s'",
					line, display, name, tpl.Module,
				)
			}
			continue // presumivelmente builtin — ver heuristica no comentario acima
		}

		definedType, hasDefinedType := c.importedBindingType(tpl.Module, name)
		if !hasDefinedType {
			// name e ele mesmo outro template generico do modulo definidor: a
			// dependencia certa e ele estar IMPORTADO (registrado no registry
			// do importador) E VINDO DO MESMO MODULO DEFINIDOR — nao ter um
			// tipo em globals (templates nunca tem tipo de valor). O registry
			// e um mapa FLAT por nome bare (R8): sem o check de Module aqui,
			// um template HOMONIMO importado de um modulo DIFERENTE (`use
			// outro select ajuda` quando quem processa<T> precisa e o
			// 'ajuda' de 'colecoes') passaria a validacao por engano — a
			// chamada bare-name dentro do corpo clonado (compileGenericCallSite)
			// resolveria contra ESSE template errado em silencio, exatamente
			// a classe de bug que o §8 existe pra prevenir.
			funcDep, isFuncDep := registry.Funcs[name]
			structDep, isStructDep := registry.Structs[name]
			if (isFuncDep && funcDep.Module == moduleQualifier(tpl.Module)) ||
				(isStructDep && structDep.Module == moduleQualifier(tpl.Module)) {
				continue
			}
			return fmt.Errorf(
				"[line %d] '%s' precisa de '%s' de '%s' — adicione ao select ou use select *",
				line, display, name, tpl.Module,
			)
		}

		importerType, hasImporterType := c.globals[name]
		if !hasImporterType {
			return fmt.Errorf(
				"[line %d] '%s' precisa de '%s' de '%s' — adicione ao select ou use select *",
				line, display, name, tpl.Module,
			)
		}
		if importerType == nil || !(definedType.String() == importerType.String() || c.typesEquivalent(definedType, importerType)) {
			importerDesc := "desconhecido"
			if importerType != nil {
				importerDesc = importerType.String()
			}
			return fmt.Errorf(
				"[line %d] '%s' tem tipo %s no importador e %s em '%s' — conflito de shadowing",
				line, name, importerDesc, definedType.String(), tpl.Module,
			)
		}
	}
	return nil
}

// collectFreeIdentifiers devolve o conjunto de identificadores LIDOS
// (leitura OU alvo de atribuicao — os dois exigem que o nome ja resolva
// para um binding existente) no corpo de um template de funcao que NAO sao
// localmente vinculados por ele (parametros, `let`, variavel de for-each,
// parametro de lambda ou de func aninhada, nome de func/struct aninhado).
//
// Sobre-aproxima o conjunto de nomes vinculados numa unica passada FLAT
// (collectBoundNamesInBlock), sem pilha de escopos: um nome local a um
// ramo do corpo e tratado como vinculado no corpo INTEIRO. Isso so pode
// produzir FALSOS NEGATIVOS (deixar de sinalizar uma referencia realmente
// fora de escopo que por coincidencia tem o mesmo nome de um local em outro
// ramo) — nunca falso positivo, que rejeitaria um template importado
// valido. E a direcao segura para a checagem do §8.
func collectFreeIdentifiers(decl *ast.FunctionStatement) map[string]bool {
	bound := make(map[string]bool, len(decl.Parameters))
	for _, param := range decl.Parameters {
		bound[param.Name] = true
	}
	collectBoundNamesInBlock(decl.Body, bound)
	free := make(map[string]bool)
	collectFreeInBlock(decl.Body, bound, free)
	return free
}

// collectBoundNamesInBlock/Statement/Expression povoam bound com todo nome
// de VALOR introduzido em qualquer profundidade do corpo — espelham a
// cobertura de nos de substituteInStatement/substituteInExpression
// (generics_substitute.go), inclusive o panic no default: um nó novo em ast
// tem de ganhar tratamento aqui tambem, senao este walker fica cego para os
// bindings que esse nó introduz.
//
// A exaustividade e verificada estaticamente por
// TestGenericWalkersCoverEveryNode (generics_walkers_guard_test.go), que
// enumera os nós de ast.go e exige um `case` em cada walker — o panic
// sozinho so dispararia se algum teste exercitasse exatamente o nó novo.
func collectBoundNamesInBlock(blk *ast.BlockStatement, bound map[string]bool) {
	if blk == nil {
		return
	}
	for _, s := range blk.Statements {
		collectBoundNamesInStatement(s, bound)
	}
}

func collectBoundNamesInStatement(s ast.Statement, bound map[string]bool) {
	if s == nil {
		return
	}
	switch n := s.(type) {
	case *ast.LetStmt:
		bound[n.Name.Value] = true
		collectBoundNamesInExpression(n.Value, bound)
	case *ast.AssignStmt:
		collectBoundNamesInExpression(n.Target, bound)
		collectBoundNamesInExpression(n.Value, bound)
	case *ast.ReturnStmt:
		collectBoundNamesInExpression(n.ReturnValue, bound)
	case *ast.DeferStmt:
		collectBoundNamesInExpression(n.Call, bound)
	case *ast.BreakStmt:
		// sem nome, sem sub-no.
	case *ast.ContinueStmt:
		// sem nome, sem sub-no.
	case *ast.UseStmt:
		// sem identificador de valor vinculado por este walker.
	case *ast.ExpressionStmt:
		collectBoundNamesInExpression(n.Expression, bound)
	case *ast.BlockStatement:
		collectBoundNamesInBlock(n, bound)
	case *ast.IfStatement:
		collectBoundNamesInExpression(n.Condition, bound)
		collectBoundNamesInBlock(n.Consequence, bound)
		collectBoundNamesInBlock(n.Alternative, bound)
	case *ast.WhileStatement:
		collectBoundNamesInExpression(n.Condition, bound)
		collectBoundNamesInBlock(n.Body, bound)
	case *ast.FunctionStatement:
		bound[n.Name] = true
		for _, p := range n.Parameters {
			bound[p.Name] = true
		}
		collectBoundNamesInBlock(n.Body, bound)
	case *ast.ForStatement:
		bound[n.Identifier] = true
		collectBoundNamesInExpression(n.Collection, bound)
		collectBoundNamesInBlock(n.Body, bound)
	case *ast.StructStatement:
		// struct aninhado: so o nome do TIPO, que este walker (identificadores
		// de VALOR) nao rastreia.
	case *ast.WhenStatement:
		for _, clause := range n.Cases {
			collectBoundNamesInStatement(clause.Condition, bound)
			collectBoundNamesInBlock(clause.Body, bound)
		}
	default:
		panic("collectBoundNamesInStatement: nó sem case — adicione aqui e o guard passa")
	}
}

func collectBoundNamesInExpression(e ast.Expression, bound map[string]bool) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BytesLiteral, *ast.NullLiteral, *ast.Boolean:
		// sem sub-no.
	case *ast.ZerosLiteral:
		collectBoundNamesInExpression(n.Size, bound)
	case *ast.PrefixExpression:
		collectBoundNamesInExpression(n.Right, bound)
	case *ast.InfixExpression:
		collectBoundNamesInExpression(n.Left, bound)
		collectBoundNamesInExpression(n.Right, bound)
	case *ast.FunctionLiteral:
		for _, p := range n.Parameters {
			bound[p.Name] = true
		}
		collectBoundNamesInBlock(n.Body, bound)
	case *ast.CallExpression:
		collectBoundNamesInExpression(n.Function, bound)
		for _, a := range n.Arguments {
			collectBoundNamesInExpression(a, bound)
		}
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			collectBoundNamesInExpression(el, bound)
		}
	case *ast.MapLiteral:
		for i := range n.Keys {
			collectBoundNamesInExpression(n.Keys[i], bound)
		}
		for i := range n.Values {
			collectBoundNamesInExpression(n.Values[i], bound)
		}
	case *ast.IndexExpression:
		collectBoundNamesInExpression(n.Left, bound)
		collectBoundNamesInExpression(n.Index, bound)
	case *ast.MemberAccessExpression:
		collectBoundNamesInExpression(n.Left, bound)
	default:
		panic("collectBoundNamesInExpression: nó sem case — adicione aqui e o guard passa")
	}
}

// collectFreeInBlock/Statement/Expression e o segundo passe de
// collectFreeIdentifiers: mesma cobertura de nós que
// collectBoundNamesInBlock/Statement/Expression (e o mesmo guard estatico de
// TestGenericWalkersCoverEveryNode), so que em vez de POVOAR bound, cada
// *ast.Identifier encontrado em posicao de valor (leitura ou alvo de
// atribuicao) que NAO esta em bound entra em free.
// MemberAccessExpression.Member e uma string crua (nao um Identifier), e
// portanto nunca contribui — `x.campo` nao exige que "campo" resolva como
// nome solto.
func collectFreeInBlock(blk *ast.BlockStatement, bound, free map[string]bool) {
	if blk == nil {
		return
	}
	for _, s := range blk.Statements {
		collectFreeInStatement(s, bound, free)
	}
}

func collectFreeInStatement(s ast.Statement, bound, free map[string]bool) {
	if s == nil {
		return
	}
	switch n := s.(type) {
	case *ast.LetStmt:
		collectFreeInExpression(n.Value, bound, free)
	case *ast.AssignStmt:
		collectFreeInExpression(n.Target, bound, free)
		collectFreeInExpression(n.Value, bound, free)
	case *ast.ReturnStmt:
		collectFreeInExpression(n.ReturnValue, bound, free)
	case *ast.DeferStmt:
		collectFreeInExpression(n.Call, bound, free)
	case *ast.BreakStmt:
	case *ast.ContinueStmt:
	case *ast.UseStmt:
	case *ast.ExpressionStmt:
		collectFreeInExpression(n.Expression, bound, free)
	case *ast.BlockStatement:
		collectFreeInBlock(n, bound, free)
	case *ast.IfStatement:
		collectFreeInExpression(n.Condition, bound, free)
		collectFreeInBlock(n.Consequence, bound, free)
		collectFreeInBlock(n.Alternative, bound, free)
	case *ast.WhileStatement:
		collectFreeInExpression(n.Condition, bound, free)
		collectFreeInBlock(n.Body, bound, free)
	case *ast.FunctionStatement:
		collectFreeInBlock(n.Body, bound, free)
	case *ast.ForStatement:
		collectFreeInExpression(n.Collection, bound, free)
		collectFreeInBlock(n.Body, bound, free)
	case *ast.StructStatement:
		// sem identificador de valor.
	case *ast.WhenStatement:
		for _, clause := range n.Cases {
			collectFreeInStatement(clause.Condition, bound, free)
			collectFreeInBlock(clause.Body, bound, free)
		}
	default:
		panic("collectFreeInStatement: nó sem case — adicione aqui e o guard passa")
	}
}

func collectFreeInExpression(e ast.Expression, bound, free map[string]bool) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case *ast.Identifier:
		if !bound[n.Value] {
			free[n.Value] = true
		}
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BytesLiteral, *ast.NullLiteral, *ast.Boolean:
		// sem sub-no.
	case *ast.ZerosLiteral:
		collectFreeInExpression(n.Size, bound, free)
	case *ast.PrefixExpression:
		collectFreeInExpression(n.Right, bound, free)
	case *ast.InfixExpression:
		collectFreeInExpression(n.Left, bound, free)
		collectFreeInExpression(n.Right, bound, free)
	case *ast.FunctionLiteral:
		collectFreeInBlock(n.Body, bound, free)
	case *ast.CallExpression:
		collectFreeInExpression(n.Function, bound, free)
		for _, a := range n.Arguments {
			collectFreeInExpression(a, bound, free)
		}
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			collectFreeInExpression(el, bound, free)
		}
	case *ast.MapLiteral:
		for i := range n.Keys {
			collectFreeInExpression(n.Keys[i], bound, free)
		}
		for i := range n.Values {
			collectFreeInExpression(n.Values[i], bound, free)
		}
	case *ast.IndexExpression:
		collectFreeInExpression(n.Left, bound, free)
		collectFreeInExpression(n.Index, bound, free)
	case *ast.MemberAccessExpression:
		collectFreeInExpression(n.Left, bound, free)
	default:
		panic("collectFreeInExpression: nó sem case — adicione aqui e o guard passa")
	}
}
