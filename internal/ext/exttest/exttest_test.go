package exttest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot tem de ser absoluto e apontar para o go.mod deste modulo mesmo
// com GOFLAGS=-trimpath (configuracao de maquina): com runtime.Caller o
// caminho gravado no binario e relativo ao modulo ("noxy-vm/...") e o chdir
// do go build dos guests falhava.
func TestRepoRootFindsModuleRoot(t *testing.T) {
	root := repoRoot(t)
	if !filepath.IsAbs(root) {
		t.Fatalf("repoRoot must be absolute, got %q", root)
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatalf("go.mod at repoRoot %q: %v", root, err)
	}
	first := strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0])
	if first != "module noxy-vm" {
		t.Fatalf("repoRoot %q is not the noxy-vm module (first line %q)", root, first)
	}
}

func TestBuildGuestProducesWasm(t *testing.T) {
	data := BuildGuest(t, "")
	// Preambulo wasm: \0asm
	if len(data) < 8 || string(data[:4]) != "\x00asm" {
		t.Fatalf("not a wasm binary (%d bytes)", len(data))
	}
	if again := BuildGuest(t, ""); len(again) != len(data) {
		t.Fatal("cache must return the same artifact")
	}
}
