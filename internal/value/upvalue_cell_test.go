package value

import "testing"

// A celula fechada e a "variavel anonima na heap" que json_loads usa para
// preencher um slot `ref T` nulo: possuidora, desligada da pilha, com
// Load/Store funcionando como numa caixa fechada por closeUpvalue.
func TestNewClosedUpvalueIsDetachedOwnedCell(t *testing.T) {
	cell := NewClosedUpvalue(NewInt(42))
	if !cell.IsValid() {
		t.Fatal("celula fechada deve ser valida")
	}
	if cell.IsBorrowed() {
		t.Fatal("celula fechada e possuidora, nao emprestada")
	}
	got, ok := cell.Load()
	if !ok || got.Type != VAL_INT || got.AsInt != 42 {
		t.Fatalf("Load = %#v, %v; esperado 42", got, ok)
	}
	var stackSlot Value
	if cell.PointsTo(&stackSlot) {
		t.Fatal("celula fechada nunca aponta para um slot de pilha")
	}
	if !cell.Store(NewInt(7)) {
		t.Fatal("Store deve funcionar numa celula fechada")
	}
	if got, _ := cell.Load(); got.AsInt != 7 {
		t.Fatalf("Load apos Store = %d, esperado 7", got.AsInt)
	}
}
