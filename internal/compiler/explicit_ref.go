package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// refReadHint e o hint que acompanha todo erro "esperava T, veio ref T"
// (spec 2026-08-24-explicit-ref, R2): a leitura e sempre explicita com '*'.
func refReadHint(expr ast.Expression) string {
	if ident, ok := expr.(*ast.Identifier); ok {
		return fmt.Sprintf("\n  hint: use '*%s' to read the referenced value", ident.Value)
	}
	return "\n  hint: use '*' to read the referenced value"
}

// rejectRefRead aplica R2 nas posicoes que nao passam por areTypesCompatible
// (operando de operador, condicao, indice, colecao de for): onde o compilador
// espera um valor, um `ref T` estatico e erro. `where` nomeia a posicao na
// mensagem ("operand of '+'", "condition", "index").
func (c *Compiler) rejectRefRead(t ast.NoxyType, expr ast.Expression, where string) error {
	if _, isRef := t.(*ast.RefType); !isRef {
		return nil
	}
	return fmt.Errorf("[line %d] %s cannot be %s: a ref is never read implicitly%s",
		c.currentLine, where, noxyTypeName(t), refReadHint(expr))
}

// refArgument e o resultado de compileRefArgument.
type refArgument struct {
	element ast.NoxyType // tipo apontado; nil para null ou tipo desconhecido
	plain   ast.NoxyType // != nil quando o argumento e um valor T conhecido (R5 violada)
	proven  bool         // modo provado em compilacao (false: any/desconhecido -> validateParameterModes)
}

// compileRefArgument compila um argumento destinado a um parametro ou slot
// `ref T` (R5): `ref x` cria a referencia (R1, compileReferenceArgument);
// qualquer outra expressao e compilada como valor comum e precisa JA ter
// tipo `ref T` (variavel, campo, elemento, chamada que retorna ref), ser
// `null`, ou ter tipo desconhecido/any (fronteira dinamica). Um valor T
// conhecido volta em `plain` para o chamador montar o erro com a posicao.
func (c *Compiler) compileRefArgument(arg ast.Expression) (refArgument, error) {
	if prefix, ok := arg.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
		element, err := c.compileReferenceArgument(prefix.Right)
		if err != nil {
			return refArgument{}, err
		}
		return refArgument{element: element, proven: true}, nil
	}
	_, actual, err := c.Compile(arg)
	if err != nil {
		return refArgument{}, err
	}
	if ref, ok := actual.(*ast.RefType); ok {
		return refArgument{element: ref.ElementType, proven: true}, nil
	}
	if isNullType(actual) {
		return refArgument{proven: true}, nil
	}
	if actual == nil || isAny(actual) {
		return refArgument{}, nil
	}
	return refArgument{plain: actual}, nil
}

// exprDisplay renderiza expr como o fonte Noxy, para mensagens de diagnostico
// — sem os parenteses de agrupamento que MemberAccessExpression.String() e
// IndexExpression.String() usam internamente (AST, nao mensagem ao usuario),
// e com aspas em torno de um indice string literal (StringLiteral.String()
// devolve o texto cru sem aspas — `m["k"]` viraria `m[k]`, um hint que nao
// compila de volta).
func exprDisplay(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.MemberAccessExpression:
		return exprDisplay(e.Left) + "." + e.Member
	case *ast.IndexExpression:
		return exprDisplay(e.Left) + "[" + exprDisplay(e.Index) + "]"
	case *ast.StringLiteral:
		return fmt.Sprintf("%q", e.Value)
	default:
		return expr.String()
	}
}

// refArgumentHint diz como consertar um valor T passado onde se esperava
// ref T: `ref x` para o que e enderecavel; para literal/temporario, uma
// variavel antes.
func refArgumentHint(arg ast.Expression) string {
	switch arg.(type) {
	case *ast.Identifier, *ast.MemberAccessExpression, *ast.IndexExpression:
		return fmt.Sprintf("\n  hint: use 'ref %s'", exprDisplay(arg))
	}
	return "\n  hint: bind the value to a variable and pass 'ref <name>'"
}

// valueNativeSpec descreve QUAIS argumentos de uma native central sao sempre
// um VALOR (R2). `all` cobre todas as posicoes; senao valem so as posicoes
// listadas em `positions` (0-based).
type valueNativeSpec struct {
	all       bool
	positions []int
}

func (spec valueNativeSpec) checksArg(pos int) bool {
	if spec.all {
		return true
	}
	for _, p := range spec.positions {
		if p == pos {
			return true
		}
	}
	return false
}

// valueNatives sao as nativas centrais sem assinatura (DefineNative /
// DefineContextualNative sem NativeSignature) cujos argumentos sao sempre um
// VALOR, nunca uma referencia: nao passam por compileBuiltinCall (nao sao
// append/pop/delete/json_loads/range) nem tem ast.FunctionType para
// areStrictTypesCompatible barrar um `ref T` — sem este check dedicado um
// `ref T` estatico chegaria a native, que responderia com o default
// silencioso (0, [], false) ou codificaria o String() da referencia
// ("<ref ...>"), como o repro do Task 10a mostrou.
//
// A tabela e a MESMA para o check estatico (rejectRefArgumentsForValueNatives,
// aqui) e para o dinamico (rejectRefArgs, internal/vm) — o runtime a consulta
// por ValueNativeChecksArg.
//
// Escopo por posicao (M1 da revisao final #82): nas cinco colecoes so o
// argumento 1 e a colecao; o argumento 2 de contains/has_key e um elemento ou
// uma chave, e um ref ali e um valor de busca legitimo. As nativas de
// codificacao/serializacao consomem TODOS os argumentos como valor.
var valueNatives = map[string]valueNativeSpec{
	// colecoes: so a colecao (argumento 1)
	"length":   {positions: []int{0}},
	"keys":     {positions: []int{0}},
	"slice":    {positions: []int{0}},
	"contains": {positions: []int{0}},
	"has_key":  {positions: []int{0}},
	// codificacao / serializacao / cripto: todos os argumentos
	"json_dumps":                {all: true},
	"json_dumps_result":         {all: true},
	"json_parse":                {all: true},
	"base64_encode":             {all: true},
	"base64_decode":             {all: true},
	"hex":                       {all: true},
	"hex_encode":                {all: true},
	"hex_decode":                {all: true},
	"base62_encode":             {all: true},
	"base62_decode":             {all: true},
	"to_bytes":                  {all: true},
	"fmt":                       {all: true},
	"crypto_pbkdf2_sha256":      {all: true},
	"crypto_aes256_gcm_encrypt": {all: true},
	"crypto_aes256_gcm_decrypt": {all: true},
}

// ValueNativeChecksArg diz se o argumento na posicao pos (0-based) da native
// central chamada name e sempre um valor (R2). E o ponto de consulta do
// runtime (internal/vm, rejectRefArgs): o compilador e a VM decidem pela
// MESMA tabela quais posicoes recusam um `ref T`.
func ValueNativeChecksArg(name string, pos int) bool {
	spec, listed := valueNatives[name]
	return listed && spec.checksArg(pos)
}

// rejectRefArgumentsForValueNatives aplica R2 aos argumentos das nativas de
// valueNatives quando o callee resolve para o native global (nao sombreado
// por local/upvalue/global declarado — mesma checagem de compileBuiltinCall e
// do fallback de builtinReturnType em compileCallExpression). Um argumento
// `ref T` estatico e erro; any/tipo desconhecido segue para a checagem em
// runtime (rejectRefArgs, VM).
func (c *Compiler) rejectRefArgumentsForValueNatives(call *ast.CallExpression, argTypes []ast.NoxyType) error {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return nil
	}
	spec, listed := valueNatives[ident.Value]
	if !listed {
		return nil
	}
	if c.isShadowedByLocal(ident.Value) {
		return nil
	}
	if _, declared := c.globals[ident.Value]; declared {
		return nil
	}
	// argTypes so e posicional no caminho NAO-exato: no exato (callee com
	// ast.FunctionType) o laco de compileCallExpression pula o append em
	// argumentos de parametro `ref T`, e o indice deixaria de casar com
	// call.Arguments. Nenhuma native desta tabela e exata (todas sao sem
	// assinatura, isExact false), entao o guard so protege contra um futuro
	// que lhes de assinatura — nesse dia o check certo passa a ser o de
	// areStrictTypesCompatible, nao este.
	if len(argTypes) != len(call.Arguments) {
		return nil
	}
	for i, argType := range argTypes {
		if _, isRef := argType.(*ast.RefType); !isRef {
			continue
		}
		if !spec.checksArg(i) {
			continue
		}
		return fmt.Errorf("[line %d] argument %d to '%s': expected a value, got %s%s",
			c.currentLine, i+1, ident.Value, noxyTypeName(argType), refReadHint(call.Arguments[i]))
	}
	return nil
}

// alreadyReferenceError e R1: `ref e` com e ja de tipo `ref T`.
func alreadyReferenceError(line int, expr ast.Expression) error {
	display := exprDisplay(expr)
	return fmt.Errorf("[line %d] '%s' is already a reference\n  hint: pass '%s' directly, without 'ref'",
		line, display, display)
}
