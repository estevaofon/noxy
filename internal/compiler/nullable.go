package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// Helpers de nulidade (spec §2.4, issue #105 item 1). `T?` e ast.NullableType;
// um `T` nu nunca e null (fase 2). `any` e `null` ja admitem null e nunca
// sao embrulhados.

func isNullable(t ast.NoxyType) bool {
	_, ok := t.(*ast.NullableType)
	return ok
}

// nonNull devolve o elemento de `T?` e true; para qualquer outro tipo,
// devolve o proprio tipo e false.
func nonNull(t ast.NoxyType) (ast.NoxyType, bool) {
	if n, ok := t.(*ast.NullableType); ok {
		return n.ElementType, true
	}
	return t, false
}

// nullable embrulha t em `T?`, idempotente: `T??` normaliza para `T?`, e
// `any`/`null` voltam iguais.
func nullable(t ast.NoxyType) ast.NoxyType {
	if t == nil || isNullable(t) || isAny(t) || isNullType(t) {
		return t
	}
	return &ast.NullableType{ElementType: t}
}

// asRefType responde "este slot e de referencia?" desembrulhando `?`:
// `ref T` e `ref T?` sao os dois slots de modo ref (ParamInfo.IsRef,
// RefFields, rebind por nome). Quem quer LER atraves da referencia (`*r`,
// `r.f`) tem de tratar a nulidade antes, com isNullable/mayBeNullError.
func asRefType(t ast.NoxyType) (*ast.RefType, bool) {
	if n, ok := t.(*ast.NullableType); ok {
		t = n.ElementType
	}
	ref, ok := t.(*ast.RefType)
	return ref, ok
}

// nullMismatchHint acrescenta ao mismatch "expected T, got T?" o motivo:
// o valor pode ser null e precisa ser testado antes.
func (c *Compiler) nullMismatchHint(expected, actual ast.NoxyType, expr ast.Expression) string {
	// Fase 2: `null` num T nu — a solucao e declarar o slot como T?.
	if isNullType(actual) && expected != nil && !c.acceptsNull(expected) && nullableAlternative(expected) {
		return fmt.Sprintf("\n  hint: declare it as '%s' to allow null", (&ast.NullableType{ElementType: expected}).String())
	}
	if !isNullable(actual) || isNullable(expected) || isAny(expected) {
		return ""
	}
	if key, ok := stableKey(expr); ok {
		return fmt.Sprintf("\n  hint: '%s' may be null; test it first", key)
	}
	return "\n  hint: the value may be null; bind it with 'let' and test it"
}

// stableKey e a chave canonica de uma expressao cujo valor nao muda sem
// uma atribuicao visivel: identificador, `*ident` e cadeias de membro
// (`a.b.c`). E a unidade do narrowing (narrowing.go) e dos hints.
func stableKey(expr ast.Expression) (string, bool) {
	switch e := expr.(type) {
	case *ast.Identifier:
		return e.Value, true
	case *ast.PrefixExpression:
		if e.Operator != "*" {
			return "", false
		}
		inner, ok := stableKey(e.Right)
		if !ok {
			return "", false
		}
		return "*" + inner, true
	case *ast.MemberAccessExpression:
		base, ok := stableKey(e.Left)
		if !ok {
			return "", false
		}
		return base + "." + e.Member, true
	}
	return "", false
}
