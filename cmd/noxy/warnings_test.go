package main

import (
	"strings"
	"testing"
)

// Issue #61 item 3: aviso do compilador ("rebinding ref parameter") e
// diagnostico — sai em diagOut, nunca em stdout (AGENTS.md, regra "Saida"), e nao muda
// o codigo de saida nem a saida do programa. Vale para `noxy arquivo.nx` e
// para o REPL.

const rebindWarningProgram = "func f(r: ref int)\n    let y: int = 10\n    r = ref y\nend\nlet x: int = 1\nf(ref x)\nprint(x)\n"

func TestCompilerWarningsGoToDiagOutNotStdout(t *testing.T) {
	diag := withDiagBuffer(t)
	var code int
	stdout := captureStdout(t, func() { code = runWithConfig("warn.nx", rebindWarningProgram, ".", false) })
	if code != 0 {
		t.Fatalf("exit code=%d, want 0 (aviso nao e erro)", code)
	}
	want := "warning: rebinding ref parameter 'r' has no effect outside function\n  --> warn.nx:3\n"
	if !strings.Contains(diag.String(), want) {
		t.Fatalf("diagnostics=%q, want %q", diag.String(), want)
	}
	if stdout != "1\n" {
		t.Fatalf("stdout=%q, want so a saida do programa", stdout)
	}
}

func TestREPLCompilerWarningsGoToDiagOut(t *testing.T) {
	diag := withDiagBuffer(t)
	src := &fakeLines{steps: []fakeLine{
		{text: "func f(r: ref int)"},
		{text: "let y: int = 10"},
		{text: "r = ref y"},
		{text: "end"},
		{text: "print(7)"},
	}}
	stdout := captureStdout(t, func() { runREPL(src, ">>> ", "... ", false) })
	if !strings.Contains(diag.String(), "warning: rebinding ref parameter 'r' has no effect outside function") || !strings.Contains(diag.String(), "--> REPL:3") {
		t.Fatalf("diagnostics=%q", diag.String())
	}
	if strings.Contains(stdout, "warning") || !strings.Contains(stdout, "7\n") {
		t.Fatalf("stdout=%q", stdout)
	}
}
