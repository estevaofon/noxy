package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/estevaofon/noxy/internal/ext/exttest"
)

// sys_exit chama os.Exit direto: o unico jeito de provar que fecha as
// extensoes antes e rodar o interpretador num subprocesso (spec §4.5).
func TestSysExitClosesProcessExtensions(t *testing.T) {
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	root := t.TempDir()
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("name = \"guest\"\nabi = 1\nkind = \"process\"\n\n[binaries]\n%s = %q\n\n[[export]]\nname = \"guest_pid\"\nparams = []\nreturns = \"int\"\n",
		runtime.GOOS+"-"+runtime.GOARCH, asset)
	bin, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"noxy_ext.toml":             []byte(manifest),
		"guest.nx":                  []byte("func pid() -> int\n    return guest_pid()\nend\n"),
		filepath.Join("bin", asset): bin,
	} {
		if err := os.WriteFile(filepath.Join(pkg, name), data, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(root, "main.nx")
	if err := os.WriteFile(script, []byte("use guest as g\nprint(g.pid())\nsys_exit(0)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./cmd/noxy", script)
	cmd.Dir = repoRootForTest(t)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Falha precoce nao pode deixar o go run (e o guest) vivos.
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	type readResult struct {
		line string
		err  error
	}
	lineCh := make(chan readResult, 1)
	go func() {
		l, e := bufio.NewReader(stdout).ReadString('\n')
		lineCh <- readResult{line: l, err: e}
	}()
	var line string
	select {
	case r := <-lineCh:
		if r.err != nil {
			t.Fatalf("script must print the guest pid: %v", r.err)
		}
		line = r.line
	case <-time.After(60 * time.Second):
		t.Fatal("noxy did not print the guest pid within 60s")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("noxy exited with %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("pid line %q", line)
	}
	deadline := time.Now().Add(3 * time.Second)
	for exttest.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if exttest.ProcessAlive(pid) {
		t.Fatalf("guest %d survived sys_exit", pid)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(wd)) // cmd/noxy → raiz
}
