package compiler

import "noxy-vm/internal/ast"

// Indexacao tipada de array (issue #66, item 1): o compilador sabe quando a
// base e T[] e emite os opcodes especializados de internal/chunk. Este arquivo
// reune os predicados da decisao; a emissao mora em compiler.go (leitura em
// `case *ast.IndexExpression`, escrita na atribuicao a IndexExpression e no
// for-each).

// isUntrackedElementType responde se um ELEMENTO desse tipo estatico nunca
// tem contador RC — os unicos casos em que a escrita pode usar a forma NORC.
// Lista fechada por nome: struct tambem e PrimitiveType (pelo nome da
// declaracao) e `any` pode guardar composto, entao ambos respondem false.
func isUntrackedElementType(t ast.NoxyType) bool {
	prim, ok := t.(*ast.PrimitiveType)
	if !ok {
		return false
	}
	switch prim.Name {
	case "int", "float", "bool", "string", "bytes":
		return true
	}
	return false
}

// arrayTypeOf desembrulha um nivel de `ref` e devolve o ArrayType da base,
// se for um — o tipo que decide entre OP_GET_INDEX_ARRAY e OP_GET_INDEX.
func arrayTypeOf(t ast.NoxyType) (*ast.ArrayType, bool) {
	if ref, ok := t.(*ast.RefType); ok {
		t = ref.ElementType
	}
	arr, ok := t.(*ast.ArrayType)
	return arr, ok
}
