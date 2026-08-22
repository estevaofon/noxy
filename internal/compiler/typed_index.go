package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
)

// Indexacao tipada de array (issue #66, item 1): o compilador sabe quando a
// base e T[] e emite os opcodes especializados de internal/chunk. Este arquivo
// reune os predicados e as decisoes; a emissao das formas genericas tipadas
// mora em compiler.go (leitura em `case *ast.IndexExpression`, escrita na
// atribuicao a IndexExpression e no for-each), e as formas fundidas por slot
// sao emitidas por tryFuseLocalIndexAssign / fusedLocalIndexRead abaixo.

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

// isSideEffectFree e o predicado sintatico que libera as formas FUNDIDAS por
// slot (spec §3.3): elas leem o slot do local DEPOIS de avaliar indice (e
// valor), entao so valem quando nada ali pode rodar codigo — chamada
// (inclusive builtin e f-string, que viram chamadas), closure, `ref`, literal
// composto, zeros — que rebindasse ou compartilhasse o local no meio da
// statement. Com operandos assim, a ordem observavel e a da sequencia
// generica. Conservador: o que nao esta na lista e impuro.
func isSideEffectFree(expr ast.Expression) bool {
	switch n := expr.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BytesLiteral, *ast.Boolean, *ast.NullLiteral:
		return true
	case *ast.PrefixExpression:
		switch n.Operator {
		case "-", "!", "~", "*":
			return isSideEffectFree(n.Right)
		}
		return false
	case *ast.InfixExpression:
		return isSideEffectFree(n.Left) && isSideEffectFree(n.Right)
	case *ast.IndexExpression:
		return isSideEffectFree(n.Left) && isSideEffectFree(n.Index)
	case *ast.MemberAccessExpression:
		return isSideEffectFree(n.Left)
	}
	return false
}

// fusedLocalIndexRead decide se `n` (X[i]) pode ser lida pela forma fundida
// por slot: X e identificador resolvido a local (slot <= 255) de tipo T[]
// (OP_GET_LOCAL_INDEX_ARRAY) ou `ref T[]` (OP_GET_REF_LOCAL_INDEX_ARRAY, que
// resolve a caixa do ref dentro do opcode — sem OP_DEREF) e i e livre de
// efeito colateral. Devolve o opcode, o slot e o tipo do elemento.
func (c *Compiler) fusedLocalIndexRead(n *ast.IndexExpression) (chunk.OpCode, int, ast.NoxyType, bool) {
	ident, ok := n.Left.(*ast.Identifier)
	if !ok {
		return 0, 0, nil, false
	}
	arg, localType := c.resolveLocal(ident.Value)
	if arg == -1 || arg > 255 || !isSideEffectFree(n.Index) {
		return 0, 0, nil, false
	}
	if arr, ok := localType.(*ast.ArrayType); ok {
		return chunk.OP_GET_LOCAL_INDEX_ARRAY, arg, arr.ElementType, true
	}
	if ref, ok := localType.(*ast.RefType); ok {
		if arr, ok := ref.ElementType.(*ast.ArrayType); ok {
			return chunk.OP_GET_REF_LOCAL_INDEX_ARRAY, arg, arr.ElementType, true
		}
	}
	return 0, 0, nil, false
}

// tryFuseLocalIndexAssign funde `x[i] = v` quando x e local T[] POSSUIDOR
// (OP_SET_LOCAL_INDEX_ARRAY_NORC) ou local `ref T[]` NAO-possuidor
// (OP_SET_REF_LOCAL_INDEX_ARRAY_NORC — a semantica de GET_LOCAL_MUT_BORROW +
// DEREF_MUT dentro do opcode), T sem contador RC, e i e v sao livres de
// efeito colateral. Devolve (true, nil) se emitiu; (true, err) num erro de
// compilacao — as MESMAS checagens e mensagens do ramo ArrayType do caminho
// generico, so que sem compileLValueBase antes (para identificador local ele
// nunca erra: so emitiria a cadeia MUT que a forma fundida substitui);
// (false, nil) para seguir o caminho generico. Owns continua a fonte de
// verdade (spec CoW-RC §4.2): slot T[] nao-possuidor ou slot ref possuidor
// seriam estado inesperado e vao pelo generico.
func (c *Compiler) tryFuseLocalIndexAssign(target *ast.IndexExpression, valueExpr ast.Expression) (bool, error) {
	ident, ok := target.Left.(*ast.Identifier)
	if !ok {
		return false, nil
	}
	arg, localType := c.resolveLocal(ident.Value)
	if arg == -1 || arg > 255 {
		return false, nil
	}
	var op chunk.OpCode
	var arrType *ast.ArrayType
	switch t := localType.(type) {
	case *ast.ArrayType:
		if !c.localOwns(arg) {
			return false, nil
		}
		op, arrType = chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC, t
	case *ast.RefType:
		// Slot `ref T[]` EMPRESTA (spec CoW-RC §4.2): a forma fundida de ref
		// reproduz GET_LOCAL_MUT_BORROW + DEREF_MUT.
		inner, isArr := t.ElementType.(*ast.ArrayType)
		if !isArr || c.localOwns(arg) {
			return false, nil
		}
		op, arrType = chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC, inner
	default:
		return false, nil
	}
	if !isUntrackedElementType(arrType.ElementType) || !isSideEffectFree(target.Index) || !isSideEffectFree(valueExpr) {
		return false, nil
	}
	_, idxType, err := c.Compile(target.Index)
	if err != nil {
		return true, err
	}
	if _, isRef := idxType.(*ast.RefType); isRef {
		c.emitByte(byte(chunk.OP_DEREF))
	}
	_, valType, err := c.Compile(valueExpr)
	if err != nil {
		return true, err
	}
	if ref, isRef := idxType.(*ast.RefType); isRef {
		idxType = ref.ElementType
	}
	if idxType != nil && idxType.String() != "int" {
		return true, fmt.Errorf("[line %d] array index must be int, got %s", c.currentLine, idxType.String())
	}
	if !c.areTypesCompatible(arrType.ElementType, valType) {
		return true, fmt.Errorf("[line %d] type mismatch in array assignment: expected %s, got %s%s", c.currentLine, arrType.ElementType.String(), valType.String(), c.derefReadHint(arrType.ElementType, valType, valueExpr))
	}
	if err := c.emitRuntimeValueType(arrType.ElementType); err != nil {
		return true, err
	}
	c.emitBytes(byte(op), byte(arg))
	return true, nil
}
