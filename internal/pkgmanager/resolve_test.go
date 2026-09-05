package pkgmanager

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// repos: modulo → (arquivos, tag). gitURLFor mapeia modulo → repo local.
func stubRepos(t *testing.T, repos map[string]string) {
	t.Helper()
	prevURL, prevTags := gitURLFor, listTags
	gitURLFor = func(m string) string { return repos[m] }
	listTags = func(url string) (string, error) {
		out, err := gitLsRemoteTags(url)
		return out, err
	}
	t.Cleanup(func() { gitURLFor, listTags = prevURL, prevTags })
}

func TestClosurePicksMaxVersionAndWarns(t *testing.T) {
	// raiz → a@v1.0.0, b@v1.0.0 ; b → a@v1.2.0  ⇒ a@v1.2.0
	a := newLocalRepo(t, map[string]string{"a.nx": "a1"}, "v1.0.0")
	writeTree(t, a, map[string]string{"a.nx": "a2"})
	gitIn(t, a, "add", ".")
	gitIn(t, a, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "v1.2.0")
	gitIn(t, a, "tag", "v1.2.0")
	b := newLocalRepo(t, map[string]string{"b.nx": "b", "noxy.mod": "module b\n\nrequire github.com/t/a v1.2.0\n"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a, "github.com/t/b": b})

	root := NewModuleConfig()
	root.Require["github.com/t/a"] = "v1.0.0"
	root.Require["github.com/t/b"] = "v1.0.0"
	out := &bytes.Buffer{}
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), out)
	defer f.cleanup()
	closure, err := computeClosure(closureInput{Root: root, Lock: &SumFile{}, Stamp: map[string]string{}, Libs: f.libs, Fetch: f, Out: out})
	if err != nil {
		t.Fatal(err)
	}
	if closure["github.com/t/a"] != "v1.2.0" || closure["github.com/t/b"] != "v1.0.0" || len(closure) != 2 {
		t.Fatalf("closure: %v", closure)
	}
	if !strings.Contains(out.String(), "github.com/t/a: noxy.mod requires v1.0.0, but github.com/t/b requires v1.2.0; using v1.2.0") {
		t.Fatalf("warning missing:\n%s", out.String())
	}
	if root.Require["github.com/t/a"] != "v1.0.0" {
		t.Fatal("the root noxy.mod line is a floor and must not be rewritten")
	}
}

func TestClosureIgnoresSelfRequireAndKeepsPinnedVersion(t *testing.T) {
	// Regressao do §0: o quicksort publicado requer a si mesmo em HEAD.
	q := newLocalRepo(t, map[string]string{"q.nx": "q", "noxy.mod": "module quicksort\n\nrequire github.com/t/q HEAD\n"}, "v0.1.0")
	stubRepos(t, map[string]string{"github.com/t/q": q})
	prev := listTags
	listTags = func(string) (string, error) { t.Fatal("self-require must not touch the network"); return "", nil }
	t.Cleanup(func() { listTags = prev })
	root := NewModuleConfig()
	root.Require["github.com/t/q"] = "v0.1.0"
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), &bytes.Buffer{})
	defer f.cleanup()
	closure, err := computeClosure(closureInput{Root: root, Lock: &SumFile{}, Stamp: map[string]string{}, Libs: f.libs, Fetch: f, Out: &bytes.Buffer{}})
	if err != nil || closure["github.com/t/q"] != "v0.1.0" || len(closure) != 1 {
		t.Fatalf("%v %v", closure, err)
	}
}

func TestClosureHeadInDependencyUsesLockVersion(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	b := newLocalRepo(t, map[string]string{"b.nx": "b", "noxy.mod": "module b\n\nrequire github.com/t/a HEAD\n"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a, "github.com/t/b": b})
	prev := listTags
	listTags = func(string) (string, error) {
		t.Fatal("HEAD in a dependency must use the lock, not the network")
		return "", nil
	}
	t.Cleanup(func() { listTags = prev })
	lock := &SumFile{}
	lock.SetTree("github.com/t/a", "v1.0.0", "x")
	root := NewModuleConfig()
	root.Require["github.com/t/b"] = "v1.0.0"
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), &bytes.Buffer{})
	defer f.cleanup()
	closure, err := computeClosure(closureInput{Root: root, Lock: lock, Stamp: map[string]string{}, Libs: f.libs, Fetch: f, Out: &bytes.Buffer{}})
	if err != nil || closure["github.com/t/a"] != "v1.0.0" {
		t.Fatalf("%v %v", closure, err)
	}
	// Sob --locked, HEAD em dependencia FORA do lock e erro.
	_, err = computeClosure(closureInput{Root: root, Lock: &SumFile{}, Stamp: map[string]string{}, Libs: f.libs, Fetch: f, Locked: true, Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "HEAD") {
		t.Fatalf("locked + HEAD outside the lock must fail, got %v", err)
	}
}

func TestClosureRootHeadIsPinnedIntoNoxyMod(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v2.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	root := NewModuleConfig()
	root.Require["github.com/t/a"] = "HEAD"
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), &bytes.Buffer{})
	defer f.cleanup()
	if _, err := computeClosure(closureInput{Root: root, Lock: &SumFile{}, Stamp: map[string]string{}, Libs: f.libs, Fetch: f, Locked: true, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "pins github.com/t/a to HEAD") {
		t.Fatalf("locked refuses HEAD in the root: %v", err)
	}
	closure, err := computeClosure(closureInput{Root: root, Lock: &SumFile{}, Stamp: map[string]string{}, Libs: f.libs, Fetch: f, Out: &bytes.Buffer{}})
	if err != nil || closure["github.com/t/a"] != "v2.0.0" || root.Require["github.com/t/a"] != "v2.0.0" {
		t.Fatalf("HEAD → newest tag and written back: %v %v %v", closure, root.Require, err)
	}
}

func TestClosureReadsInstalledDepModWithoutNetwork(t *testing.T) {
	libs := filepath.Join(t.TempDir(), "noxy_libs")
	dep := filepath.Join(libs, "github_com", "t", "b")
	writeTree(t, dep, map[string]string{"b.nx": "b", "noxy.mod": "module b\n\nrequire github.com/t/a v1.0.0\n"})
	digest, _ := TreeHash(dep)
	lock := &SumFile{}
	lock.SetTree("github.com/t/b", "v1.0.0", digest)
	lock.SetTree("github.com/t/a", "v1.0.0", "whatever")
	prevURL := gitURLFor
	gitURLFor = func(string) string { t.Fatal("cached package must be read from disk"); return "" }
	t.Cleanup(func() { gitURLFor = prevURL })
	root := NewModuleConfig()
	root.Require["github.com/t/b"] = "v1.0.0"
	f := newFetcher(libs, &bytes.Buffer{})
	closure, err := computeClosure(closureInput{Root: root, Lock: lock, Stamp: map[string]string{"github.com/t/b": "v1.0.0"}, Libs: libs, Fetch: f, Out: &bytes.Buffer{}})
	if err != nil || closure["github.com/t/a"] != "v1.0.0" {
		t.Fatalf("%v %v", closure, err)
	}
}

func TestCheckNoxyVersion(t *testing.T) {
	cfg := NewModuleConfig()
	cfg.NoxyVersion = "v99.0.0"
	if err := checkNoxyVersion(cfg, "noxy.mod"); err == nil || !strings.Contains(err.Error(), "requires noxy v99.0.0") {
		t.Fatalf("newer requirement must fail: %v", err)
	}
	cfg.NoxyVersion = "v0.1.0"
	if err := checkNoxyVersion(cfg, "noxy.mod"); err != nil {
		t.Fatal(err)
	}
	cfg.NoxyVersion = ""
	if err := checkNoxyVersion(cfg, "noxy.mod"); err != nil {
		t.Fatal(err)
	}
}
