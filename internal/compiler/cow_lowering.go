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
// indicando se o nível final era ref (informativo: desde a #50 o
// member-assignment checa o campo do mesmo jeito com base ref ou valor).
func (c *Compiler) compileLValueBase(expr ast.Expression) (ast.NoxyType, bool, error) {
	switch n := expr.(type) {
	case *ast.Identifier:
		var t ast.NoxyType
		if arg, localType := c.resolveLocal(n.Value); arg != -1 {
			// RC: a pergunta e "este slot RETEM o que guarda?" (Local.Owns),
			// marcado exatamente onde o inc e emitido. Com todo local nao-ref
			// possuidor desde o nascimento (let, parametro sem ref, variavel de
			// for-each, binding de case do select — spec §4.2), Owns coincide
			// com "tipo declarado nao e `ref T`" para locais nomeados; o flag
			// continua sendo a fonte de verdade para nao reabrir o dec a menos
			// se um bind site futuro esquecer o inc.
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
		if err := c.rejectRefRead(idxType, n.Index, "index"); err != nil {
			return nil, false, err
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
		if idx, ok := c.fieldSlot(leftType, n.Member); ok {
			c.emitFieldOp(chunk.OP_GET_FIELD_MUT, idx, n.Member)
		} else {
			nameConst := c.makeConstant(value.NewString(n.Member))
			c.emitOpWithConstantIndex(chunk.OP_GET_PROP_MUT, nameConst)
		}
		// memberType: dono resolvido pela declaracao (`File` ≡ `io.File`) e
		// tipo do campo ja na visao do programa (issue #58 item 1).
		t, wasRef := c.derefMutIfRef(c.memberType(leftType, n.Member))
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

// NOTA (Task 8): havia aqui um trio de funções (reconhecimento de composto
// fresco, checagem de tipo marcável e emissão condicional) que decidia
// quando emitir OP_MARK_SHARED, o antigo opcode de marcação sticky. A
// unicidade agora é decidida em runtime pelo contador Owners (RC) — o
// compilador não emite mais esse opcode. Removidas junto com as 5 call
// sites em compiler.go; o opcode segue definido em chunk.go só para não
// renumerar.
