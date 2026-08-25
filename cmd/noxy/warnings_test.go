package main

import (
	"strings"
	"testing"
)

// Issue #61 item 3: aviso do compilador ("rebinding ref parameter") e
// diagnostico — sai em diagOut, nunca em stdout (AGENTS.md E.6), e nao muda
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

// R12 (issue #83): o aviso de emprestimo guardado usa o MESMO canal — diagOut,
// exit 0, stdout so com a saida do programa. Etapa 1 do rollout (spec §9.2):
// enquanto for aviso, nenhum programa existente quebra.
const keptBorrowWarningProgram = "let g: ref int = null\nfunc keep(r: ref int) -> void\n    g = r\nend\n" +
	"let arr: int[] = [1, 2, 3]\nkeep(ref arr[0])\nprint(*g)\n"

func TestKeptBorrowWarningGoesToDiagOutNotStdout(t *testing.T) {
	diag := withDiagBuffer(t)
	var code int
	stdout := captureStdout(t, func() { code = runWithConfig("kept.nx", keptBorrowWarningProgram, ".", false) })
	if code != 0 {
		t.Fatalf("exit code=%d, want 0 (aviso nao e erro)", code)
	}
	want := "warning: parameter 'r' is a borrow and cannot be kept: it is stored in 'g'"
	if !strings.Contains(diag.String(), want) {
		t.Fatalf("diagnostics=%q, want %q", diag.String(), want)
	}
	if !strings.Contains(diag.String(), "--> kept.nx:3") {
		t.Fatalf("o aviso tem de apontar a linha ofensora DENTRO do callee; diagnostics=%q", diag.String())
	}
	if strings.Contains(stdout, "warning") || stdout != "1\n" {
		t.Fatalf("stdout=%q, want so a saida do programa", stdout)
	}
}
