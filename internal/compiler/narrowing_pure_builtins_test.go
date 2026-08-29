package compiler

import (
	"strings"
	"testing"
)

// Issue #118 item 2: uma chamada a builtin central (print, to_str, length...)
// nao encerra o narrowing — sao nativos que nunca executam codigo Noxy, entao
// nada alcanca a raiz durante a chamada. A f-string vira `to_str(...)` no
// parser, logo `f"{m['a']} {m['b']}"` sobre um global estreitado compila.

const narrowingGlobalMapPrelude = "func busca() -> map[string, any]?\n    return {\"nome\": \"Ana\", \"idade\": 30}\nend\nlet m: map[string, any]? = busca()\n"

func TestNarrowingGlobalSurvivesFStringInterpolations(t *testing.T) {
	src := narrowingGlobalMapPrelude + "if m != null then\n    print(f\"{m['nome']} {m['idade']}\")\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingGlobalSurvivesCoreBuiltinCalls(t *testing.T) {
	src := narrowingGlobalMapPrelude + "if m != null then\n    print(to_str(m[\"nome\"]) + \" \" + to_str(m[\"idade\"]))\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingGlobalSurvivesPrintBetweenUses(t *testing.T) {
	src := narrowingGlobalMapPrelude + "if m != null then\n    print(m[\"nome\"])\n    print(m[\"idade\"])\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingGlobalSurvivesLoopWithOnlyCoreBuiltinCalls(t *testing.T) {
	src := narrowingGlobalMapPrelude + "if m != null then\n    let i: int = 0\n    while i < 2 do\n        print(m[\"nome\"])\n        i = i + 1\n    end\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingGlobalLostAfterUserFunctionCall(t *testing.T) {
	src := narrowingGlobalMapPrelude + "func toca() -> void\n    m = null\nend\nif m != null then\n    toca()\n    print(m[\"nome\"])\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'m' may be null") {
		t.Fatalf("want 'm' may be null, got %v", err)
	}
}

func TestNarrowingShadowedBuiltinEndsNarrowing(t *testing.T) {
	// `to_str` declarado pelo programa nao e o nativo puro: pode reatribuir m.
	src := narrowingGlobalMapPrelude + "func to_str(v: any) -> string\n    m = null\n    return \"x\"\nend\nif m != null then\n    print(to_str(m[\"nome\"]) + to_str(m[\"idade\"]))\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'m' may be null") {
		t.Fatalf("want 'm' may be null, got %v", err)
	}
}

// Issue #118 item 3: quando o fato existiu e foi derrubado por uma chamada, o
// diagnostico diz isso — e nao sugere o `if` que ja esta ali.

func TestNarrowingLostAfterCallDiagnosticNamesGlobalRoot(t *testing.T) {
	src := narrowingGlobalMapPrelude + "func toca() -> void\n    m = null\nend\nif m != null then\n    toca()\n    print(m[\"nome\"])\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "[line 10] 'm' may be null: it was tested, but 'm' is a global and a call came between the test and this use\n  hint: test it again after the call, bind it first ('let v = m' before the 'if') and use 'v', or move the code into a function"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNarrowingLostAfterCallDiagnosticNamesRefRoot(t *testing.T) {
	src := "struct Node\n    valor: int\n    prox: Node?\nend\nfunc toca(n: ref Node) -> void\n    n.prox = null\nend\nfunc f(no: ref Node) -> int\n    if no.prox != null then\n        toca(no)\n        return no.prox.valor\n    end\n    return 0\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "'no.prox' may be null: it was tested, but 'no' is a ref and a call came between the test and this use\n  hint: test it again after the call, or bind it first ('let v = no.prox' before the 'if') and use 'v'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNarrowingLostDiagnosticDoesNotLeakOutsideTheBranch(t *testing.T) {
	// Fora do `if` nunca houve fato: a mensagem e a comum, nao a de fato perdido.
	src := narrowingGlobalMapPrelude + "func toca() -> void\n    m = null\nend\nif m != null then\n    toca()\nend\nprint(m[\"nome\"])\n"
	_, err := compileFunctionSource(t, src)
	want := "'m' may be null; test it first\n  hint: use 'if m != null then ... end'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNarrowingLostInLoopDiagnosticNamesTheLoop(t *testing.T) {
	// O fato cai na ENTRADA do laco (dropForLoop): a chamada vem depois do
	// uso no texto, mas roda antes dele na iteracao seguinte — a mensagem
	// nao pode dizer "a call came between the test and this use".
	src := narrowingGlobalMapPrelude + "func toca() -> void\n    m = null\nend\nif m != null then\n    let i: int = 0\n    while i < 2 do\n        print(m[\"nome\"])\n        toca()\n        i = i + 1\n    end\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "[line 11] 'm' may be null: it was tested, but 'm' is a global and the loop body calls a function that can run before this use\n  hint: test it again inside the loop, bind it first ('let v = m' before the 'if') and use 'v', or move the code into a function"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

func TestNarrowingLostDiagnosticClearedWhenTestedAgain(t *testing.T) {
	src := narrowingGlobalMapPrelude + "func toca() -> void\n    m = null\nend\nif m != null then\n    toca()\n    if m != null then\n        print(m[\"nome\"])\n    end\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}
