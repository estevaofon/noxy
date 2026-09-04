package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
)

// typeWithoutDefault responde qual (sub)tipo impede `let nome: tipo` sem
// inicializador (issue #61 item 1; spec §3). chan, os tipos funcao e — desde
// a fase 2 do issue #105 (spec §2.4) — struct e `ref` NAO tem valor default:
// `= null` e rejeitado pelo checador (acceptsNull so admite any/null/T?), e
// o null que emitDefaultInit fabricava para eles era a unica brecha. `T?`
// tem default (null). Array dimensionado repete o default do ELEMENTO, entao
// herda a restricao; array dinamico e map comecam vazios e nao precisam de
// default de elemento. Devolve nil quando o tipo tem default.
func (c *Compiler) typeWithoutDefault(t ast.NoxyType) ast.NoxyType {
	switch typed := t.(type) {
	case *ast.ChanType, *ast.FunctionType, *ast.RefType:
		return t
	case *ast.NullableType:
		return nil
	case *ast.PrimitiveType:
		if isBareFunctionType(typed) {
			return t
		}
		if c.structDeclarationOf(typed) != nil {
			return t
		}
	case *ast.ArrayType:
		if typed.Size > 0 {
			return c.typeWithoutDefault(typed.ElementType)
		}
	}
	return nil
}

// defaultInitError e o erro de `let nome: tipo` sem valor quando o tipo (ou
// o elemento do array dimensionado) nao tem default; nil quando tem. Para
// struct e ref o hint oferece a forma anulavel.
func (c *Compiler) defaultInitError(name string, declared ast.NoxyType) error {
	culprit := c.typeWithoutDefault(declared)
	if culprit == nil {
		return nil
	}
	hint := fmt.Sprintf("hint: write 'let %s: %s = ...'", name, declared.String())
	if nullableAlternative(culprit) {
		hint += fmt.Sprintf(" or declare it as '%s'", (&ast.NullableType{ElementType: declared}).String())
	}
	return fmt.Errorf("[line %d] variable '%s' needs an initializer: %s has no default value; %s",
		c.currentLine, name, culprit.String(), hint)
}

// nullableAlternative: o tipo sem default aceitaria `?` (struct e ref sim;
// chan e func nao admitem null nem com `?` — spec §3).
func nullableAlternative(culprit ast.NoxyType) bool {
	switch typed := culprit.(type) {
	case *ast.RefType:
		return true
	case *ast.PrimitiveType:
		return !isBareFunctionType(typed)
	}
	return false
}
