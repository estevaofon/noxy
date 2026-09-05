package pkgmanager

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type syncProject struct {
	root, libs string
	out        *bytes.Buffer
}

func newSyncProject(t *testing.T, mod string) *syncProject {
	t.Helper()
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	if err := os.WriteFile(filepath.Join(root, "noxy.mod"), []byte(mod), 0o644); err != nil {
		t.Fatal(err)
	}
	return &syncProject{root: root, libs: filepath.Join(root, NoxyLibsDir), out: &bytes.Buffer{}}
}

func (p *syncProject) sync(t *testing.T, locked bool) error {
	t.Helper()
	p.out.Reset()
	return Sync(p.root, SyncOptions{Locked: locked, Out: p.out})
}

func (p *syncProject) sum(t *testing.T) string {
	data, _ := os.ReadFile(filepath.Join(p.root, "noxy.sum"))
	return string(data)
}

func TestSyncInstallsClosureAndWritesLockAndStamp(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	b := newLocalRepo(t, map[string]string{"b.nx": "b", "noxy.mod": "module b\n\nrequire github.com/t/a v1.0.0\n"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a, "github.com/t/b": b})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/b v1.0.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"github_com/t/a/a.nx", "github_com/t/b/b.nx"} {
		if _, err := os.Stat(filepath.Join(p.libs, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("%s not installed: %v", rel, err)
		}
	}
	sum := p.sum(t)
	if !strings.Contains(sum, "github.com/t/a v1.0.0 sha256:") || !strings.Contains(sum, "github.com/t/b v1.0.0 sha256:") {
		t.Fatalf("lock must list the whole closure:\n%s", sum)
	}
	stamp := readStamp(p.libs)
	if stamp["github.com/t/a"] != "v1.0.0" || stamp["github.com/t/b"] != "v1.0.0" {
		t.Fatalf("stamp: %v", stamp)
	}
	if !strings.Contains(p.out.String(), "installed") || !strings.Contains(p.out.String(), "Done.") {
		t.Fatalf("output:\n%s", p.out.String())
	}

	// Segundo sync: tudo cached, sem rede, sem reescrever o lock.
	info, _ := os.Stat(filepath.Join(p.root, "noxy.sum"))
	before := info.ModTime()
	time.Sleep(20 * time.Millisecond)
	prevURL := gitURLFor
	gitURLFor = func(string) string { t.Fatal("a synced project must not touch the network"); return "" }
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	gitURLFor = prevURL
	if !strings.Contains(p.out.String(), "cached") {
		t.Fatalf("output:\n%s", p.out.String())
	}
	if info, _ = os.Stat(filepath.Join(p.root, "noxy.sum")); !info.ModTime().Equal(before) {
		t.Fatal("unchanged lock must not be rewritten")
	}
	// --locked num projeto sincronizado passa.
	if err := p.sync(t, true); err != nil {
		t.Fatal(err)
	}
}

func TestSyncReinstallsMissingOrEditedPackage(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(p.libs, "github_com", "t", "a")
	_ = os.WriteFile(filepath.Join(dir, "a.nx"), []byte("edited"), 0o644)
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.nx")); string(data) != "a" {
		t.Fatal("hand-edited package must be reinstalled")
	}
	_ = os.RemoveAll(dir)
	if err := p.sync(t, true); err != nil {
		t.Fatalf("--locked installs what is missing on disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.nx")); err != nil {
		t.Fatal("deleted package must be reinstalled")
	}
}

func TestSyncTreeHashMismatchIsFatal(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\n")
	if err := os.WriteFile(filepath.Join(p.root, "noxy.sum"), []byte("github.com/t/a v1.0.0 sha256:0000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := p.sync(t, false)
	if err == nil || !strings.Contains(err.Error(), "tree hash mismatch") {
		t.Fatalf("moved tag must be fatal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(p.libs, "github_com", "t", "a")); !os.IsNotExist(err) {
		t.Fatal("nothing is installed on mismatch")
	}
}

func TestSyncLockedRefusesOutOfDateLock(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\n")
	err := p.sync(t, true) // lock vazio
	if err == nil || !strings.Contains(err.Error(), "noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked") {
		t.Fatalf("%v", err)
	}
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	// linha v1 no lugar da árvore → desatualizado
	_ = os.WriteFile(filepath.Join(p.root, "noxy.sum"), []byte("github_com/t/a noxy_ext.toml sha256:00\n"), 0o644)
	if err := p.sync(t, true); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("v1 line under --locked: %v", err)
	}
}

func TestSyncPrunesOnlyWhatItInstalled(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	b := newLocalRepo(t, map[string]string{"b.nx": "b"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a, "github.com/t/b": b})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\nrequire github.com/t/b v1.0.0\n")
	writeTree(t, filepath.Join(p.libs, "math_lib"), map[string]string{"math_lib.nx": "hand-placed"})
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(p.root, "noxy.mod"), []byte("module p\n\nrequire github.com/t/a v1.0.0\n"), 0o644)
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.libs, "github_com", "t", "b")); !os.IsNotExist(err) {
		t.Fatal("removed require must be pruned")
	}
	if _, err := os.Stat(filepath.Join(p.libs, "math_lib", "math_lib.nx")); err != nil {
		t.Fatal("hand-placed library must survive")
	}
	if strings.Contains(p.sum(t), "github.com/t/b") {
		t.Fatal("pruned module must leave the lock")
	}
	if !strings.Contains(p.out.String(), "Removed github.com/t/b v1.0.0") {
		t.Fatalf("output:\n%s", p.out.String())
	}
}

func TestSyncMigratesV1LockByReinstalling(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\n")
	_ = os.WriteFile(filepath.Join(p.root, "noxy.sum"), []byte("github_com/t/a noxy_ext.toml sha256:00\n"), 0o644)
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	sum := p.sum(t)
	if strings.Contains(sum, "github_com/") || !strings.Contains(sum, "github.com/t/a v1.0.0 sha256:") {
		t.Fatalf("v1 → v2:\n%s", sum)
	}
}

func TestSyncWithoutNoxyModFails(t *testing.T) {
	err := Sync(t.TempDir(), SyncOptions{Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "no noxy.mod") {
		t.Fatalf("%v", err)
	}
}
