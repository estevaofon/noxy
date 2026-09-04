package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// Inferencia local de tipo em `let` (issue #41, spec §3): `let x = expr` sem
// anotacao binda o tipo ESTATICO do RHS, como se ele tivesse sido escrito. A
// inferencia e unidirecional (RHS -> binding) e so acontece em `let` — nada
// de Hindley-Milner, parametros ou retornos continuam anotados.
//
// O tipo inferido e gravado in-place em LetStmt.Type (mesmo mecanismo das
// anotacoes resolvidas de genericos), entao TODO o resto do caminho — checagem
// de type-stability, RC/borrow de `ref T`, registro de globais, exports de
// modulo — e identico ao de um `let` anotado.

// inferLetType valida o tipo estatico do inicializador de `let name` e
// devolve o tipo a bindar. Recusa, com hint, o que nao tem tipo unico:
// literal vazio (`[]`, `{}`), `null`, tipo desconhecido (global ainda nao
// declarado) e `any` no TOPO — fronteira dinamica, que a spec quer explicita
// na anotacao. `any` aninhado (`map[string, any]`) e um tipo declaravel comum
// e e inferido fielmente.
func inferLetType(name string, valType ast.NoxyType, line int) (ast.NoxyType, error) {
	fail := func(reason, hint string) error {
		return fmt.Errorf("[line %d] cannot infer type for '%s' from its initializer: %s\n  hint: use 'let %s: %s'",
			line, name, reason, name, hint)
	}
	if valType == nil {
		// Fontes usuais de tipo desconhecido: global declarado mais adiante,
		// membro de modulo cujo tipo o programa nao consegue nomear (issue
		// #126 item 2: o membro de namespace passou a ser tipado, exceto
		// quando programViewType nao acha nome para alguma parte do tipo),
		// builtin sem tipo de retorno estatico (ver builtin_return_types.go).
		return nil, fail("its type is not known here (a global declared later, a module member the program cannot name, or a builtin without a static return type)", "<type> = ...")
	}
	if isNullType(valType) {
		return nil, fail("'null' has no type of its own", "<type> = null")
	}
	if valType.String() == "void" {
		return nil, fail("the initializer does not return a value (void)", "<type> = ...")
	}
	if isAny(valType) {
		return nil, fail("the initializer's type is 'any'", "any = ...")
	}
	switch typed := valType.(type) {
	case *ast.ArrayType:
		if typed.ElementType == nil {
			return nil, fail("an empty array literal has no element type", "<type>[] = []")
		}
	case *ast.MapType:
		if typed.KeyType == nil || typed.ValueType == nil {
			return nil, fail("an empty map literal has no key/value types", "map[<key>, <value>] = {}")
		}
	}
	if reason := incompleteTypeReason(valType); reason != "" {
		hint := "<type> = ..."
		switch valType.(type) {
		case *ast.ArrayType:
			hint = "<type>[] = ..."
		case *ast.MapType:
			hint = "map[<key>, <value>] = ..."
		}
		return nil, fail(reason, hint)
	}
	return normalizeInferredType(valType), nil
}

// incompleteTypeReason procura, em qualquer profundidade, um componente sem
// tipo unico: elemento/chave/valor nil (`[[]]`, `{"a": []}`) ou `null`
// (`[null]`). Devolve "" quando o tipo e totalmente determinado.
func incompleteTypeReason(t ast.NoxyType) string {
	switch typed := t.(type) {
	case nil:
		return "part of its type is not known"
	case *ast.ArrayType:
		if typed.ElementType == nil {
			return "an empty array literal inside it has no element type"
		}
		if isNullType(typed.ElementType) {
			return "'null' has no type of its own"
		}
		return incompleteTypeReason(typed.ElementType)
	case *ast.MapType:
		if typed.KeyType == nil || typed.ValueType == nil {
			return "an empty map literal inside it has no key/value types"
		}
		if isNullType(typed.KeyType) || isNullType(typed.ValueType) {
			return "'null' has no type of its own"
		}
		if reason := incompleteTypeReason(typed.KeyType); reason != "" {
			return reason
		}
		return incompleteTypeReason(typed.ValueType)
	case *ast.RefType:
		if typed.ElementType == nil {
			return "part of its type is not known"
		}
		return incompleteTypeReason(typed.ElementType)
	case *ast.NullableType:
		if typed.ElementType == nil {
			return "part of its type is not known"
		}
		return incompleteTypeReason(typed.ElementType)
	case *ast.ChanType:
		if typed.ElementType == nil {
			return "part of its type is not known"
		}
		return incompleteTypeReason(typed.ElementType)
	}
	return ""
}

// normalizeInferredType transforma o tipo de um LITERAL no tipo de um
// BINDING: `[1, 2, 3]` e int[3] como valor, mas a variavel que o recebe e
// int[] — fixar o tamanho surpreenderia (push, reatribuicao com outro
// tamanho) e nenhuma anotacao escrita a mao diria `int[3]` para isso. A
// normalizacao e profunda (`[[1, 2], [3, 4]]` -> int[][]) e so reconstroi os
// containers; tipos folha (primitivos, structs, funcoes) sao compartilhados.
func normalizeInferredType(t ast.NoxyType) ast.NoxyType {
	switch typed := t.(type) {
	case *ast.ArrayType:
		return &ast.ArrayType{ElementType: normalizeInferredType(typed.ElementType)}
	case *ast.MapType:
		return &ast.MapType{KeyType: normalizeInferredType(typed.KeyType), ValueType: normalizeInferredType(typed.ValueType)}
	case *ast.RefType:
		return &ast.RefType{ElementType: normalizeInferredType(typed.ElementType)}
	case *ast.NullableType:
		return nullable(normalizeInferredType(typed.ElementType))
	case *ast.ChanType:
		return &ast.ChanType{ElementType: normalizeInferredType(typed.ElementType)}
	}
	return t
}

// inferGlobalLetTypes e a segunda varredura de predeclareGlobalBindings:
// `let` de topo SEM anotacao precisa do tipo ja na pre-declaracao, porque
// corpos de funcao declarados antes do `let` leem o global por
// programBindings — sem isso o global cairia em "tipo desconhecido" e uma
// atribuicao errada dentro da funcao passaria em silencio.
//
// Roda DEPOIS da varredura principal (funcoes, structs, lets anotados ja em
// c.globals), na ordem do programa, compilando cada RHS num chunk descartavel
// (typeOfDiscardedExpression) so para ler o tipo. O tipo vai in-place para
// declaration.Type, entao quando o LetStmt compilar de verdade ele segue o
// caminho anotado — e a segunda compilacao do RHS e a unica, no bytecode
// final. Um RHS que nao tem tipo aqui (`let a = b` com `b` declarado depois)
// e erro de inferencia: em runtime seria leitura de global indefinido.
func (c *Compiler) inferGlobalLetTypes(statements []ast.Statement) error {
	for _, statement := range statements {
		declaration, ok := statement.(*ast.LetStmt)
		if !ok || declaration.Type != nil || declaration.Value == nil {
			continue
		}
		c.setLine(declaration.Token.Line)
		valType, err := c.typeOfGlobalInitializer(declaration.Value)
		if err != nil {
			return err
		}
		inferred, err := inferLetType(declaration.Name.Value, valType, declaration.Token.Line)
		if err != nil {
			return err
		}
		declaration.Type = inferred
		c.globals[declaration.Name.Value] = inferred
	}
	return nil
}

// typeOfGlobalInitializer e o leitor de tipo da varredura de globais. Um
// literal de funcao tem o tipo escrito na propria assinatura: le-la direto
// (resolvendo anotacoes de struct generico, como o compile faria) evita
// compilar o CORPO fora de ordem — o corpo pode ler um global inferido
// declarado DEPOIS (`let f = func() ... counter ... end` / `let counter =
// 10`), que nesta altura ainda nao tem tipo. Qualquer outra expressao passa
// por typeOfDiscardedExpression.
func (c *Compiler) typeOfGlobalInitializer(expr ast.Expression) (ast.NoxyType, error) {
	if literal, ok := expr.(*ast.FunctionLiteral); ok {
		if err := c.resolveSignatureAnnotations(literal.Parameters, &literal.ReturnType, literal.Token.Line); err != nil {
			return nil, err
		}
		return newFunctionType(literal.Parameters, literal.ReturnType), nil
	}
	return c.typeOfDiscardedExpression(expr)
}
