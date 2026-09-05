package pkgmanager

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newLocalRepo cria um repositório com um commit "v0.1.0" (taggeado se tag)
// e devolve o caminho; commits extras via gitIn.
func newLocalRepo(t *testing.T, files map[string]string, tag string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, repo, files)
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "first")
	if tag != "" {
		gitIn(t, repo, "tag", tag)
	}
	return repo
}

func stubGit(t *testing.T, repo string, tags string) {
	t.Helper()
	prevURL, prevTags := gitURLFor, listTags
	gitURLFor = func(string) string { return repo }
	listTags = func(string) (string, error) { return tags, nil }
	t.Cleanup(func() { gitURLFor, listTags = prevURL, prevTags })
}

func TestFetcherResolvesHeadToNewestTag(t *testing.T) {
	repo := newLocalRepo(t, map[string]string{"p.nx": "x"}, "v0.1.0")
	stubGit(t, repo, "abc\trefs/tags/v0.1.0\nabd\trefs/tags/v0.2.0\n")
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), &bytes.Buffer{})
	defer f.cleanup()
	if v, err := f.resolve("github.com/acme/p", "HEAD"); err != nil || v != "v0.2.0" {
		t.Fatalf("HEAD → newest tag: %q %v", v, err)
	}
	if v, err := f.resolve("github.com/acme/p", "0.1.0"); err != nil || v != "v0.1.0" {
		t.Fatalf("tag is normalized: %q %v", v, err)
	}
	if len(f.clones) != 0 {
		t.Fatal("resolving a tag must not clone")
	}
}

func TestFetcherResolvesHeadWithoutTagsToPseudoVersion(t *testing.T) {
	repo := newLocalRepo(t, map[string]string{"p.nx": "x"}, "")
	stubGit(t, repo, "")
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), &bytes.Buffer{})
	defer f.cleanup()
	v, err := f.resolve("github.com/acme/p", "HEAD")
	if err != nil || !IsPseudoVersion(v) || !strings.HasPrefix(v, "v0.0.0-") {
		t.Fatalf("no tag → v0.0.0 pseudo-version: %q %v", v, err)
	}
	dir, err := f.dir("github.com/acme/p", v)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must be gone from the clone")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "p.nx")); string(data) != "x" {
		t.Fatal("clone content")
	}
}

func TestFetcherPseudoVersionUsesBaseTag(t *testing.T) {
	repo := newLocalRepo(t, map[string]string{"p.nx": "x"}, "v0.1.0")
	writeTree(t, repo, map[string]string{"p.nx": "y"})
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "second")
	stubGit(t, repo, "abc\trefs/tags/v0.1.0\n")
	f := newFetcher(filepath.Join(t.TempDir(), "noxy_libs"), &bytes.Buffer{})
	defer f.cleanup()
	v, err := f.resolve("github.com/acme/p", "master")
	if err != nil {
		// repositórios novos podem chamar o branch de "main"
		v, err = f.resolve("github.com/acme/p", "main")
	}
	if err != nil || !strings.HasPrefix(v, "v0.1.1-0.") {
		t.Fatalf("branch after v0.1.0 → v0.1.1-0.<ts>-<sha>: %q %v", v, err)
	}
	dir, err := f.dir("github.com/acme/p", v)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "p.nx")); string(data) != "y" {
		t.Fatal("pseudo-version clone must be at the branch commit")
	}
}

func TestFetcherPromoteAndCleanup(t *testing.T) {
	repo := newLocalRepo(t, map[string]string{"p.nx": "x"}, "v0.1.0")
	stubGit(t, repo, "abc\trefs/tags/v0.1.0\n")
	libs := filepath.Join(t.TempDir(), "noxy_libs")
	f := newFetcher(libs, &bytes.Buffer{})
	if _, err := f.dir("github.com/acme/p", "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.dir("github.com/acme/q", "v0.1.0"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(libs, "github_com", "acme", "p")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(target, "stale.txt"), []byte("old"), 0o644)
	if err := f.promote("github.com/acme/p", "v0.1.0", target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "stale.txt")); !os.IsNotExist(err) {
		t.Fatal("promote replaces the target directory")
	}
	f.cleanup()
	entries, _ := os.ReadDir(libs)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".get-") {
			t.Fatalf("cleanup left %s", e.Name())
		}
	}
	// Temporário de um sync morto some no início do próximo.
	_ = os.MkdirAll(filepath.Join(libs, ".get-zombie-123"), 0o755)
	cleanStaleTemps(libs)
	if _, err := os.Stat(filepath.Join(libs, ".get-zombie-123")); !os.IsNotExist(err) {
		t.Fatal("cleanStaleTemps must remove .get-*")
	}
}
