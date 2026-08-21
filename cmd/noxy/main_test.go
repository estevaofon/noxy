package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
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
