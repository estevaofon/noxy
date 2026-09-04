package compiler

import (
	"fmt"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// compileMapMemberAssignment compila `m.chave = v` quando m tem tipo estatico
// map[K, V] (issue #133, caso 3). O ramo de mapa que o #133 acrescentou ao
// OP_SET_PROPERTY tornou a forma com ponto executavel para QUALQUER mapa, nao
// so para o objeto de namespace; sem checagem estatica um `map[string, int]`
// passava a guardar uma string enquanto `m["chave"] = "boom"` continuava
// recusado — duas grafias da mesma escrita com regras diferentes. Aqui a
// escrita e tratada exatamente como `m["chave"] = v`:
//
//   - a chave e sempre a string do membro, entao K tem de aceitar string;
//   - o valor tem de satisfazer V, com o hint de deref do caminho indexado;
//   - slot de valor `ref T` so aceita ref (referenceSlotAssignmentTypeError);
//   - emitSlotGuards antes de emitir.
//
// A base ja foi compilada por compileLValueBase (cadeia MUT, igual ao caminho
// indexado). A LEITURA com ponto sobre mapa continua dinamica (memberType
// devolve nil para dono map) — fora de escopo.
//
// Pilha: [base] (ja empilhada), valor, OP_SET_PROPERTY ([base, val] -> [val]),
// OP_POP.
func (c *Compiler) compileMapMemberAssignment(n *ast.AssignStmt, target *ast.MemberAccessExpression, mapType *ast.MapType) (*chunk.Chunk, ast.NoxyType, error) {
	stringType := &ast.PrimitiveType{Name: "string"}
	if !c.areTypesCompatible(mapType.KeyType, stringType) {
		return nil, nil, fmt.Errorf("[line %d] type mismatch in map key: expected %s, got string", c.currentLine, mapType.KeyType.String())
	}

	if err := c.rewriteIfGenericValue(n.Value, mapType.ValueType); err != nil {
		return nil, nil, err
	}
	_, valType, err := c.Compile(n.Value)
	if err != nil {
		return nil, nil, err
	}

	if refType, isRef := asRefType(mapType.ValueType); isRef &&
		!(isReferenceType(valType) || valType == nil || isNullType(valType)) &&
		c.areTypesCompatible(refType.ElementType, valType) {
		return nil, nil, referenceSlotAssignmentTypeError(c.currentLine, assignmentTargetName(target), "entry", mapType.ValueType, valType)
	}
	if !c.areTypesCompatible(mapType.ValueType, valType) {
		return nil, nil, fmt.Errorf("[line %d] type mismatch in map value: expected %s, got %s%s", c.currentLine, mapType.ValueType.String(), valType.String(), c.derefReadHint(mapType.ValueType, valType, n.Value))
	}
	if err := c.emitSlotGuards(mapType.ValueType, valType); err != nil {
		return nil, nil, err
	}

	c.emitOpWithConstantIndex(chunk.OP_SET_PROPERTY, c.makeConstant(value.NewString(target.Member)))
	c.emitByte(byte(chunk.OP_POP))
	return c.currentChunk, nil, nil
}
