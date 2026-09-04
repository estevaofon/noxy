package pkgmanager

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newLocalRepo cria um repositorio com um commit "v0.1.0" (taggeado se tag)
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
		// repositorios novos podem chamar o branch de "main"
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

// TestFetcherDirCheckoutsPseudoVersionBySha exercita dir num fetcher que NAO
// resolveu a versao (caso do lock: a pseudo-versao vem do noxy.mod, nao do
// cache de resolve), obrigando dir a extrair o sha da propria pseudo-versao
// e clonar/checkout por ele.
func TestFetcherDirCheckoutsPseudoVersionBySha(t *testing.T) {
	repo := newLocalRepo(t, map[string]string{"p.nx": "x"}, "v0.1.0")
	writeTree(t, repo, map[string]string{"p.nx": "y"})
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "second")
	stubGit(t, repo, "abc\trefs/tags/v0.1.0\n")
	libs := filepath.Join(t.TempDir(), "noxy_libs")
	out := &bytes.Buffer{}

	f := newFetcher(libs, out)
	v, err := f.resolve("github.com/acme/p", "master")
	if err != nil {
		// repositorios novos podem chamar o branch de "main"
		v, err = f.resolve("github.com/acme/p", "main")
	}
	if err != nil || !strings.HasPrefix(v, "v0.1.1-0.") {
		t.Fatalf("branch after v0.1.0 → v0.1.1-0.<ts>-<sha>: %q %v", v, err)
	}
	f.cleanup()

	rev, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	wantSHA := strings.TrimSpace(string(rev))[:12]
	if got := pseudoSHA(v); got != wantSHA {
		t.Fatalf("pseudoSHA(%q) = %q, want %q", v, got, wantSHA)
	}

	g := newFetcher(libs, out)
	dir, err := g.dir("github.com/acme/p", v)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "p.nx")); string(data) != "y" {
		t.Fatal("dir must checkout the commit named by the pseudo-version's sha")
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must be gone from the clone")
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
	// Temporario de um sync morto some no inicio do proximo.
	_ = os.MkdirAll(filepath.Join(libs, ".get-zombie-123"), 0o755)
	cleanStaleTemps(libs)
	if _, err := os.Stat(filepath.Join(libs, ".get-zombie-123")); !os.IsNotExist(err) {
		t.Fatal("cleanStaleTemps must remove .get-*")
	}
}

// TestFetcherResolveDoesNotDuplicateCloneForSameVersion cobre spec §4.3:
// resolver o mesmo commit por dois refs diferentes (branch e sha cheio)
// produz a mesma pseudo-versao — o segundo resolve nao pode deixar um
// segundo clone para tras.
func TestFetcherResolveDoesNotDuplicateCloneForSameVersion(t *testing.T) {
	repo := newLocalRepo(t, map[string]string{"p.nx": "x"}, "v0.1.0")
	writeTree(t, repo, map[string]string{"p.nx": "y"})
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "second")
	stubGit(t, repo, "abc\trefs/tags/v0.1.0\n")
	rev, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(rev))

	libs := filepath.Join(t.TempDir(), "noxy_libs")
	f := newFetcher(libs, &bytes.Buffer{})
	defer f.cleanup()
	v1, err := f.resolve("github.com/acme/p", "master")
	if err != nil {
		// repositorios novos podem chamar o branch de "main"
		v1, err = f.resolve("github.com/acme/p", "main")
	}
	if err != nil {
		t.Fatal(err)
	}
	v2, err := f.resolve("github.com/acme/p", sha)
	if err != nil {
		t.Fatal(err)
	}
	if v1 != v2 {
		t.Fatalf("same commit resolved by branch (%q) and sha (%q) must yield the same pseudo-version", v1, v2)
	}
	if len(f.clones) != 1 {
		t.Fatalf("must reuse the single clone for the same module@version, got %d", len(f.clones))
	}
	entries, err := os.ReadDir(libs)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".get-") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("resolving the same version twice must not leave more than one .get-* directory, got %d", count)
	}
}
