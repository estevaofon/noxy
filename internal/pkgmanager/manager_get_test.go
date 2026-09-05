package pkgmanager

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestGetProcessExtensionFromTaggedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	work := t.TempDir()
	repo := filepath.Join(work, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	platform := runtime.GOOS + "-" + runtime.GOARCH
	asset := "guest-" + platform
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	manifest := fmt.Sprintf(`name = "guest"
abi = 1
kind = "process"
capabilities = ["net"]

[binaries]
%s = %q
plan9-mips = "guest-plan9-mips"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`, platform, asset)
	for name, data := range map[string]string{"noxy_ext.toml": manifest, "guest.nx": "func noop() -> void\n    guest_noop()\nend\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "v0.1.0")
	gitIn(t, repo, "tag", "v0.1.0")

	mine, other := []byte("my platform bits"), []byte("plan9 bits")
	srv := serveRelease(t, map[string][]byte{
		asset:              mine,
		"guest-plan9-mips": other,
		"checksums.txt":    []byte(hexSum(mine) + "  " + asset + "\n" + hexSum(other) + "  guest-plan9-mips\n"),
	})

	project := filepath.Join(work, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	prevGit, prevRel, prevTag := gitURLFor, releaseBaseURL, listTags
	gitURLFor = func(string) string { return repo }
	releaseBaseURL = func(string, string) (string, error) { return srv.URL + "/rel/", nil }
	listTags = func(string) (string, error) { return "x\trefs/tags/v0.1.0\n", nil }
	t.Cleanup(func() { gitURLFor, releaseBaseURL, listTags = prevGit, prevRel, prevTag })

	if err := Get("github.com/acme/guest"); err != nil {
		t.Fatalf("--get: %v", err)
	}
	pkg := filepath.Join(project, "noxy_libs", "github_com", "acme", "guest")
	if got, err := os.ReadFile(filepath.Join(pkg, "bin", asset)); err != nil || string(got) != "my platform bits" {
		t.Fatalf("asset: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(pkg, ".git")); !os.IsNotExist(err) {
		t.Fatal(".git must be removed")
	}
	sum, _ := os.ReadFile(filepath.Join(project, "noxy.sum"))
	for _, want := range []string{
		"github.com/acme/guest v0.1.0 noxy_ext.toml sha256:" + hexSum([]byte(manifest)),
		"github.com/acme/guest v0.1.0 bin/" + asset + " sha256:" + hexSum(mine),
		"github.com/acme/guest v0.1.0 bin/guest-plan9-mips sha256:" + hexSum(other),
	} {
		if !strings.Contains(string(sum), want) {
			t.Fatalf("noxy.sum missing %q:\n%s", want, sum)
		}
	}
	mod, _ := os.ReadFile(filepath.Join(project, "noxy.mod"))
	if !strings.Contains(string(mod), "require github.com/acme/guest v0.1.0") {
		t.Fatalf("noxy.mod must record the resolved tag:\n%s", mod)
	}

	// --get de novo substitui o diretorio (spec §8.1, passo 1)
	stray := filepath.Join(pkg, "stray.txt")
	if err := os.WriteFile(stray, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Get("github.com/acme/guest@v0.1.0"); err != nil {
		t.Fatalf("second --get: %v", err)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatal("the package directory must be replaced on every --get")
	}
	if _, err := os.Stat(filepath.Join(pkg, "bin", asset)); err != nil {
		t.Fatal("the asset must be downloaded again into the fresh directory")
	}
}

func TestGetFailsWithoutPlatformAsset(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	work := t.TempDir()
	repo := filepath.Join(work, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "name = \"guest\"\nabi = 1\nkind = \"process\"\n\n[binaries]\nplan9-mips = \"guest-plan9-mips\"\n\n[[export]]\nname = \"guest_noop\"\nparams = []\nreturns = \"void\"\n"
	if err := os.WriteFile(filepath.Join(repo, "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "init", "-q")
	gitIn(t, repo, "add", ".")
	gitIn(t, repo, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "v0.1.0")
	gitIn(t, repo, "tag", "v0.1.0")
	other := []byte("plan9 bits")
	srv := serveRelease(t, map[string][]byte{"guest-plan9-mips": other, "checksums.txt": []byte(hexSum(other) + "  guest-plan9-mips\n")})
	project := filepath.Join(work, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	prevGit, prevRel := gitURLFor, releaseBaseURL
	gitURLFor = func(string) string { return repo }
	releaseBaseURL = func(string, string) (string, error) { return srv.URL + "/rel/", nil }
	t.Cleanup(func() { gitURLFor, releaseBaseURL = prevGit, prevRel })

	err := Get("github.com/acme/guest@v0.1.0")
	if err == nil || !strings.Contains(err.Error(), "no binary for "+runtime.GOOS+"/"+runtime.GOARCH) {
		t.Fatalf("--get must fail, not runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, "noxy_libs", "github_com", "acme", "guest")); !os.IsNotExist(err) {
		t.Fatal("a failed --get installs nothing")
	}
}

func TestGetUpgradesPinnedPackageAndKeepsLockHashes(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	writeTree(t, a, map[string]string{"a.nx": "a2"})
	gitIn(t, a, "add", ".")
	gitIn(t, a, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "v1.1.0")
	gitIn(t, a, "tag", "v1.1.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	project := t.TempDir()
	t.Chdir(project)
	if err := Get("github.com/t/a@v1.0.0"); err != nil {
		t.Fatal(err)
	}
	sum1, _ := os.ReadFile(filepath.Join(project, "noxy.sum"))
	// --get de novo na mesma versao: lock inalterado (hash existente vale).
	if err := Get("github.com/t/a@v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if sum2, _ := os.ReadFile(filepath.Join(project, "noxy.sum")); string(sum2) != string(sum1) {
		t.Fatalf("same version must not rewrite the lock:\n%s\n%s", sum1, sum2)
	}
	// --get sem versao sobe para a tag mais nova.
	if err := Get("github.com/t/a"); err != nil {
		t.Fatal(err)
	}
	mod, _ := os.ReadFile(filepath.Join(project, "noxy.mod"))
	if !strings.Contains(string(mod), "require github.com/t/a v1.1.0") {
		t.Fatalf("noxy.mod:\n%s", mod)
	}
	if data, _ := os.ReadFile(filepath.Join(project, "noxy_libs", "github_com", "t", "a", "a.nx")); string(data) != "a2" {
		t.Fatal("upgrade must install the new version")
	}
	if err := Get("https://github.com/t/a"); err == nil || !strings.Contains(err.Error(), "module path must be host/user/repo") {
		t.Fatalf("scheme is rejected: %v", err)
	}
}

func TestGetSameVersionWithMovedTagFails(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	project := t.TempDir()
	t.Chdir(project)
	if err := Get("github.com/t/a@v1.0.0"); err != nil {
		t.Fatal(err)
	}
	// "move" a tag: novo commit, tag reapontada
	writeTree(t, a, map[string]string{"a.nx": "moved"})
	gitIn(t, a, "add", ".")
	gitIn(t, a, "-c", "commit.gpgsign=false", "commit", "-q", "-m", "moved")
	gitIn(t, a, "tag", "-f", "v1.0.0")
	_ = os.RemoveAll(filepath.Join(project, "noxy_libs", "github_com", "t", "a"))
	if err := Get("github.com/t/a@v1.0.0"); err == nil || !strings.Contains(err.Error(), "tree hash mismatch") {
		t.Fatalf("moved tag must be refused by --get too (spec §5.4): %v", err)
	}
}
