package compiler

import (
	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
)

// Campo de struct por indice em compilacao (issue #96).
//
// Desde o #95 uma instancia guarda os campos em Slots na ordem da declaracao
// (ObjStruct.Fields vem de StructStatement.FieldsList, compiler.go). Quando o
// tipo estatico da base e um struct do PROGRAMA, a posicao do campo em
// FieldsList E o indice do slot em runtime, e o acesso sai em OP_GET_FIELD /
// OP_SET_FIELD / OP_GET_FIELD_MUT com [idx][nome] — sem hashing no caminho
// quente. O nome vai junto porque a VM confere `Fields[idx] == nome` antes de
// usar o indice: json_loads monta definicoes proprias em ordem alfabetica
// (spec 2026-08-28 §3.1), e essas instancias entram em conteineres tipados.

// maxFieldSlot: o indice viaja em 1 byte; struct maior fica por nome.
const maxFieldSlot = 255

// fieldSlot devolve o indice de slot de `member` no struct estatico `owner`
// (desembrulhando `ref`), ou ok=false quando o acesso tem de ficar por nome:
// base `any`/desconhecida, campo inexistente (o erro e do memberType/runtime),
// struct de MODULO (a issue #96 os deixa por nome; a definicao de runtime e
// emitida pelo compilador do modulo) ou indice acima de 255.
func (c *Compiler) fieldSlot(owner ast.NoxyType, member string) (int, bool) {
	primitive, ok := unwrapRefType(owner).(*ast.PrimitiveType)
	if !ok {
		return 0, false
	}
	definition := c.structDeclarationOf(primitive)
	if definition == nil || c.structOrigin(definition) != "" {
		return 0, false
	}
	for i, field := range definition.FieldsList {
		if field.Name == member {
			if i > maxFieldSlot {
				return 0, false
			}
			return i, true
		}
	}
	return 0, false
}

// emitFieldOp emite op [idx u8][nome u16] (o nome como constante, o mesmo
// operando que a familia por nome usa).
func (c *Compiler) emitFieldOp(op chunk.OpCode, idx int, member string) {
	nameConst := c.makeConstant(value.NewString(member))
	if nameConst > 65535 {
		panic("constant pool overflow: too many constants in chunk")
	}
	c.emitByte(byte(op))
	c.emitByte(byte(idx))
	c.emitByte(byte((nameConst >> 8) & 0xff))
	c.emitByte(byte(nameConst & 0xff))
}
