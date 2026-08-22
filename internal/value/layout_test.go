package value

import (
	"math"
	"testing"
	"unsafe"
)

// O tamanho do Value e o custo de cada push/pop e de cada copia de operando
// em run(): 48 B (tag int + bool + int64 + float64 + interface) virou 32 B na
// fase 2 de perf (issue #37, estagio 1). Este teste e a assercao executavel
// do layout — se alguem acrescentar um campo, o build avisa aqui, nao num
// benchmark meses depois.
func TestValueIs32Bytes(t *testing.T) {
	if got := unsafe.Sizeof(Value{}); got != 32 {
		t.Fatalf("unsafe.Sizeof(Value{}) = %d, esperado 32 (ver spec 2026-08-22-vm-perf-fase2-value-layout)", got)
	}
	if got := unsafe.Sizeof(ValueType(0)); got != 1 {
		t.Fatalf("ValueType deve ocupar 1 byte, ocupa %d", got)
	}
}

func TestValueAccessorsRoundTrip(t *testing.T) {
	if got := NewInt(-42).Int(); got != -42 {
		t.Fatalf("Int(): %d", got)
	}
	if got := NewInt(math.MaxInt64).Int(); got != math.MaxInt64 {
		t.Fatalf("Int() MaxInt64: %d", got)
	}
	if got := NewInt(math.MinInt64).Int(); got != math.MinInt64 {
		t.Fatalf("Int() MinInt64: %d", got)
	}
	if got := NewFloat(3.5).Float(); got != 3.5 {
		t.Fatalf("Float(): %v", got)
	}
	if got := NewFloat(math.Inf(-1)).Float(); !math.IsInf(got, -1) {
		t.Fatalf("Float() -Inf: %v", got)
	}
	if got := NewFloat(math.NaN()).Float(); !math.IsNaN(got) {
		t.Fatalf("Float() NaN: %v", got)
	}
	if got := NewFloat(math.Copysign(0, -1)).Float(); !math.Signbit(got) {
		t.Fatalf("Float() -0: perdeu o sinal")
	}
	if !NewBool(true).Bool() || NewBool(false).Bool() {
		t.Fatal("Bool() nao faz round-trip")
	}
	var zero Value
	if zero.Type != VAL_BOOL || zero.Bool() || zero.Int() != 0 || zero.Obj != nil {
		t.Fatalf("zero value deve continuar sendo VAL_BOOL false: %+v", zero)
	}
	v := NewInt(1)
	v.SetInt(41)
	v.SetInt(v.Int() + 1)
	if v.Type != VAL_INT || v.Int() != 42 {
		t.Fatalf("SetInt: %+v", v)
	}
}
