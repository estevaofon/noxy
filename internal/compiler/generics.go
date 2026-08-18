package compiler

import (
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
// O ponteiro e compartilhado entre o compilador real, o compilador
// descartavel do pass 1, os compiladores de corpo de instancia e todos os
// filhos (NewChild): ha exatamente uma fila por unidade de compilacao.
type instanceQueue struct {
	memo    map[string]bool
	ordered []ast.Statement
}

func newInstanceQueue() *instanceQueue {
	return &instanceQueue{memo: make(map[string]bool)}
}

// instancesOrInit e o analogo de registryOrInit para a fila de instancias.
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
func (c *Compiler) predeclareGenericTemplates(statements []ast.Statement) {
	for _, statement := range statements {
		switch declaration := statement.(type) {
		case *ast.FunctionStatement:
			if len(declaration.TypeParams) > 0 {
				c.registryOrInit().Funcs[declaration.Name] = &FuncTemplate{Decl: declaration, Module: c.moduleName}
			}
		case *ast.StructStatement:
			if len(declaration.TypeParams) > 0 {
				c.registryOrInit().Structs[declaration.Name] = &StructTemplate{Decl: declaration, Module: c.moduleName}
			}
		}
	}
}

// hasGenerics decide se o Program precisa do two-pass. A varredura das
// declaracoes ja aconteceu em predeclareGenericTemplates, entao a pergunta se
// reduz a "o registro tem algum template?" — o que tambem cobre o caso do
// REPL/modulos, onde o registro chega populado de fora (SetGenericState) sem
// que a linha atual declare nada generico. Programa sem genericos responde
// false e pula o pass 1 inteiro (custo exatamente zero, §5).
func (c *Compiler) hasGenerics() bool {
	registry := c.registryOrInit()
	return len(registry.Funcs) > 0 || len(registry.Structs) > 0
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
	scratch := c.newPass1Compiler()
	if _, _, err := scratch.Compile(program); err != nil {
		return err
	}
	queue := c.instancesOrInit()
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
	scratch := NewWithStateAndRoot(globalsCopy, structsCopy, c.FileName, c.moduleRoot)
	scratch.moduleName = c.moduleName
	scratch.generics = c.registryOrInit()
	scratch.instances = c.instancesOrInit()
	scratch.moduleDiscovery = c.moduleDiscovery
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
	for index, argument := range call.Arguments {
		expected := params[index].Type
		// Parametro sem parametro de tipo nao contribui binding: nao ha razao
		// para compilar o argumento so para descobrir um tipo que nao sera
		// unificado (o caminho normal o compila e checa logo em seguida).
		if !containsTypeParam(expected) {
			continue
		}
		actual, err := c.typeOfDiscardedExpression(argument)
		if err != nil {
			return err
		}
		if err := unify(expected, actual, bindings); err != nil {
			return fmt.Errorf("[line %d] argumento %d de '%s': %v", line, index+1, base, err)
		}
	}

	// Hint do `let`: resolve o T que so aparece no retorno (`let xs: int[] =
	// vazio()`). Depois dos argumentos, por decisao da spec §7 — o argumento e
	// a ancora primaria, o alvo do target-typing e complemento.
	if hint != nil && containsTypeParam(tpl.Decl.ReturnType) {
		if err := unify(tpl.Decl.ReturnType, hint, bindings); err != nil {
			return fmt.Errorf("[line %d] retorno de '%s': %v", line, base, err)
		}
	}

	name, _, err := c.ensureFunctionInstance(tpl, bindings, line)
	if err != nil {
		return err
	}
	callee.Value = name
	c.setLine(line)
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

	parameterTypes := make([]ast.NoxyType, len(tpl.Decl.Parameters))
	for index, parameter := range tpl.Decl.Parameters {
		parameterTypes[index] = substituteType(parameter.Type, bindings)
	}
	instanceType := &ast.FunctionType{
		Params: parameterTypes,
		Return: substituteType(tpl.Decl.ReturnType, bindings),
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

	instance := substituteFunction(tpl, bindings, name)
	if err := c.compileInstanceBody(instance); err != nil {
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
// Como isso so roda no pass 1, cujo bytecode inteiro e descartado, eventuais
// efeitos colaterais no compilador (constantes orfas, upvalues abertos duas
// vezes — addUpvalue deduplica) nao alcancam o bytecode final do pass 2.
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
	if _, isTemplate := c.registryOrInit().Funcs[callee.Value]; !isTemplate {
		return
	}
	c.genericReturnHint = target
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
