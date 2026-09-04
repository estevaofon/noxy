package pkgmanager

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	// linha v1 no lugar da arvore → desatualizado
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

func TestSyncInstallsWasmExtensionAndRecordsArtifacts(t *testing.T) {
	manifest := `name = "guest"
abi = 1
wasm = "ext.wasm"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`
	wasm := []byte("wasm bytes")
	w := newLocalRepo(t, map[string]string{"noxy_ext.toml": manifest, "ext.wasm": string(wasm)}, "v0.1.0")
	stubRepos(t, map[string]string{"github.com/t/w": w})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/w v0.1.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	sum := p.sum(t)
	for _, want := range []string{
		"github.com/t/w v0.1.0 sha256:",
		"github.com/t/w v0.1.0 noxy_ext.toml sha256:" + hexSum([]byte(manifest)),
		"github.com/t/w v0.1.0 ext.wasm sha256:" + hexSum(wasm),
	} {
		if !strings.Contains(sum, want) {
			t.Fatalf("noxy.sum missing %q:\n%s", want, sum)
		}
	}
	if data, err := os.ReadFile(filepath.Join(p.libs, "github_com", "t", "w", "ext.wasm")); err != nil || string(data) != string(wasm) {
		t.Fatalf("ext.wasm on disk: %q %v", data, err)
	}

	// Segundo sync: manifesto wasm nao pede binario de plataforma, so o
	// hash de arvore precisa bater — cached.
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.out.String(), "cached") {
		t.Fatalf("second sync must be cached:\n%s", p.out.String())
	}
}

// TestSyncLockedRefusesToWriteRecoveredArtifactLines cobre a recusa de
// escrita do --locked (spec §5.2): um noxy.sum que so tem a linha de arvore
// de um modulo de extensao wasm (linhas de artefato perdidas, ou de um
// noxy.sum escrito a mao) com o pacote AUSENTE do disco. --locked instala o
// pacote normalmente (o hash de arvore bate com o lock, entao lockMatches
// deixa passar), mas o install recupera as linhas de artefato que faltavam
// no lock em memoria — a escrita dessa versao "consertada" tem que ser
// recusada, nao silenciosamente salva.
func TestSyncLockedRefusesToWriteRecoveredArtifactLines(t *testing.T) {
	manifest := `name = "guest"
abi = 1
wasm = "ext.wasm"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`
	wasm := []byte("wasm bytes")
	w := newLocalRepo(t, map[string]string{"noxy_ext.toml": manifest, "ext.wasm": string(wasm)}, "v0.1.0")
	stubRepos(t, map[string]string{"github.com/t/w": w})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/w v0.1.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	full := p.sum(t)
	var treeLine string
	for _, line := range strings.Split(strings.TrimRight(full, "\n"), "\n") {
		if !strings.Contains(line, "noxy_ext.toml") && !strings.Contains(line, "ext.wasm") {
			treeLine = line
		}
	}
	if treeLine == "" {
		t.Fatalf("no tree line found in:\n%s", full)
	}
	onlyTree := treeLine + "\n"
	if err := os.WriteFile(filepath.Join(p.root, "noxy.sum"), []byte(onlyTree), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(p.libs); err != nil {
		t.Fatal(err)
	}
	if err := p.sync(t, true); err == nil || !strings.Contains(err.Error(), "noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked") {
		t.Fatalf("--locked must refuse to write the recovered artifact lines: %v", err)
	}
	if got := p.sum(t); got != onlyTree {
		t.Fatalf("noxy.sum must stay unchanged on disk:\n%s", got)
	}
}

func TestSyncInstallsProcessExtensionAndReinstallsWhenBinaryMissing(t *testing.T) {
	platform := runtime.GOOS + "-" + runtime.GOARCH
	asset := "guest-" + platform
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	manifest := fmt.Sprintf(`name = "guest"
abi = 1
kind = "process"

[binaries]
%s = %q
plan9-mips = "guest-plan9-mips"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`, platform, asset)
	repo := newLocalRepo(t, map[string]string{"noxy_ext.toml": manifest}, "v0.1.0")
	stubRepos(t, map[string]string{"github.com/t/proc": repo})

	mine, other := []byte("my platform bits"), []byte("plan9 bits")
	srv := serveRelease(t, map[string][]byte{
		asset:              mine,
		"guest-plan9-mips": other,
		"checksums.txt":    []byte(hexSum(mine) + "  " + asset + "\n" + hexSum(other) + "  guest-plan9-mips\n"),
	})
	prevRel := releaseBaseURL
	releaseBaseURL = func(string, string) (string, error) { return srv.URL + "/rel/", nil }
	t.Cleanup(func() { releaseBaseURL = prevRel })

	p := newSyncProject(t, "module p\n\nrequire github.com/t/proc v0.1.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	binPath := filepath.Join(p.libs, "github_com", "t", "proc", "bin", asset)
	if data, err := os.ReadFile(binPath); err != nil || string(data) != string(mine) {
		t.Fatalf("asset on disk: %q %v", data, err)
	}
	sum := p.sum(t)
	for _, want := range []string{
		"github.com/t/proc v0.1.0 bin/" + asset + " sha256:" + hexSum(mine),
		"github.com/t/proc v0.1.0 bin/guest-plan9-mips sha256:" + hexSum(other),
	} {
		if !strings.Contains(sum, want) {
			t.Fatalf("noxy.sum missing %q:\n%s", want, sum)
		}
	}
	if !strings.Contains(p.out.String(), "installed (bin/"+asset+")") {
		t.Fatalf("output:\n%s", p.out.String())
	}

	// Apagar o binario desta plataforma: nao pode ficar "cached" para
	// sempre (platformAssetPresent), tem que baixar de novo.
	if err := os.Remove(binPath); err != nil {
		t.Fatal(err)
	}
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p.out.String(), "cached") {
		t.Fatalf("missing platform asset must not read as cached:\n%s", p.out.String())
	}
	if data, err := os.ReadFile(binPath); err != nil || string(data) != string(mine) {
		t.Fatalf("asset must be re-downloaded: %q %v", data, err)
	}
	sum = p.sum(t)
	if !strings.Contains(sum, "github.com/t/proc v0.1.0 bin/guest-plan9-mips sha256:"+hexSum(other)) {
		t.Fatalf("other-platform line must survive:\n%s", sum)
	}

	// Extensao por processo pedida numa pseudo-versao e recusada: assets
	// dependem de uma release, e pseudo-versao nao tem uma.
	scratch := newFetcher(t.TempDir(), io.Discard)
	v, err := scratch.resolve("github.com/t/proc", "master")
	if err != nil {
		v, err = scratch.resolve("github.com/t/proc", "main") // repositorios novos podem chamar o branch de "main"
	}
	scratch.cleanup()
	if err != nil {
		t.Fatal(err)
	}
	q := newSyncProject(t, "module q\n\nrequire github.com/t/proc "+v+"\n")
	err = q.sync(t, false)
	if err == nil || !strings.Contains(err.Error(), "process extensions are installed from a tagged release") {
		t.Fatalf("pseudo-version process extension must be refused: %v", err)
	}
}

// TestReadStampCorruptedLineWarnsAndPrunesNothing cobre a regra do §3.4: uma
// linha do carimbo sem exatamente dois campos derruba o carimbo INTEIRO
// (nao so a linha), com aviso em stampWarn — o proximo sync trata o carimbo
// como vazio (nada e podado, mesmo que o noxy.mod tenha perdido o require).
func TestReadStampCorruptedLineWarnsAndPrunesNothing(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	stampPath := filepath.Join(p.libs, stampFile)
	if err := os.WriteFile(stampPath, []byte("github.com/t/a v1.0.0 extra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Remove o require: sem o carimbo intacto, o pacote nao pode ser podado.
	if err := os.WriteFile(filepath.Join(p.root, "noxy.mod"), []byte("module p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	prev := stampWarn
	stampWarn = &stderr
	t.Cleanup(func() { stampWarn = prev })

	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "warning: noxy_libs/.noxy-sync is corrupted; nothing will be pruned") {
		t.Fatalf("expected corruption warning, got:\n%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(p.libs, "github_com", "t", "a")); err != nil {
		t.Fatalf("package must not be pruned when the stamp is corrupted: %v", err)
	}
}

// No Windows do CI, core.autocrlf=true faz o checkout de noxy.sum com CRLF.
// O parser tolera; a comparacao "lock mudou?" nao podia ser byte a byte, ou
// --locked recusava um lock semanticamente identico (falha vista no CI).
func TestSyncLockedAcceptsCRLFLockAndDoesNotRewriteIt(t *testing.T) {
	a := newLocalRepo(t, map[string]string{"a.nx": "a"}, "v1.0.0")
	stubRepos(t, map[string]string{"github.com/t/a": a})
	p := newSyncProject(t, "module p\n\nrequire github.com/t/a v1.0.0\n")
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(p.root, "noxy.sum")
	lf, _ := os.ReadFile(lockPath)
	crlf := strings.ReplaceAll(string(lf), "\n", "\r\n")
	if err := os.WriteFile(lockPath, []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.sync(t, true); err != nil {
		t.Fatalf("--locked must accept a CRLF lock with identical content: %v", err)
	}
	if err := p.sync(t, false); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(lockPath); string(after) != crlf {
		t.Fatalf("an unchanged lock must not be rewritten, even with CRLF:\n%q", after)
	}
}
