package vm

import (
	"strings"
	"testing"
)

// Revisao do PR #119, condicao 2 (furo residual): `any` pode carregar uma
// referencia (R2) e entrar num parametro/slot `ref T` — R5 diz que o modo e
// validado em runtime — mas ninguem conferia o TIPO DO ALVO: `inc(a)` com a
// guardando `ref string` lia a string como int. Agora a chamada (OP_CALL,
// modo nao provado) e o slot (`let`/`return`, via OP_MARK) conferem o alvo;
// um alvo null segue encaminhado (ref-null-forwarding) e falha na leitura.

const anyRefTexto = "let s: string = \"texto\"\nlet a: any = ref s\n"

func TestAnyRefArgumentTargetTypeIsCheckedAtCall(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "func inc(r: ref int) -> int\n    return *r + 1\nend\n"+anyRefTexto+"inc(a)\n")
	if err == nil || !strings.Contains(err.Error(), "function 'inc' argument 1: expected ref int, got ref string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyRefArgumentWithMatchingTargetPasses(t *testing.T) {
	got := captureVMSource(t, "func inc(r: ref int) -> int\n    *r = *r + 1\n    return *r\nend\nlet n: int = 41\nlet a: any = ref n\ntest_report(inc(a) * 100 + n)\n")
	testExpectedObject(t, 42*100+42, got)
}

func TestAnyRefArgumentNullTargetIsStillForwarded(t *testing.T) {
	// Convencao ref-null-forwarding: o null chega ao callee e falha na leitura.
	err := interpretOrCompileErr(t, New(), "struct Node\n    next: ref Node?\nend\nfunc le(r: ref Node) -> int\n    return 1\nend\nlet n: Node = Node(null)\nlet a: any = n.next\nlet z: int = le(a)\nprint(z)\n")
	if err != nil {
		t.Fatalf("a null target must be forwarded, not reported as a target-type mismatch, got %v", err)
	}
}

func TestAnyRefLetTargetTypeIsChecked(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyRefTexto+"let r: ref int = a\nprint(*r)\n")
	if err == nil || !strings.Contains(err.Error(), "expected ref int, got ref string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyRefReturnTargetTypeIsChecked(t *testing.T) {
	err := interpretOrCompileErr(t, New(), anyRefTexto+"func pega() -> ref int\n    return a\nend\nlet r: ref int = pega()\nprint(*r)\n")
	if err == nil || !strings.Contains(err.Error(), "expected ref int, got ref string") {
		t.Fatalf("error=%v", err)
	}
}

func TestAnyRefNullableParameterTargetTypeIsChecked(t *testing.T) {
	err := interpretOrCompileErr(t, New(), "func inc(r: ref int?) -> int\n    return 1\nend\n"+anyRefTexto+"inc(a)\n")
	if err == nil || !strings.Contains(err.Error(), "function 'inc' argument 1: expected ref int?, got ref string") {
		t.Fatalf("error=%v", err)
	}
}
