package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Frame raiz: LocalBase = 1, slot local 0 = stack[1]. Os testes deste arquivo
// montam o bytecode a mao para exercitar cada opcode de indexacao tipada
// (issue #66, item 1) e o seu fallback, sem depender do compilador; o
// comportamento ponta a ponta (fonte -> compilador -> VM) e coberto em
// typed_index_e2e_test.go.

func typedIndexChunk() *chunk.Chunk {
	code := &chunk.Chunk{}
	arr := code.AddConstant(value.NewArray([]value.Value{value.NewInt(10), value.NewInt(20), value.NewInt(30)}))
	code.Write(byte(chunk.OP_CONSTANT), 1) // slot 0 = array
	code.Write(byte(arr), 1)
	return code
}

func writeConstInt(code *chunk.Chunk, n int64) {
	k := code.AddConstant(value.NewInt(n))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(k), 1)
}

func TestGetLocalIndexArrayReadsInPlace(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 2)
	code.Write(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), 1)
	code.Write(0, 1) // slot do array
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[2]; got.Type != value.VAL_INT || got.Int() != 30 {
		t.Fatalf("esperado 30 no topo, obtido %s", got.String())
	}
	if machine.stackTop != 3 {
		t.Fatalf("stackTop esperado 3 (array, elemento), obtido %d", machine.stackTop)
	}
}

func TestGetLocalIndexArrayErrorsMatchGeneric(t *testing.T) {
	cases := []struct {
		name string
		idx  value.Value
		want string
	}{
		{"fora da faixa", value.NewInt(3), "array index out of bounds"},
		{"negativo", value.NewInt(-1), "array index out of bounds"},
		{"nao inteiro", value.NewString("x"), "array index must be integer"},
	}
	for _, tc := range cases {
		code := typedIndexChunk()
		k := code.AddConstant(tc.idx)
		code.Write(byte(chunk.OP_CONSTANT), 1)
		code.Write(byte(k), 1)
		code.Write(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), 1)
		code.Write(0, 1)
		err := New().Interpret(code)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: esperava %q, obtido %v", tc.name, tc.want, err)
		}
	}
}

// Container que nao e array (null) cai no OP_GET_INDEX generico via
// redispatch: a mensagem e a do generico.
func TestGetLocalIndexArrayFallsBackToGenericForNonArray(t *testing.T) {
	code := &chunk.Chunk{}
	code.Write(byte(chunk.OP_NULL), 1) // slot 0 = null
	writeConstInt(code, 0)
	code.Write(byte(chunk.OP_GET_LOCAL_INDEX_ARRAY), 1)
	code.Write(0, 1)
	err := New().Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "cannot index non-array/map/bytes") {
		t.Fatalf("esperava erro do OP_GET_INDEX generico, obtido %v", err)
	}
}

func TestGetIndexArrayReadsInPlace(t *testing.T) {
	code := typedIndexChunk()
	code.Write(byte(chunk.OP_GET_LOCAL), 1) // empilha o array
	code.Write(0, 1)
	writeConstInt(code, 1)
	code.Write(byte(chunk.OP_GET_INDEX_ARRAY), 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[2]; got.Type != value.VAL_INT || got.Int() != 20 {
		t.Fatalf("esperado 20, obtido %s", got.String())
	}
	if machine.stackTop != 3 {
		t.Fatalf("stackTop esperado 3, obtido %d", machine.stackTop)
	}
}

// Map no lugar do array: redispatch para o generico, que indexa o map.
func TestGetIndexArrayFallsBackToGenericMap(t *testing.T) {
	code := &chunk.Chunk{}
	m := code.AddConstant(value.NewMapWithData(map[string]value.Value{"k": value.NewInt(7)}))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(m), 1)
	key := code.AddConstant(value.NewString("k"))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(key), 1)
	code.Write(byte(chunk.OP_GET_INDEX_ARRAY), 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[1]; got.Type != value.VAL_INT || got.Int() != 7 {
		t.Fatalf("esperado 7 (leitura do map pelo generico), obtido %s", got.String())
	}
}

func TestSetLocalIndexArrayNorcWritesWithoutPushing(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 1)
	writeConstInt(code, 99)
	code.Write(byte(chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	arr := machine.stack[1].Obj.(*value.ObjArray)
	if arr.Elements[1].Int() != 99 {
		t.Fatalf("elemento 1 esperado 99, obtido %s", arr.Elements[1].String())
	}
	if machine.stackTop != 2 {
		t.Fatalf("escrita fundida nao deve empilhar: stackTop esperado 2, obtido %d", machine.stackTop)
	}
}

// Array compartilhado no slot (Owners > 1): a escrita fundida tem de clonar
// como OP_GET_LOCAL_MUT faria — o slot passa a guardar o clone (escrito) e o
// array original fica intacto.
func TestSetLocalIndexArrayNorcClonesSharedArray(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 0)
	writeConstInt(code, 5)
	code.Write(byte(chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	original := code.Constants[0]
	value.Retain(original)
	value.Retain(original) // Owners = 2: compartilhado
	ResetCloneCount()
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava exatamente 1 clone CoW, obtido %d", CloneCountValue())
	}
	if original.Obj.(*value.ObjArray).Elements[0].Int() != 10 {
		t.Fatalf("array compartilhado foi mutado no lugar")
	}
	clone := machine.stack[1].Obj.(*value.ObjArray)
	if clone == original.Obj.(*value.ObjArray) || clone.Elements[0].Int() != 5 {
		t.Fatalf("slot deveria guardar o clone com a escrita: %v", machine.stack[1].String())
	}
}

// Valor composto (vindo por `any`) num NORC: nao pode pular o Retain — cai no
// generico, e o composto ganha um dono (o elemento do array).
func TestSetLocalIndexArrayNorcRetainsCompositeValueViaGeneric(t *testing.T) {
	code := typedIndexChunk()
	writeConstInt(code, 0)
	inner := value.NewArray([]value.Value{value.NewInt(1)})
	k := code.AddConstant(inner)
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(k), 1)
	code.Write(byte(chunk.OP_SET_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(0, 1)
	before := value.OwnersCount(inner)
	if err := New().Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := value.OwnersCount(inner); got != before+1 {
		t.Fatalf("composto escrito em array deve ganhar 1 dono (generico retem): antes %d, depois %d", before, got)
	}
}

func TestSetIndexArrayNorcWritesAndFallsBackForNonArray(t *testing.T) {
	code := typedIndexChunk()
	code.Write(byte(chunk.OP_GET_LOCAL), 1)
	code.Write(0, 1)
	writeConstInt(code, 2)
	writeConstInt(code, -7)
	code.Write(byte(chunk.OP_SET_INDEX_ARRAY_NORC), 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[1].Obj.(*value.ObjArray).Elements[2].Int(); got != -7 {
		t.Fatalf("esperado -7, obtido %d", got)
	}
	if machine.stackTop != 2 {
		t.Fatalf("stackTop esperado 2, obtido %d", machine.stackTop)
	}

	bad := &chunk.Chunk{}
	bad.Write(byte(chunk.OP_NULL), 1)
	writeConstInt(bad, 0)
	writeConstInt(bad, 1)
	bad.Write(byte(chunk.OP_SET_INDEX_ARRAY_NORC), 1)
	err := New().Interpret(bad)
	if err == nil || !strings.Contains(err.Error(), "cannot set index on non-array/map") {
		t.Fatalf("esperava erro do OP_SET_INDEX generico, obtido %v", err)
	}
}

// Slot 1 = ref (REF_UPVALUE via OP_REF_LOCAL) para o slot 0 (array).
func refTypedIndexChunk() *chunk.Chunk {
	code := typedIndexChunk()
	code.Write(byte(chunk.OP_REF_LOCAL), 1)
	code.Write(0, 1)
	return code
}

func TestGetRefLocalIndexArrayResolvesUpvalueRef(t *testing.T) {
	code := refTypedIndexChunk()
	writeConstInt(code, 1)
	code.Write(byte(chunk.OP_GET_REF_LOCAL_INDEX_ARRAY), 1)
	code.Write(1, 1) // slot do ref
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[3]; got.Type != value.VAL_INT || got.Int() != 20 {
		t.Fatalf("esperado 20, obtido %s", got.String())
	}
}

// Slot de tipo estatico ref que guarda null (parametro `ref T[]` recebendo
// null): desde a revisao final da issue #82 (I5) o OP_DEREF do caminho
// generico erra em vez de passar o null adiante — a forma fundida tem de
// produzir a MESMA mensagem, e o teste compara as duas.
func TestGetRefLocalIndexArrayNullRefMatchesGeneric(t *testing.T) {
	fused := &chunk.Chunk{}
	fused.Write(byte(chunk.OP_NULL), 1) // slot 0 = null
	writeConstInt(fused, 0)
	fused.Write(byte(chunk.OP_GET_REF_LOCAL_INDEX_ARRAY), 1)
	fused.Write(0, 1)
	fusedErr := New().Interpret(fused)

	// Caminho generico: GET_LOCAL + OP_DEREF + indice + GET_INDEX.
	generic := &chunk.Chunk{}
	generic.Write(byte(chunk.OP_NULL), 1) // slot 0 = null
	generic.Write(byte(chunk.OP_GET_LOCAL), 1)
	generic.Write(0, 1)
	generic.Write(byte(chunk.OP_DEREF), 1)
	writeConstInt(generic, 0)
	generic.Write(byte(chunk.OP_GET_INDEX), 1)
	genericErr := New().Interpret(generic)

	if fusedErr == nil || genericErr == nil {
		t.Fatalf("ambos os caminhos deveriam errar: fundido=%v generico=%v", fusedErr, genericErr)
	}
	if !strings.Contains(fusedErr.Error(), "cannot dereference null reference") {
		t.Fatalf("fundido: %v, esperava 'cannot dereference null reference'", fusedErr)
	}
	if !strings.Contains(genericErr.Error(), "cannot dereference null reference") {
		t.Fatalf("generico: %v, esperava 'cannot dereference null reference'", genericErr)
	}
}

// Escrita: o generico e GET_LOCAL_MUT_BORROW + DEREF_MUT, que recusa gravar
// atraves de ref nulo; a forma fundida acompanha.
func TestSetRefLocalIndexArrayNorcNullRefMatchesGeneric(t *testing.T) {
	fused := &chunk.Chunk{}
	fused.Write(byte(chunk.OP_NULL), 1)
	writeConstInt(fused, 0)
	writeConstInt(fused, 1)
	fused.Write(byte(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC), 1)
	fused.Write(0, 1)
	fusedErr := New().Interpret(fused)

	generic := &chunk.Chunk{}
	generic.Write(byte(chunk.OP_NULL), 1)
	generic.Write(byte(chunk.OP_GET_LOCAL_MUT_BORROW), 1)
	generic.Write(0, 1)
	generic.Write(byte(chunk.OP_DEREF_MUT), 1)
	genericErr := New().Interpret(generic)

	if fusedErr == nil || genericErr == nil {
		t.Fatalf("ambos os caminhos deveriam errar: fundido=%v generico=%v", fusedErr, genericErr)
	}
	if !strings.Contains(fusedErr.Error(), "cannot write through a null reference") {
		t.Fatalf("fundido: %v, esperava 'cannot write through a null reference'", fusedErr)
	}
	if !strings.Contains(genericErr.Error(), "cannot write through a null reference") {
		t.Fatalf("generico: %v, esperava 'cannot write through a null reference'", genericErr)
	}
}

func TestSetRefLocalIndexArrayNorcWritesThroughRef(t *testing.T) {
	code := refTypedIndexChunk()
	writeConstInt(code, 0)
	writeConstInt(code, 42)
	code.Write(byte(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(1, 1)
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if got := machine.stack[1].Obj.(*value.ObjArray).Elements[0].Int(); got != 42 {
		t.Fatalf("escrita via ref esperada 42, obtido %d", got)
	}
	if machine.stackTop != 3 {
		t.Fatalf("stackTop esperado 3 (array, ref), obtido %d", machine.stackTop)
	}
}

// Ref para array compartilhado: clona e grava o clone DE VOLTA pelo ref
// (unicizeThroughRefValue), como a sequencia GET_LOCAL_MUT_BORROW + DEREF_MUT.
func TestSetRefLocalIndexArrayNorcClonesSharedThroughRef(t *testing.T) {
	code := refTypedIndexChunk()
	writeConstInt(code, 0)
	writeConstInt(code, 42)
	code.Write(byte(chunk.OP_SET_REF_LOCAL_INDEX_ARRAY_NORC), 1)
	code.Write(1, 1)
	original := code.Constants[0]
	value.Retain(original)
	value.Retain(original)
	ResetCloneCount()
	machine := New()
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	if CloneCountValue() != 1 {
		t.Fatalf("esperava 1 clone, obtido %d", CloneCountValue())
	}
	if original.Obj.(*value.ObjArray).Elements[0].Int() != 10 {
		t.Fatalf("array compartilhado foi mutado no lugar atraves do ref")
	}
	if got := machine.stack[1].Obj.(*value.ObjArray); got == original.Obj.(*value.ObjArray) || got.Elements[0].Int() != 42 {
		t.Fatalf("o slot apontado pelo ref deveria guardar o clone escrito")
	}
}
