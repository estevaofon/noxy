package compiler

// Terceira familia de hooks da spec §4: TODA resolucao de GenericType em
// posicao de anotacao de tipo e um site de instanciacao de struct. Sem call
// site e sem target-typing, `let xs: Caixa<int>[] = []` ja exige que
// main::Caixa<int> exista em c.structs (resolucao de campo por member access) e
// tenha runtime type info completa (validacao CoW). Este arquivo concentra a
// maquinaria: resolveAnnotation (o hook), ensureStructInstance (a
// monomorfizacao) e o site de construtor `Caixa(41)`.

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// structInstanceKey guarda a tupla de tipos que gerou uma instancia de struct.
// Serve para o caminho INVERSO da resolucao: um tipo concreto observado num
// site de chamada ja carrega o nome qualificado (`main::Caixa<int>`), mas a
// anotacao do template contra a qual ele sera unificado esta escrita na forma
// generica (`Caixa<T>`). Com a tupla em maos, expandInstanceNames reconstroi a
// forma generica e unify casa os dois sem precisar conhecer tabela nenhuma.
type structInstanceKey struct {
	Base string
	Args []ast.NoxyType
}

// resolveAnnotation percorre uma anotacao de tipo e devolve a anotacao
// equivalente com todo GenericType substituido pelo nome QUALIFICADO da
// instancia correspondente (`Caixa<int>` -> `main::Caixa<int>`), instanciando o
// struct pelo caminho memoizado do §4. A estrutura externa e preservada:
// `Caixa<int>[]`, `ref Node<int>`, `map[string, Caixa<int>]` e assinaturas de
// funcao voltam com a mesma forma, so com o miolo resolvido.
//
// Erros (§9):
//   - GenericType cujo Name nao e um template em escopo: nao ha o que instanciar.
//   - aridade divergente do template: "'Caixa' espera 1 argumento de tipo, recebeu 2".
//   - TypeParamType: `T` fora de uma declaracao generica nao e um tipo
//     ("tipo 'T' não declarado"). Dentro de template isso nao acontece: templates
//     nao sao compilados, so seus clones — e no clone T ja foi substituido.
//
// Fast path: anotacao sem GenericType nem TypeParamType volta pelo MESMO
// ponteiro, sem alocar. E o que mantem o custo de programa sem genericos em
// exatamente zero (§5) mesmo com o hook em todo ponto de entrada de anotacao.
func (c *Compiler) resolveAnnotation(t ast.NoxyType, line int) (ast.NoxyType, error) {
	if !needsAnnotationResolution(t) {
		return t, nil
	}
	switch n := t.(type) {
	case *ast.GenericType:
		template, isTemplate := c.registryOrInit().Structs[n.Name]
		if !isTemplate {
			return nil, fmt.Errorf("[line %d] '%s' não é um tipo genérico declarado", line, n.Name)
		}
		if len(n.Args) != len(template.Decl.TypeParams) {
			return nil, typeArgumentArityError(line, n.Name, len(template.Decl.TypeParams), len(n.Args))
		}
		// Args ANTES da instancia: `Caixa<Caixa<int>>` resolve o interno
		// primeiro, entao main::Caixa<int> entra na fila antes de quem depende
		// dele (§4 — a ordem de dependencia sai da ordem de criacao).
		args := make([]ast.NoxyType, len(n.Args))
		for index, argument := range n.Args {
			resolved, err := c.resolveAnnotation(argument, line)
			if err != nil {
				return nil, err
			}
			args[index] = resolved
		}
		name, err := c.ensureStructInstance(template, args, line)
		if err != nil {
			return nil, err
		}
		return &ast.PrimitiveType{Name: name}, nil

	case *ast.TypeParamType:
		return nil, fmt.Errorf("[line %d] tipo '%s' não declarado", line, n.Name)

	case *ast.ArrayType:
		element, err := c.resolveAnnotation(n.ElementType, line)
		if err != nil {
			return nil, err
		}
		return &ast.ArrayType{ElementType: element, Size: n.Size}, nil

	case *ast.MapType:
		key, err := c.resolveAnnotation(n.KeyType, line)
		if err != nil {
			return nil, err
		}
		mapValue, err := c.resolveAnnotation(n.ValueType, line)
		if err != nil {
			return nil, err
		}
		return &ast.MapType{KeyType: key, ValueType: mapValue}, nil

	case *ast.RefType:
		element, err := c.resolveAnnotation(n.ElementType, line)
		if err != nil {
			return nil, err
		}
		return &ast.RefType{ElementType: element}, nil

	case *ast.NullableType:
		element, err := c.resolveAnnotation(n.ElementType, line)
		if err != nil {
			return nil, err
		}
		return nullable(element), nil

	case *ast.ChanType:
		element, err := c.resolveAnnotation(n.ElementType, line)
		if err != nil {
			return nil, err
		}
		return &ast.ChanType{ElementType: element}, nil

	case *ast.FunctionType:
		params := make([]ast.NoxyType, len(n.Params))
		for index, param := range n.Params {
			resolved, err := c.resolveAnnotation(param, line)
			if err != nil {
				return nil, err
			}
			params[index] = resolved
		}
		result, err := c.resolveAnnotation(n.Return, line)
		if err != nil {
			return nil, err
		}
		return &ast.FunctionType{Params: params, Return: result}, nil

	default:
		return t, nil
	}
}

// needsAnnotationResolution responde se t carrega, em qualquer profundidade,
// algum no que resolveAnnotation precisa tocar: GenericType (instancia a criar)
// ou TypeParamType (erro de escopo). Anotacao de programa sem genericos
// responde false na primeira olhada e volta intocada.
func needsAnnotationResolution(t ast.NoxyType) bool {
	switch n := t.(type) {
	case *ast.GenericType, *ast.TypeParamType:
		return true
	case *ast.ArrayType:
		return needsAnnotationResolution(n.ElementType)
	case *ast.MapType:
		return needsAnnotationResolution(n.KeyType) || needsAnnotationResolution(n.ValueType)
	case *ast.RefType:
		return needsAnnotationResolution(n.ElementType)
	case *ast.NullableType:
		return needsAnnotationResolution(n.ElementType)
	case *ast.ChanType:
		return needsAnnotationResolution(n.ElementType)
	case *ast.FunctionType:
		for _, param := range n.Params {
			if needsAnnotationResolution(param) {
				return true
			}
		}
		return needsAnnotationResolution(n.Return)
	default:
		return false
	}
}

// resolveSignatureAnnotations resolve as anotacoes de uma assinatura de funcao
// NAO-generica, in-place: os parametros e o tipo de retorno passam a nomear
// instancias qualificadas. In-place e o ponto — o pass 2 compila o MESMO AST e
// tem de ver os nomes ja reescritos (§4, item 4).
func (c *Compiler) resolveSignatureAnnotations(params []*ast.Parameter, returnType *ast.NoxyType, line int) error {
	for _, param := range params {
		resolved, err := c.resolveAnnotation(param.Type, line)
		if err != nil {
			return err
		}
		param.Type = resolved
	}
	resolved, err := c.resolveAnnotation(*returnType, line)
	if err != nil {
		return err
	}
	*returnType = resolved
	return nil
}

// resolveStructFieldAnnotations resolve as anotacoes dos campos de um struct
// (in-place, nos DOIS espelhos: FieldsList, a fonte ordenada, e Fields, o mapa
// por nome — divergir os dois faria resolucao de campo e runtime type info
// discordarem). Serve tanto para struct comum que menciona um generico
// (`struct Caixa_de_caixas  c: Caixa<int> end`) quanto para os campos de uma
// instancia recem-clonada.
func (c *Compiler) resolveStructFieldAnnotations(definition *ast.StructStatement, line int) error {
	fields := make(map[string]ast.NoxyType, len(definition.FieldsList))
	for _, field := range definition.FieldsList {
		resolved, err := c.resolveAnnotation(field.Type, line)
		if err != nil {
			return err
		}
		field.Type = resolved
		fields[field.Name] = resolved
	}
	definition.Fields = fields
	return nil
}

// ensureStructInstance devolve (criando se preciso) o nome qualificado da
// instancia monomorfizada de tpl para args. Ordem obrigatoria do §4, gemea de
// ensureFunctionInstance: nome -> memo -> clone -> REGISTRA o clone (memo,
// c.structs e c.globals) -> so DEPOIS resolve as anotacoes dos campos ->
// enfileira. Registrar antes de resolver os campos e o que faz auto-referencia
// terminar: o campo `next: ref Node<T>` reentra aqui, encontra o memo e para.
//
// A instancia entra em queue.ordered depois de tudo que os campos dela
// exigiram, entao dependencia sempre precede dependente; e como structs sao
// criados durante a resolucao das assinaturas/corpos das instancias de funcao,
// eles tambem precedem naturalmente as funcoes que os usam.
func (c *Compiler) ensureStructInstance(tpl *StructTemplate, args []ast.NoxyType, line int) (string, error) {
	base := tpl.Decl.Name
	if len(args) != len(tpl.Decl.TypeParams) {
		return "", typeArgumentArityError(line, base, len(tpl.Decl.TypeParams), len(args))
	}
	name := instanceName(tpl.Module, base, args)
	if !c.pass1 {
		// Defensivo, gemeo do guard de compileGenericCallSite: no pass 2 toda
		// anotacao alcancavel ja foi reescrita pelo pass 1. Se um GenericType
		// chegou aqui, existe caminho de compilacao que nao passou pelo pass 1 —
		// e instanciar agora registraria o struct sem NUNCA emitir a declaracao
		// (a fila do pass 2 e descartada), virando "undefined global" em runtime.
		return "", fmt.Errorf(
			"[line %d] anotação de tipo genérico '%s' chegou ao pass 2 sem monomorfização — bug do compilador de genéricos",
			line, displayInstanceName(base, args),
		)
	}

	queue := c.instancesOrInit()
	if instance, memoized := queue.structInstances[name]; memoized {
		// Registro SEMPRE no compilador atual, inclusive no acerto de memo: cada
		// filho tem sua propria copia de structs/globals, e quem usa a instancia
		// precisa enxergar os campos (member access) e o construtor (call site).
		c.registerStructInstance(instance)
		return name, nil
	}

	// Teto de aninhamento (§9), gemeo do de ensureFunctionInstance: resolver
	// os campos e o passo que pode pedir OUTRA instancia (`campo: Caixa<T[]>`
	// numa cadeia que nunca fecha).
	if queue.depth >= maxInstantiationDepth {
		return "", instantiationDepthError(line)
	}

	bindings := make(map[string]ast.NoxyType, len(args))
	for index, typeParam := range tpl.Decl.TypeParams {
		bindings[typeParam] = args[index]
	}
	instance := substituteStruct(tpl, bindings, name)
	if tpl.Module != c.moduleName {
		// Template IMPORTADO: o clone herda o token do template, cuja linha e
		// do ARQUIVO DO MODULO — um erro na compilacao da instancia (ex.:
		// campo com struct nao importado, #58) apontaria para uma linha que
		// nao existe no programa. A linha da primeira instanciacao e a do
		// programa. Template local mantem a linha da declaracao (mesmo
		// arquivo, e e la que o campo esta escrito).
		instance.Token.Line = line
	}
	queue.structInstances[name] = instance
	queue.structKeys[name] = structInstanceKey{Base: base, Args: args}
	c.registerStructInstance(instance)

	queue.depth++
	err := c.resolveStructFieldAnnotations(instance, line)
	queue.depth--
	if err != nil {
		return "", instantiationChainError(displayInstanceName(base, args), line, err)
	}
	// Re-registra: os campos mudaram (GenericType -> nome qualificado) e o tipo
	// do construtor derivado deles tem de acompanhar.
	c.registerStructInstance(instance)
	queue.ordered = append(queue.ordered, instance)
	return name, nil
}

// registerStructInstance faz a instancia existir para o compilador atual: a
// declaracao em c.structs (resolucao de campo, runtime type info) e o tipo do
// construtor em c.globals (o call site reescrito resolve um global comum).
func (c *Compiler) registerStructInstance(instance *ast.StructStatement) {
	c.structs[instance.Name] = instance
	params := make([]ast.NoxyType, len(instance.FieldsList))
	for index, field := range instance.FieldsList {
		params[index] = field.Type
	}
	c.globals[instance.Name] = newStructFunctionType(instance.Name, params)
}

// compileGenericConstructorSite e o hook de call site do §4 para construtor de
// struct generico: `Caixa(41)`. Infere a tupla unificando os tipos dos CAMPOS
// (que sao os parametros do construtor posicional) contra os tipos dos
// argumentos, com o hint da anotacao do `let` como complemento para o que os
// argumentos nao ancorarem (`Caixa([])` com `let c: Caixa<int>`), e reescreve o
// identificador para o nome qualificado. Depois disso o caminho normal de
// compileCallExpression resolve um global comum.
func (c *Compiler) compileGenericConstructorSite(call *ast.CallExpression, callee *ast.Identifier, tpl *StructTemplate) error {
	base := tpl.Decl.Name
	line := c.currentLine
	if !c.pass1 {
		return fmt.Errorf(
			"[line %d] construtor do template genérico '%s' chegou ao pass 2 sem monomorfização — bug do compilador de genéricos",
			line, base,
		)
	}

	// O hint vale para ESTE site e so para ele: consumimos antes de compilar os
	// argumentos, que podem conter outros construtores genericos aninhados
	// (`Caixa(Caixa(9))`).
	hint := c.genericReturnHint
	c.genericReturnHint = nil

	fields := tpl.Decl.FieldsList
	if len(call.Arguments) != len(fields) {
		return fmt.Errorf(
			"[line %d] function '%s' expects %d arguments, got %d",
			line, base, len(fields), len(call.Arguments),
		)
	}

	// unifyPositionalArguments/missingTypeParamNullError sao a maquinaria
	// COMPARTILHADA com compileGenericCallSite (generics.go, documentada
	// la): mesma unificacao posicional campo-a-campo, mesmo rastreio de
	// nullOnlyParams (§9 "inferência só com null" — `Caixa(null)`) e
	// argIndexOf (§9 "conflito de unificação" com atribuição por argumento).
	// skip fica nil — construtor de struct nao tem o equivalente aos
	// argumentos-template de compileGenericCallSite (campo posicional e
	// sempre um valor, nunca um template de funcao nu).
	bindings := make(map[string]ast.NoxyType, len(tpl.Decl.TypeParams))
	nullOnlyParams := make(map[string]bool)
	argIndexOf := make(map[string]int, len(tpl.Decl.TypeParams))
	if err := c.unifyPositionalArguments(
		line, base, call.Arguments,
		func(index int) ast.NoxyType { return fields[index].Type },
		nil,
		tpl.Decl.TypeParams, bindings, nullOnlyParams, argIndexOf,
	); err != nil {
		return err
	}

	// Hint do `let` depois dos argumentos (§7: o argumento e a ancora primaria).
	// A anotacao ja E a tupla, posicao a posicao — nao ha o que unificar, so
	// preencher o que ficou em aberto.
	c.applyStructHintBindings(tpl, hint, bindings, line)

	// §9 "inferência só com null": mesmo gate de compileGenericCallSite,
	// ANTES da checagem de aridade generica abaixo (cuja mensagem, para
	// construtor, e "não foi possível inferir %s em '%s'").
	if err := missingTypeParamNullError(line, tpl.Decl.TypeParams, bindings, nullOnlyParams); err != nil {
		return err
	}

	args := make([]ast.NoxyType, 0, len(tpl.Decl.TypeParams))
	for _, typeParam := range tpl.Decl.TypeParams {
		bound, ok := bindings[typeParam]
		if !ok {
			return fmt.Errorf(
				"[line %d] não foi possível inferir %s em '%s' — anote o tipo",
				line, typeParam, base,
			)
		}
		args = append(args, bound)
	}

	name, err := c.ensureStructInstance(tpl, args, line)
	if err != nil {
		return err
	}
	callee.Value = name
	c.setLine(line)
	return nil
}

// applyStructHintBindings completa bindings com os argumentos de tipo escritos
// na anotacao do `let` que envolve o site de construtor. Regra geral: so
// preenche parametro de tipo AINDA em aberto — o argumento e a ancora
// primaria (§7), e um conflito entre argumento e anotacao aparece adiante
// como erro de tipo do `let`, com a mensagem do caminho normal.
//
// Excecao deliberada (§7: "any é legal como argumento de tipo explícito" é
// uma regra incondicional, não qualificada por "só quando nada mais
// bindou"): quando a anotacao pede `any` explicitamente para um parametro,
// esse `any` PREVALECE mesmo sobre um binding ja inferido do argumento —
// `Caixa<any> = Caixa(1)` produz a instancia `Caixa<any>` (campo `any`
// aceita o `1` normalmente pelo caminho comum de checagem de tipos), em vez
// de conflitar com `Caixa<int>` inferido do argumento sozinho. A checagem e
// sintatica direta (isAny do proprio no da anotacao, sem resolveAnnotation)
// porque `any` nunca e GenericType — não ha instanciacao/efeito colateral a
// evitar ao inspeciona-lo cedo, ao contrario do caso geral abaixo.
//
// A anotacao chega em uma de duas formas, dependendo de quem a resolveu
// primeiro (o predeclare do topo do programa ou o proprio case do `let`):
// `Caixa<int>` crua ou `main::Caixa<int>` ja qualificada. expandInstanceNames
// normaliza as duas para a forma generica, que e onde a tupla esta legivel.
func (c *Compiler) applyStructHintBindings(tpl *StructTemplate, hint ast.NoxyType, bindings map[string]ast.NoxyType, line int) {
	if hint == nil {
		return
	}
	annotation, ok := c.expandInstanceNames(hint).(*ast.GenericType)
	if !ok || annotation.Name != tpl.Decl.Name || len(annotation.Args) != len(tpl.Decl.TypeParams) {
		return
	}
	for index, typeParam := range tpl.Decl.TypeParams {
		if isAny(annotation.Args[index]) {
			bindings[typeParam] = annotation.Args[index]
			continue
		}
		if _, bound := bindings[typeParam]; bound {
			continue
		}
		// A anotacao pode nomear outro generico (`Caixa<Caixa<int>>`): resolve o
		// argumento para o nome qualificado antes de bindar, para que a tupla
		// deste site seja identica a que a propria anotacao produz.
		resolved, err := c.resolveAnnotation(annotation.Args[index], line)
		if err != nil {
			// Anotacao invalida nao vira erro AQUI: a resolucao da propria
			// anotacao do `let` (que roda no mesmo statement) reporta com o
			// contexto certo. Aqui so deixamos o parametro em aberto.
			return
		}
		bindings[typeParam] = resolved
	}
}

// unifyAnnotation unifica um tipo de TEMPLATE (expected, escrito na forma
// generica — pode conter `Caixa<T>`) contra um tipo CONCRETO observado no site
// (actual, que ja carrega nomes qualificados de instancia, `main::Caixa<int>`).
// Sem tradutor, essas duas formas nunca casariam: o template diz `Caixa<T>` e o
// mundo diz `main::Caixa<int>`. Antes de unificar, os nomes de instancia em
// actual sao re-expandidos para GenericType a partir da tupla memoizada, e
// unify (funcao pura, sem acesso a tabela nenhuma) faz o resto.
func (c *Compiler) unifyAnnotation(expected, actual ast.NoxyType, bindings map[string]ast.NoxyType) error {
	if containsGenericType(expected) {
		actual = c.expandInstanceNames(actual)
	}
	return unify(expected, actual, bindings)
}

// expandInstanceNames reconstroi a forma generica de todo nome de instancia
// dentro de t (`main::Caixa<int>` -> `Caixa<int>`, recursivamente nos args).
// Tipo que nao contem instancia nenhuma volta pelo mesmo ponteiro.
func (c *Compiler) expandInstanceNames(t ast.NoxyType) ast.NoxyType {
	switch n := t.(type) {
	case *ast.PrimitiveType:
		key, ok := c.structInstanceKey(n.Name)
		if !ok {
			return t
		}
		args := make([]ast.NoxyType, len(key.Args))
		for index, argument := range key.Args {
			args[index] = c.expandInstanceNames(argument)
		}
		return &ast.GenericType{Name: key.Base, Args: args}
	case *ast.ArrayType:
		return &ast.ArrayType{ElementType: c.expandInstanceNames(n.ElementType), Size: n.Size}
	case *ast.MapType:
		return &ast.MapType{
			KeyType:   c.expandInstanceNames(n.KeyType),
			ValueType: c.expandInstanceNames(n.ValueType),
		}
	case *ast.RefType:
		return &ast.RefType{ElementType: c.expandInstanceNames(n.ElementType)}
	case *ast.NullableType:
		return &ast.NullableType{ElementType: c.expandInstanceNames(n.ElementType)}
	case *ast.ChanType:
		return &ast.ChanType{ElementType: c.expandInstanceNames(n.ElementType)}
	case *ast.FunctionType:
		params := make([]ast.NoxyType, len(n.Params))
		for index, param := range n.Params {
			params[index] = c.expandInstanceNames(param)
		}
		return &ast.FunctionType{Params: params, Return: c.expandInstanceNames(n.Return)}
	case *ast.GenericType:
		// Forma generica JA expandida no topo, mas com argumentos que podem
		// carregar nomes de instancia (`Pilha<main::Caixa<int>>`): recursa nos
		// args para que os dois lados de uma unificacao usem a mesma grafia em
		// toda profundidade.
		args := make([]ast.NoxyType, len(n.Args))
		for index, argument := range n.Args {
			args[index] = c.expandInstanceNames(argument)
		}
		return &ast.GenericType{Name: n.Name, Args: args}
	default:
		return t
	}
}

// structInstanceKey consulta a tupla que gerou uma instancia de struct. So
// existe durante a compilacao de um programa (a fila vive por Compile de
// Program, §5); fora dela nao ha instancia para expandir.
func (c *Compiler) structInstanceKey(name string) (structInstanceKey, bool) {
	if c.instances == nil {
		return structInstanceKey{}, false
	}
	key, ok := c.instances.structKeys[name]
	return key, ok
}

// containsGenericType responde se t carrega um GenericType em qualquer
// profundidade — ou seja, se t e uma anotacao de template que menciona um
// struct generico e portanto precisa da re-expansao de unifyAnnotation.
func containsGenericType(t ast.NoxyType) bool {
	switch n := t.(type) {
	case *ast.GenericType:
		return true
	case *ast.ArrayType:
		return containsGenericType(n.ElementType)
	case *ast.MapType:
		return containsGenericType(n.KeyType) || containsGenericType(n.ValueType)
	case *ast.RefType:
		return containsGenericType(n.ElementType)
	case *ast.NullableType:
		return containsGenericType(n.ElementType)
	case *ast.ChanType:
		return containsGenericType(n.ElementType)
	case *ast.FunctionType:
		for _, param := range n.Params {
			if containsGenericType(param) {
				return true
			}
		}
		return containsGenericType(n.Return)
	default:
		return false
	}
}

// typeArgumentArityError e a mensagem de aridade de argumentos de tipo do §9:
// "'Caixa' espera 1 argumento de tipo, recebeu 2".
func typeArgumentArityError(line int, name string, want, got int) error {
	noun := "argumentos"
	if want == 1 {
		noun = "argumento"
	}
	return fmt.Errorf("[line %d] '%s' espera %d %s de tipo, recebeu %d", line, name, want, noun, got)
}

// forEachElementType diz o tipo da variavel de um for-each a partir do tipo da
// colecao, quando ele e estaticamente conhecido (ruling R5). Array itera
// elementos; map itera CHAVES (o compilador reescreve `for k in m` para iterar
// `keys(m)`, ver o case de ForStatement). Colecao de tipo desconhecido, string,
// ou array/map com elemento nao inferido continuam com variavel sem tipo — nil
// aqui e "nao sei", que e exatamente o comportamento anterior.
//
// Tipar a variavel do laco nao e cosmetico para genericos: e a unica ancora de
// inferencia de `identity(v)` dentro de um for-each — sem ela o argumento chega
// a unificacao como nil e T fica sem binding.
//
// A colecao nunca e ref aqui (R2 rejeita antes).
func forEachElementType(collection ast.NoxyType) ast.NoxyType {
	switch typed := collection.(type) {
	case *ast.ArrayType:
		return typed.ElementType
	case *ast.MapType:
		return typed.KeyType
	default:
		return nil
	}
}
