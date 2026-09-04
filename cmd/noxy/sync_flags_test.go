package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A CLI e o unico lugar que traduz FindRoot(cwd) + Sync em exit code; o
// resto esta testado em pkgmanager. Sobe o binario e roda --sync num
// projeto sem dependencias (nao precisa de rede) e fora de qualquer projeto.
func TestSyncFlagFindsRootAndFailsWithoutNoxyMod(t *testing.T) {
	// Windows so executa caminho absoluto com extensao: sem o ".exe", exec
	// falha com "executable file not found in %PATH%" (falha vista no CI).
	bin := filepath.Join(t.TempDir(), "noxy")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "noxy.mod"), []byte("module p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(project, "sub")
	_ = os.MkdirAll(sub, 0o755)
	cmd := exec.Command(bin, "--sync", "--locked")
	cmd.Dir = sub
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "Done.") {
		t.Fatalf("--sync from a subdirectory: %v\n%s", err, out)
	}
	cmd = exec.Command(bin, "--sync")
	cmd.Dir = t.TempDir()
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "no noxy.mod") {
		t.Fatalf("--sync outside a project: %v\n%s", err, out)
	}
	cmd = exec.Command(bin, "--locked")
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "--locked requires --sync") {
		t.Fatalf("--locked alone: %v\n%s", err, out)
	}
}
