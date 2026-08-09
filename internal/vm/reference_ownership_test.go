package vm

import (
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
	"testing"
)

func TestImportedFunctionUsesCallerGlobalReferenceOwner(t *testing.T) {
	got := runTypedFunctionProgram(t, `
use rand
let rng: rand.RandomGenerator = rand.RandomGenerator(1, 2, 3, 100)
rand.rng.state = 10
rand.new_random(ref rng)
test_report(rng.state * 100 + rand.rng.state)`)
	testExpectedObject(t, 510, got)
}

func TestModuleFrameGlobalReferenceCapturesSharedFallbackOwner(t *testing.T) {
	code := chunk.New()
	code.FileName = "shared_global_reference"
	name := code.AddConstant(value.NewString("shared_value"))
	for _, instruction := range []byte{
		byte(chunk.OP_REF_GLOBAL), byte(name),
		byte(chunk.OP_DEREF),
		byte(chunk.OP_RETURN),
	} {
		code.Write(instruction, 1)
	}

	machine := New()
	machine.SetGlobal("shared_value", value.NewInt(42))
	moduleGlobals := map[string]value.Value{"module_only": value.NewInt(1)}
	if err := machine.InterpretWithGlobals(code, moduleGlobals); err != nil {
		t.Fatal(err)
	}
	got := machine.pop()
	if got.Type != value.VAL_INT || got.AsInt != 42 {
		t.Fatalf("shared fallback=%v, want 42", got)
	}
}
