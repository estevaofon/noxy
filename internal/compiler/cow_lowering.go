package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// compileLValueBase compila a BASE de um lvalue emitindo a cadeia de opcodes
// *_MUT que uniciza cada nível do caminho de mutação (CoW), gravando clones
// de volta no slot pai. Devolve o tipo estático da expressão, já com refs
// dereferenciados (emite OP_DEREF_MUT quando o tipo é ref), e um flag
// indicando se o nível final era ref — o branch de member-assignment usa o
// flag para replicar a leniência do checker pré-0.4 com bases ref.
func (c *Compiler) compileLValueBase(expr ast.Expression) (ast.NoxyType, bool, error) {
	switch n := expr.(type) {
	case *ast.Identifier:
		var t ast.NoxyType
		if arg, localType := c.resolveLocal(n.Value); arg != -1 {
			// RC: a pergunta e "este slot RETEM o que guarda?" (Local.Owns), nao
			// "o tipo declarado e `ref T`?". Nao-possuidor e estritamente mais
			// largo: alem dos slots `ref`, a variavel de for-each e o binding de
			// case do select recebem um valor sem inc e sem tipo declarado.
			// Emitir o gemeo possuidor neles fazia o caminho MUT soltar um
			// objeto que o slot nunca reteve (dec a menos).
			if c.localOwns(arg) {
				c.emitBytes(byte(chunk.OP_GET_LOCAL_MUT), byte(arg))
			} else {
				c.emitBytes(byte(chunk.OP_GET_LOCAL_MUT_BORROW), byte(arg))
			}
			t = localType
		} else if arg, upvalueType := c.resolveUpvalue(n.Value); arg != -1 {
			c.emitBytes(byte(chunk.OP_GET_UPVALUE_MUT), byte(arg))
			t = upvalueType
		} else {
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL_MUT, nameConstant)
			t = c.globals[n.Value] // pode ser nil (desconhecido/any)
		}
		t, wasRef := c.derefMutIfRef(t)
		return t, wasRef, nil

	case *ast.IndexExpression:
		leftType, _, err := c.compileLValueBase(n.Left)
		if err != nil {
			return nil, false, err
		}
		_, idxType, err := c.Compile(n.Index)
		if err != nil {
			return nil, false, err
		}
		if _, ok := idxType.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_DEREF))
		}
		c.emitByte(byte(chunk.OP_GET_INDEX_MUT))
		var t ast.NoxyType
		if arrType, ok := leftType.(*ast.ArrayType); ok {
			t = arrType.ElementType
		} else if mapType, ok := leftType.(*ast.MapType); ok {
			t = mapType.ValueType
		}
		t, wasRef := c.derefMutIfRef(t)
		return t, wasRef, nil

	case *ast.MemberAccessExpression:
		leftType, _, err := c.compileLValueBase(n.Left)
		if err != nil {
			return nil, false, err
		}
		nameConst := c.makeConstant(value.NewString(n.Member))
		c.emitOpWithConstantIndex(chunk.OP_GET_PROP_MUT, nameConst)
		var t ast.NoxyType
		if prim, ok := leftType.(*ast.PrimitiveType); ok {
			if structDef, exists := c.structs[prim.Name]; exists {
				for _, f := range structDef.FieldsList {
					if f.Name == n.Member {
						t = f.Type
						break
					}
				}
			}
		}
		t, wasRef := c.derefMutIfRef(t)
		return t, wasRef, nil

	default:
		return nil, false, fmt.Errorf("[line %d] invalid assignment target", c.currentLine)
	}
}

// derefMutIfRef emite OP_DEREF_MUT quando o tipo estático é ref, devolvendo
// o tipo do elemento e true; caso contrário devolve o tipo inalterado e false.
func (c *Compiler) derefMutIfRef(t ast.NoxyType) (ast.NoxyType, bool) {
	if refType, ok := t.(*ast.RefType); ok {
		c.emitByte(byte(chunk.OP_DEREF_MUT))
		return refType.ElementType, true
	}
	return t, false
}

// isFreshComposite reconhece expressões que produzem um composto novo em
// folha — literais e zeros — cujo resultado não precisa de OP_MARK_SHARED
// ao ser armazenado.
func isFreshComposite(expr ast.Expression) bool {
	switch expr.(type) {
	case *ast.ArrayLiteral, *ast.MapLiteral, *ast.ZerosLiteral:
		return true
	}
	return false
}

// typeNeedsSharedMark decide se um valor do tipo estático dado pode ser um
// composto CoW (array, map, struct ou desconhecido/any) e portanto precisa
// de OP_MARK_SHARED ao ser armazenado em um slot.
func (c *Compiler) typeNeedsSharedMark(t ast.NoxyType) bool {
	switch tt := t.(type) {
	case nil:
		return true // desconhecido: conservador
	case *ast.ArrayType, *ast.MapType:
		return true
	case *ast.PrimitiveType:
		if _, isStruct := c.structs[tt.Name]; isStruct {
			return true
		}
		return tt.Name == "any"
	default:
		return false
	}
}

// emitMarkSharedForStore emite OP_MARK_SHARED para o valor no topo da pilha
// quando ele pode ser um composto que passa a ter mais de um dono.
func (c *Compiler) emitMarkSharedForStore(valueExpr ast.Expression, valType ast.NoxyType) {
	if valueExpr != nil && isFreshComposite(valueExpr) {
		return
	}
	if !c.typeNeedsSharedMark(valType) {
		return
	}
	c.emitByte(byte(chunk.OP_MARK_SHARED))
}
