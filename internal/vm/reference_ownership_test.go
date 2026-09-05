package vm

import (
	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/value"
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
	captured := code.AddConstant(value.NewString("captured"))
	// OP_REF_GLOBAL and OP_SET_GLOBAL's constant-pool index is a 16-bit
	// big-endian operand (see emitOpWithConstantIndex), so each name/captured
	// index below is written as two bytes rather than one.
	for _, instruction := range []byte{
		byte(chunk.OP_REF_GLOBAL), byte((name >> 8) & 0xff), byte(name & 0xff),
		byte(chunk.OP_DEREF),
		byte(chunk.OP_SET_GLOBAL), byte((captured >> 8) & 0xff), byte(captured & 0xff),
		byte(chunk.OP_POP),
		byte(chunk.OP_NULL),
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
	got := moduleGlobals["captured"]
	if got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("shared fallback=%v, want 42", got)
	}
}

func TestInterpretWithGlobalsUsesNonNilEmptyEnvironment(t *testing.T) {
	code := compileVMSource(t, "let answer: int = 42")
	globals := map[string]value.Value{}
	if err := New().InterpretWithGlobals(code, globals); err != nil {
		t.Fatal(err)
	}
	if got := globals["answer"]; got.Type != value.VAL_INT || got.Int() != 42 {
		t.Fatalf("answer=%v", got)
	}
}

func TestModuleGlobalReferenceRetainsResolvedEnvironment(t *testing.T) {
	root := value.NewGlobalEnvironment(nil)
	root.SetLocal("root", value.NewInt(1))
	module := value.NewGlobalEnvironment(root)
	module.SetLocal("module", value.NewInt(2))
	machine := New()
	machine.shared.Root = root
	ref := &value.ObjRef{RefType: value.REF_GLOBAL, Name: "module", GlobalOwner: module}
	if err := machine.storeReferenceValue(value.Value{Type: value.VAL_REF, Obj: ref}, value.NewInt(3)); err != nil {
		t.Fatal(err)
	}
	got, _ := module.GetLocal("module")
	if got.Int() != 3 {
		t.Fatalf("module value=%v", got)
	}
}
