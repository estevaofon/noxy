package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

// Frame raiz: LocalBase = 1, slot local 0 = stack[1]. Empilha 5 no slot e
// aplica dois incrementos fundidos (+3, -1). Sem OP_RETURN de proposito.
func TestIncLocalInt(t *testing.T) {
	machine := New()
	code := &chunk.Chunk{}
	five := code.AddConstant(value.NewInt(5))
	code.Write(byte(chunk.OP_CONSTANT), 1)
	code.Write(byte(five), 1)
	code.Write(byte(chunk.OP_INC_LOCAL_INT), 1)
	code.Write(0, 1)             // slot
	code.Write(byte(int8(3)), 1) // delta +3
	code.Write(byte(chunk.OP_INC_LOCAL_INT), 1)
	code.Write(0, 1) // slot
	// delta -1: byte(int8(-1)) direto é constante e o compilador rejeita a
	// conversão (overflow de byte); passar por uma variável forca a conversão
	// em tempo de execucao, que faz o wrap esperado (-1 -> 0xFF).
	negOne := int8(-1)
	code.Write(byte(negOne), 1) // delta -1
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	got := machine.stack[1]
	if got.Type != value.VAL_INT || got.AsInt != 7 {
		t.Fatalf("esperado slot=7, obtido %s", got.String())
	}
}

// TestIncLocalIntBehavior confirma, ponta a ponta (fonte -> compilador ->
// VM), que a fusao produz o mesmo resultado que o caminho generico: dois
// locais int possuidores incrementados/decrementados dentro de um while.
func TestIncLocalIntBehavior(t *testing.T) {
	result := captureVMSource(t, `
func count() -> int
    let i: int = 0
    let downs: int = 100
    while i < 10 do
        i = i + 1
        downs = downs - 2
    end
    return i * 1000 + downs
end
test_report(count())
`)
	if result.Type != value.VAL_INT || result.AsInt != 10080 {
		t.Fatalf("esperado 10080 (i=10, downs=80), obtido %s", result.String())
	}
}

// TestIncLocalIntAfterClosureCapture cobre exatamente o cenario de risco do
// upvalue: uma closure abre um upvalue SOBRE o slot de `i` (aponta
// diretamente para vm.stack[LocalBase+slot] enquanto aberto — ver
// vm.captureUpvalue/ObjUpvalue.location) e so DEPOIS disso `i` sofre um
// segundo incremento fundido. OP_INC_LOCAL_INT escreve no mesmo indice de
// pilha que OP_SET_LOCAL usaria — a mesma celula que o upvalue aberto
// enxerga por ponteiro — entao a closure deve ver o valor pos-incremento
// (7), nao o valor no momento da captura (5). Um resultado de 5 aqui
// revelaria que a fusao bypassa a memoria compartilhada com upvalues
// abertos.
func TestIncLocalIntAfterClosureCapture(t *testing.T) {
	result := captureVMSource(t, `
func make() -> int
    let i: int = 0
    i = i + 5
    let get: func() -> int = func() -> int
        return i
    end
    i = i + 2
    return get()
end
test_report(make())
`)
	if result.Type != value.VAL_INT || result.AsInt != 7 {
		t.Fatalf("esperado 7 (closure deve ver escrita pos-captura via fusao), obtido %s", result.String())
	}
}
