//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPluginPowerShellStopsAfterGoBuildFailure(t *testing.T) {
	shimDirectory := t.TempDir()
	shimPath := filepath.Join(shimDirectory, "go.cmd")
	shim := "@echo off\r\necho injected go build failure 1>&2\r\nexit /b 37\r\n"
	if err := os.WriteFile(shimPath, []byte(shim), 0o600); err != nil {
		t.Fatalf("write go shim: %v", err)
	}

	scriptPath, err := filepath.Abs("build_plugin.ps1")
	if err != nil {
		t.Fatalf("resolve build script: %v", err)
	}
	command := exec.Command(
		"powershell.exe",
		"-NoLogo",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
	)
	command.Env = prependPath(os.Environ(), shimDirectory)

	output, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("build script exited successfully after go build failure:\n%s", output)
	}
	if strings.Contains(string(output), "Created noxy-plugin-terminal.exe") {
		t.Fatalf("build script printed success after go build failure:\n%s", output)
	}
	if !strings.Contains(string(output), "injected go build failure") {
		t.Fatalf("build script did not execute injected go shim:\n%s", output)
	}
}

func prependPath(environment []string, directory string) []string {
	updated := append([]string(nil), environment...)
	for index, entry := range updated {
		key, value, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(key, "PATH") {
			updated[index] = key + "=" + directory + string(os.PathListSeparator) + value
			return updated
		}
	}
	return append(updated, "PATH="+directory)
}
