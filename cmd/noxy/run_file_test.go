package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadScript e runFile são o caminho de `noxy arquivo.nx` fora do REPL; só o
// núcleo runWithConfig tinha teste. Arquivo inexistente precisa virar
// diagnóstico + exit 1 (antes devolvia 0), e os profiles de CPU/memória
// precisam ser escritos mesmo quando o programa termina em erro — é o caso
// que mais interessa perfilar, e os.Exit não roda defers.

func TestLoadScriptReportsMissingFileOnDiagOut(t *testing.T) {
	diag := withDiagBuffer(t)
	content, ok := loadScript(filepath.Join(t.TempDir(), "nao_existe.nx"))
	if ok || content != "" {
		t.Fatalf("loadScript de arquivo inexistente devolveu ok=%v content=%q", ok, content)
	}
	if !strings.Contains(diag.String(), "Error reading file:") {
		t.Fatalf("diagnóstico=%q, want 'Error reading file:'", diag.String())
	}
}

func TestLoadScriptReadsExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.nx")
	if err := os.WriteFile(path, []byte("print(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content, ok := loadScript(path)
	if !ok || content != "print(1)\n" {
		t.Fatalf("loadScript devolveu ok=%v content=%q", ok, content)
	}
}

func TestRunFileExitCodesFollowTheProgram(t *testing.T) {
	diag := withDiagBuffer(t)
	var okCode, failCode int
	stdout := captureStdout(t, func() {
		okCode = runFile("ok.nx", "print(\"feito\")\n", ".", false, "", "")
		failCode = runFile("fail.nx", "let z: int = 0\nprint(1 / z)\n", ".", false, "", "")
	})
	if okCode != 0 || failCode != 1 {
		t.Fatalf("exit codes: ok=%d fail=%d, want 0 e 1", okCode, failCode)
	}
	if !strings.Contains(stdout, "feito") {
		t.Fatalf("stdout=%q, want saída do programa", stdout)
	}
	if !strings.Contains(diag.String(), "Runtime error:") || !strings.Contains(diag.String(), "division by zero") {
		t.Fatalf("diagnóstico=%q, want runtime error de divisão por zero", diag.String())
	}
}

func TestRunFileWritesCPUAndMemoryProfilesEvenWhenProgramFails(t *testing.T) {
	_ = withDiagBuffer(t)
	dir := t.TempDir()
	cpuPath := filepath.Join(dir, "cpu.prof")
	memPath := filepath.Join(dir, "mem.prof")
	var code int
	_ = captureStdout(t, func() {
		code = runFile("fail.nx", "let z: int = 0\nprint(1 / z)\n", ".", false, cpuPath, memPath)
	})
	if code != 1 {
		t.Fatalf("exit code=%d, want 1", code)
	}
	for _, path := range []string{cpuPath, memPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("profile %s não foi escrito: %v", path, err)
		}
		if info.Size() == 0 {
			t.Fatalf("profile %s ficou vazio", path)
		}
	}
}

func TestRunFileReportsUnwritableProfilePath(t *testing.T) {
	diag := withDiagBuffer(t)
	bad := filepath.Join(t.TempDir(), "pasta_inexistente", "cpu.prof")
	var code int
	_ = captureStdout(t, func() {
		code = runFile("ok.nx", "print(1)\n", ".", false, bad, "")
	})
	if code != 1 {
		t.Fatalf("exit code=%d, want 1 quando o profile não pode ser criado", code)
	}
	if !strings.Contains(diag.String(), "Error creating CPU profile:") {
		t.Fatalf("diagnóstico=%q, want 'Error creating CPU profile:'", diag.String())
	}
}
