package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/lineedit"
)

func captureStdout(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = writer
	run()
	_ = writer.Close()
	os.Stdout = previous
	out, _ := io.ReadAll(reader)
	return string(out)
}

func withDiagBuffer(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := diagOut
	diagOut = &buffer
	t.Cleanup(func() { diagOut = previous })
	return &buffer
}

func TestDiagnosticsGoToStderrWriterAndExitCodeIsOne(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"runtime", "print(\"antes\")\nlet z: int = 0\nprint(1 / z)\n", "Runtime error:"},
		{"compiler", "let x: int = \"s\"\n", "Compiler error:"},
		{"parser", "let x: int = \n", "SyntaxError"},
	}
	for _, tc := range cases {
		diag := withDiagBuffer(t)
		var code int
		stdout := captureStdout(t, func() { code = runWithConfig(tc.name+".nx", tc.source, ".", false) })
		if code != 1 {
			t.Fatalf("%s: exit code=%d, want 1", tc.name, code)
		}
		if !strings.Contains(diag.String(), tc.want) {
			t.Fatalf("%s: diagnostics=%q, want %q", tc.name, diag.String(), tc.want)
		}
		if strings.Contains(stdout, tc.want) {
			t.Fatalf("%s: diagnostic leaked to stdout: %q", tc.name, stdout)
		}
	}
}

func TestLoadScriptMissingFileReportsAndFails(t *testing.T) {
	diag := withDiagBuffer(t)
	if _, ok := loadScript("nao_existe_56.nx"); ok {
		t.Fatal("missing file should not load")
	}
	if !strings.Contains(diag.String(), "Error reading file:") {
		t.Fatalf("diagnostics=%q", diag.String())
	}
}

// fakeLine e um passo de entrada do REPL: o texto digitado ou o erro que a
// fonte devolveria (Ctrl-C, EOF).
type fakeLine struct {
	text string
	err  error
}

// fakeLines simula a fonte de linhas do REPL e registra os prompts pedidos,
// para testar o loop sem tty nem editor.
type fakeLines struct {
	steps   []fakeLine
	prompts []string
}

func (f *fakeLines) ReadLine(prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	if len(f.steps) == 0 {
		return "", io.EOF
	}
	step := f.steps[0]
	f.steps = f.steps[1:]
	return step.text, step.err
}

func TestREPLAsksContinuationPromptWhileInputIsIncomplete(t *testing.T) {
	src := &fakeLines{steps: []fakeLine{{text: "if true then"}, {text: "print(1)"}, {text: "end"}}}
	stdout := captureStdout(t, func() { runREPL(src, ">>> ", "... ", false) })
	if !strings.Contains(stdout, "1\n") {
		t.Fatalf("block did not run; stdout=%q", stdout)
	}
	want := []string{">>> ", "... ", "... ", ">>> "}
	if strings.Join(src.prompts, "|") != strings.Join(want, "|") {
		t.Fatalf("prompts = %q, want %q", src.prompts, want)
	}
}

// Ctrl-C no prompt encerra o REPL, como no Windows (console cooked) e como
// era no Unix antes do editor de linha (SIGINT matava o processo): o loop
// para na hora e devolve ErrInterrupt para o chamador sair com 130.
func TestREPLInterruptStopsTheLoopAndReportsIt(t *testing.T) {
	src := &fakeLines{steps: []fakeLine{
		{text: "print(1)"},
		{err: lineedit.ErrInterrupt},
		{text: "print(2)"},
	}}
	var err error
	stdout := captureStdout(t, func() { err = runREPL(src, ">>> ", "... ", false) })
	if !errors.Is(err, lineedit.ErrInterrupt) {
		t.Fatalf("runREPL err = %v, want ErrInterrupt", err)
	}
	if !strings.Contains(stdout, "1\n") || strings.Contains(stdout, "2\n") {
		t.Fatalf("Ctrl-C must stop the loop; stdout=%q", stdout)
	}
	if len(src.prompts) != 2 {
		t.Fatalf("prompts after interrupt = %q, want exactly 2 reads", src.prompts)
	}
}

func TestREPLStopsOnEOFAndOnExit(t *testing.T) {
	src := &fakeLines{steps: []fakeLine{{text: "print(3)"}, {text: "exit"}, {text: "print(4)"}}}
	var err error
	stdout := captureStdout(t, func() { err = runREPL(src, ">>> ", "... ", false) })
	if err != nil {
		t.Fatalf("exit: runREPL err = %v, want nil", err)
	}
	if !strings.Contains(stdout, "3\n") || strings.Contains(stdout, "4\n") {
		t.Fatalf("exit must stop the loop; stdout=%q", stdout)
	}
	src = &fakeLines{steps: []fakeLine{{text: "print(5)"}, {err: io.EOF}, {text: "print(6)"}}}
	stdout = captureStdout(t, func() { err = runREPL(src, ">>> ", "... ", false) })
	if err != nil {
		t.Fatalf("EOF: runREPL err = %v, want nil", err)
	}
	if !strings.Contains(stdout, "5\n") || strings.Contains(stdout, "6\n") {
		t.Fatalf("EOF must stop the loop; stdout=%q", stdout)
	}
}

func TestREPLExitCodeIs130OnInterruptAndZeroOtherwise(t *testing.T) {
	if got := replExitCode(nil); got != 0 {
		t.Fatalf("replExitCode(nil) = %d, want 0", got)
	}
	if got := replExitCode(lineedit.ErrInterrupt); got != 130 {
		t.Fatalf("replExitCode(ErrInterrupt) = %d, want 130", got)
	}
}

func TestScannerSourceReturnsLinesThenEOF(t *testing.T) {
	src := &scannerSource{scanner: bufio.NewScanner(strings.NewReader("a\nb\n"))}
	var lines []string
	stdout := captureStdout(t, func() {
		for {
			line, err := src.ReadLine(">>> ")
			if err == io.EOF {
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			lines = append(lines, line)
		}
	})
	if strings.Join(lines, "|") != "a|b" {
		t.Fatalf("lines = %q", lines)
	}
	if strings.Count(stdout, ">>> ") != 3 {
		t.Fatalf("prompt must be printed before every read; stdout=%q", stdout)
	}
}
