package vm

import (
	"noxy-vm/internal/value"
	"strings"
	"testing"
)

func runTypedFunctionProgram(t *testing.T, input string) value.Value {
	t.Helper()
	return captureVMSource(t, input)
}

func runTypedFunctionProgramError(t *testing.T, input string) error {
	t.Helper()
	return interpretVMSource(t, New(), input)
}

func TestDynamicCallRejectsPlainValueForReferenceParameter(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func increment(value: ref int) -> void
    return
end
let dynamic: func = increment
let answer: int = 41
dynamic(answer)`)
	if err == nil || !strings.Contains(err.Error(), "argument 1: expected ref int, got int") {
		t.Fatalf("error=%v", err)
	}
}

func TestDynamicCallRejectsReferenceForValueParameter(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func consume(value: int) -> void
    return
end
let dynamic: func = consume
let answer: int = 41
dynamic(ref answer)`)
	if err == nil || !strings.Contains(err.Error(), "argument 1: expected int, got ref") {
		t.Fatalf("error=%v", err)
	}
}

func TestDynamicReferenceParameterAcceptsNull(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
func consume(value: ref int) -> void
    return
end
let dynamic: func = consume
dynamic(null)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestExactCallableReferenceParameterReceivesNullValue(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func accept(value: ref func() -> int) -> void
    test_report(ref value)
end
accept(null)`)
	if got.Type != value.VAL_NULL {
		t.Fatalf("argument=%v (%v), want VAL_NULL", got, got.Type)
	}
	if got.Obj != nil {
		t.Fatalf("null argument object=%T, want nil (no ObjRef)", got.Obj)
	}
}

func TestUpdatingNullReferenceFailsClearly(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
let pointer: ref int = null
*pointer = 1`)
	if err == nil || !strings.Contains(err.Error(), "cannot update null reference") {
		t.Fatalf("error=%v", err)
	}
}

func TestExecutesExactHigherOrderFunction(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func add(a: int, b: int) -> int
    return a + b
end
func apply(f: func(int, int) -> int, a: int, b: int) -> int
    return f(a, b)
end
test_report(apply(add, 20, 22))`)
	testExpectedObject(t, 42, got)
}

func TestExecutesExactClosureReturn(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func makeAdder(base: int) -> func(int) -> int
    return func(value: int) -> int
        return base + value
    end
end
let add10: func(int) -> int = makeAdder(10)
test_report(add10(5))`)
	testExpectedObject(t, 15, got)
}

func TestExecutesExactReferenceArgument(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func increment(value: ref int) -> void
    *value = *value + 1
end
let answer: int = 41
increment(answer)
test_report(answer)`)
	testExpectedObject(t, 42, got)
}

// Spec §2.3 regra 2 / §4.2: um campo `ref Node` nulo passado a parametro
// `ref Node` e encaminhado como null (`node == null` dentro da funcao). Para
// preencher o campo, a funcao recebe o PAI e liga `parent.next = ref novo`
// — o padrao fill-null-slot (`*node == null` / `*node = Node(...)` sobre um
// slot recebido contextualmente) deixou de existir.
func TestReferenceFieldArgumentForwardsNullSlot(t *testing.T) {
	got := runTypedFunctionProgram(t, `
struct Node
    value: int
    next: ref Node
end
func is_null(node: ref Node) -> bool
    return node == null
end
func fill(parent: ref Node) -> void
    if parent.next == null then
        let novo: Node = Node(42, null)
        parent.next = ref novo
    end
end
let head: Node = Node(1, null)
let was_null: bool = is_null(head.next)
fill(head)
if was_null then
    test_report(head.next.value)
else
    test_report(0)
end`)
	testExpectedObject(t, 42, got)
}

func TestWritingThroughForwardedNullFieldIsRuntimeError(t *testing.T) {
	err := runTypedFunctionProgramError(t, `
struct Node
    value: int
    next: ref Node
end
func fill(node: ref Node) -> void
    *node = Node(42, null)
end
let head: Node = Node(1, null)
fill(head.next)`)
	if err == nil || !strings.Contains(err.Error(), "cannot update null reference") {
		t.Fatalf("error=%v", err)
	}
}

func TestExplicitReferenceSurvivesBareFunctionCall(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func increment(value: ref int) -> void
    *value = *value + 1
end
let dynamic: func = increment
let answer: int = 41
dynamic(ref answer)
test_report(answer)`)
	testExpectedObject(t, 42, got)
}

func TestContextualReferenceUpdatesClosedUpvalue(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func increment(value: ref int) -> void
    *value = *value + 1
end
func make_incrementer() -> func() -> int
    let value: int = 40
    return func() -> int
        increment(value)
        return value
    end
end
let incrementer: func() -> int = make_incrementer()
incrementer()
test_report(incrementer())`)
	testExpectedObject(t, 42, got)
}

func TestExplicitReferenceToClosedUpvalueCanEscape(t *testing.T) {
	got := runTypedFunctionProgram(t, `
func make_reference_factory() -> func() -> ref int
    let value: int = 41
    return func() -> ref int
        return ref value
    end
end
let get_reference: func() -> ref int = make_reference_factory()
let pointer: ref int = get_reference()
*pointer = 42
test_report(*pointer)`)
	testExpectedObject(t, 42, got)
}

func TestContextualReferenceAcceptsReferenceReturningCall(t *testing.T) {
	got := runTypedFunctionProgram(t, `
let answer: int = 41
func get_reference() -> ref int
    return ref answer
end
func increment(value: ref int) -> void
    *value = *value + 1
end
increment(get_reference())
test_report(answer)`)
	testExpectedObject(t, 42, got)
}
