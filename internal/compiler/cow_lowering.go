package compiler

import (
	"fmt"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
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
		} else if _, isNamespace := c.pureNamespaceAlias(n.Value); isNamespace {
			// Objeto do namespace: visao do modulo, nunca unicizado — issue
			// #133. O ObjMap do alias COMPARTILHA o bindingStore do modulo, e
			// copyValue clona um ObjMap para um store NOVO: se a raiz
			// entrasse pelo caminho _MUT e o mapa tivesse mais de um dono
			// (dois `use` do mesmo modulo — o cache entrega o MESMO valor —,
			// ou `let m: any = st`), a escrita cairia num orfao destacado e o
			// modulo nunca a veria. Leitura simples, entao: o FILHO ainda e
			// unicizado por OP_GET_PROP_MUT e gravado de volta com Set no
			// MESMO objeto de mapa, que e o que o store compartilhado ve.
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, nameConstant)
			t = nil // o caso MemberAccessExpression tipa `m.a` por namespaceMemberType
		} else {
			if !c.globalIsKnown(n.Value) {
				return nil, false, c.undefinedGlobalError(n.Value)
			}
			nameConstant := c.makeConstant(value.NewString(n.Value))
			c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL_MUT, nameConstant)
			t = c.globals[n.Value] // pode ser nil (desconhecido/any)
		}
		t = c.narrowType(n.Value, t)
		if isNullable(t) {
			return nil, false, c.mayBeNullError(n, t)
		}
		return c.derefMutIfRef(t, n)

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
		if isNullable(t) {
			return nil, false, c.mayBeNullError(n, t)
		}
		return c.derefMutIfRef(t, n)

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
		t := c.memberType(leftType, n.Member)
		if t == nil && leftType == nil {
			// Issue #133: raiz `m.a` de um lvalue pelo namespace — o tipo do
			// membro traduzido, para que `m.a.b = v` e `m.xs[i] = v` entrem
			// no funil tipado. namespaceMemberType ja exige alias nao
			// sombreado; leftType nil garante que o global e o marcador de
			// namespace (um global tipado homonimo teria tipo).
			t = c.namespaceMemberType(n)
		}
		if key, ok := stableKey(n); ok {
			t = c.narrowType(key, t)
		}
		if isNullable(t) {
			return nil, false, c.mayBeNullError(n, t)
		}
		return c.derefMutIfRef(t, n)

	default:
		return nil, false, fmt.Errorf("[line %d] invalid assignment target", c.currentLine)
	}
}

// derefMutIfRef emite OP_DEREF_MUT quando o tipo estático é ref, devolvendo
// o tipo do elemento e true; caso contrário devolve o tipo inalterado e false.
// `ref (T?)` (slot apontado anulavel) e erro: a escrita atravessaria um null.
func (c *Compiler) derefMutIfRef(t ast.NoxyType, base ast.Expression) (ast.NoxyType, bool, error) {
	if refType, ok := t.(*ast.RefType); ok {
		elem := refType.ElementType
		if key, ok := stableKey(base); ok {
			elem = c.narrowType("*"+key, elem)
		}
		if isNullable(elem) {
			return nil, false, c.mayBeNullError(&ast.PrefixExpression{Operator: "*", Right: base}, refType.ElementType)
		}
		c.emitByte(byte(chunk.OP_DEREF_MUT))
		return elem, true, nil
	}
	return t, false, nil
}

// NOTA (Task 8): havia aqui um trio de funções (reconhecimento de composto
// fresco, checagem de tipo marcável e emissão condicional) que decidia
// quando emitir OP_MARK_SHARED, o antigo opcode de marcação sticky. A
// unicidade agora é decidida em runtime pelo contador Owners (RC) — o
// compilador não emite mais esse opcode. Removidas junto com as 5 call
// sites em compiler.go; o opcode segue definido em chunk.go só para não
// renumerar.
