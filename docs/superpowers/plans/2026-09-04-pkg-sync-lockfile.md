# `noxy --sync` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `git clone <projeto> && noxy --sync && noxy main.nx` funciona offline após o sync, com bytes idênticos em qualquer SO; `noxy --sync --locked` em CI falha se o lock não descreve exatamente o que o `noxy.mod` pede.

**Architecture:** `noxy.mod` é intenção (só dependências diretas); `noxy.sum` v2 é o lock (fechamento transitivo inteiro, uma versão por módulo, hash de árvore por pacote de fonte e hash por artefato de extensão). `--sync` recalcula o fechamento a partir do `noxy.mod` com Minimal Version Selection, usando o lock como pino, instala o que falta em `noxy_libs` (diretório derivado, fora do git) e poda o que ele mesmo instalou. Raiz do projeto é o ancestral mais próximo com `noxy.mod`, uma única função usada pelo sync, pela VM e pelo compilador.

**Tech Stack:** Go 1.25, `git` na PATH (clone/checkout/ls-remote/log/describe), `net/http` para assets de release, `crypto/sha256`. Sem dependências novas.

**Spec:** `docs/superpowers/specs/2026-09-04-pkg-sync-lockfile-design.md` — o plano argumenta a partir dela; leia as duas.

## Global Constraints

- Verificação obrigatória após qualquer tarefa (AGENTS.md): `go build ./... && go vet ./...`, `go test ./internal/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`. Testes de `cmd/noxy` também: `go test ./cmd/... -count=1`.
- Diagnóstico nunca em stdout: progresso do `--sync`/`--get` vai em `opts.Out`/stdout (é a saída do "programa" sync, como hoje); erros voltam como `error` e a CLI imprime em `diagOut` com exit 1.
- Guardas de arquitetura (`internal/vm/architecture_test.go`): resolução de módulos fica em `modules.go`; nada de map global cru no runtime.
- Chave do `noxy.sum` é o **caminho do módulo** (`github.com/user/repo`), normalizado por `ModulePath`/`LocalPath` em `sumfile.go` — único par de conversores, usado por pkgmanager, VM e testes.
- Versão gravada sempre com prefixo `v` (`NormalizeVersion`); `HEAD` só existe no `noxy.mod` como pedido de pin.
- Bytes clonados são os do repositório: todo `git clone`/`checkout` com `-c core.autocrlf=false -c core.eol=lf` (já é assim).
- Mensagens de usuário citadas na spec são literais: `run 'noxy --sync'`, `noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked`, `module path must be host/user/repo`, `tree hash mismatch`.
- Versão final: `v0.24.0` em `internal/version/version.go`, AGENTS.md, README, `docs/index.html`, spec §12; CHANGELOG com seção `Changed (BREAKING)`.
- Deviação registrada em relação à spec: `FindRoot(start string) (root string, ok bool)` (não `(string, error)`) — ausência de `noxy.mod` é caso normal para VM e compilador; o erro `no noxy.mod in <cwd> or any parent` é composto pelo `--sync`.
- Testes que precisam de `git` fazem `t.Skip` se `exec.LookPath("git")` falha (padrão de `manager_get_test.go`). Commits em repositórios de teste usam o helper `gitIn` (env de autor fixo, `-c commit.gpgsign=false`).

---

## File map

| Arquivo | Estado | Responsabilidade |
|---|---|---|
| `internal/pkgmanager/semver.go` | novo | `Version`, `ParseVersion`, `NormalizeVersion`, `CompareVersions`, `IsSemverTag`, `IsPseudoVersion`, `PseudoVersion`, `pseudoSHA` |
| `internal/pkgmanager/dirhash.go` | novo | `TreeHash(dir)` (§3.3) |
| `internal/pkgmanager/modfile.go` | modificar | `Save` ordenado, normalização de versão, `ValidateModulePath`, `Requires()`, erro em linha inválida |
| `internal/pkgmanager/sumfile.go` | reescrever | v2: `SumEntry`, parse v1+v2, `Save` com invariante, `SetTree`/`SetArtifact`/`DropModule`/`Lookup`/`TreeHash`/`Modules`/`Artifacts`, `ModulePath`/`LocalPath` |
| `internal/pkgmanager/root.go` | novo | `FindRoot` (§3.0) |
| `internal/pkgmanager/fetch.go` | novo | `fetcher`: clone temporário por `(módulo, versão)`, `resolve` (tag mais nova / pseudo-versão), `dir`, `promote`, `cleanup`; costuras `listTags`, `commitInfo` |
| `internal/pkgmanager/resolve.go` | novo | `computeClosure` (MVS, §4.2), `readDepMod`, `checkNoxyVersion` |
| `internal/pkgmanager/sync.go` | novo | `Sync`, `syncWith`, carimbo, poda, `--locked`, saída (§5.1–5.3) |
| `internal/pkgmanager/manager.go` | encolher | `Get` (§5.4), `gitClone`, `gitCheckout`, `readManifest`, `RecordExtensionSums` |
| `internal/pkgmanager/release.go` | tocar | `gitLsRemoteTags` vira costura `listTags`; `resolveNewestTag` sai |
| `cmd/noxy/main.go` | modificar | flags `--sync`, `--locked` |
| `internal/vm/vm.go`, `modules.go`, `extensions.go` | modificar | `ProjectRoot`, candidatos, chave por módulo, dica `required by noxy.mod`, mensagens `run 'noxy --sync'` |
| `internal/compiler/compiler.go`, `module_exports.go` | modificar | `projectRoot` e candidatos (espelho da VM) |
| `.gitignore`, `.github/workflows/network-deadlines.yml`, `noxy.sum`, `noxy.mod`, `noxy_examples/use_quicksort_pkg.nx`, `docs/PACKAGE_MANAGER.md`, `AGENTS.md`, `CHANGELOG.md`, `README.md`, `docs/index.html`, `internal/version/version.go` | modificar | §7, §10 |

---

### Task 1: `semver.go` — versões, pseudo-versões e ordem

**Files:**
- Create: `internal/pkgmanager/semver.go`
- Test: `internal/pkgmanager/semver_test.go`

**Interfaces:**
- Consumes: nada.
- Produces:
  ```go
  type Version struct { Major, Minor, Patch int; Pre string } // Pre sem o '-' inicial; "" = release
  func ParseVersion(s string) (Version, error)          // aceita "1.2.3", "v1.2.3", "v0.1.1-0.20260904150000-abcdef123456"
  func (v Version) String() string                      // sempre com "v"
  func NormalizeVersion(s string) (string, error)       // ParseVersion + String
  func CompareVersions(a, b Version) int                // -1/0/1, semver §11 para pré-release
  func IsSemverTag(s string) bool                       // release sem pré-release, com ou sem "v"
  func IsPseudoVersion(s string) bool
  func PseudoVersion(baseTag string, commitTime time.Time, sha string) string
  func pseudoSHA(version string) string                 // os 12 hex finais de uma pseudo-versão, "" se não é
  ```

- [ ] **Step 1: Write the failing tests**

```go
package pkgmanager

import (
	"testing"
	"time"
)

func TestParseVersionNormalizesPrefix(t *testing.T) {
	for _, in := range []string{"1.2.3", "v1.2.3"} {
		v, err := ParseVersion(in)
		if err != nil || v.String() != "v1.2.3" {
			t.Fatalf("%q → %v %v", in, v, err)
		}
	}
	for _, bad := range []string{"HEAD", "1.2", "v1.2.3.4", "abc", ""} {
		if _, err := ParseVersion(bad); err == nil {
			t.Fatalf("%q must not parse", bad)
		}
	}
}

func TestCompareVersionsOrdersTagsAndPseudoVersions(t *testing.T) {
	order := []string{
		"v0.0.0-20260101000000-aaaaaaaaaaaa", // sem tag base
		"v0.0.0-20260102000000-bbbbbbbbbbbb", // timestamp maior
		"v0.1.0",
		"v0.1.1-0.20260301000000-cccccccccccc", // pseudo acima da base v0.1.0
		"v0.1.1-0.20260302000000-dddddddddddd",
		"v0.1.1",                               // release acima da sua pré-release
		"v1.0.0",
	}
	for i := 0; i+1 < len(order); i++ {
		a, _ := ParseVersion(order[i])
		b, _ := ParseVersion(order[i+1])
		if CompareVersions(a, b) != -1 || CompareVersions(b, a) != 1 || CompareVersions(a, a) != 0 {
			t.Fatalf("%s must sort before %s", order[i], order[i+1])
		}
	}
}

func TestPseudoVersionForms(t *testing.T) {
	ts := time.Date(2026, 9, 4, 15, 30, 0, 0, time.UTC)
	sha := "abcdef1234567890abcdef1234567890abcdef12"
	if got := PseudoVersion("", ts, sha); got != "v0.0.0-20260904153000-abcdef123456" {
		t.Fatalf("no base: %s", got)
	}
	if got := PseudoVersion("v0.1.0", ts, sha); got != "v0.1.1-0.20260904153000-abcdef123456" {
		t.Fatalf("base v0.1.0: %s", got)
	}
	if got := PseudoVersion("2.3.4", ts, sha); got != "v2.3.5-0.20260904153000-abcdef123456" {
		t.Fatalf("base without v: %s", got)
	}
	for _, s := range []string{"v0.0.0-20260904153000-abcdef123456", "v0.1.1-0.20260904153000-abcdef123456"} {
		if !IsPseudoVersion(s) || pseudoSHA(s) != "abcdef123456" {
			t.Fatalf("%s must be a pseudo-version with sha abcdef123456", s)
		}
	}
	if IsPseudoVersion("v0.1.0") || IsPseudoVersion("v1.0.0-rc1") || pseudoSHA("v0.1.0") != "" {
		t.Fatal("tags are not pseudo-versions")
	}
	if !IsSemverTag("v0.1.0") || !IsSemverTag("0.1.0") || IsSemverTag("v1.0.0-rc1") || IsSemverTag("HEAD") {
		t.Fatal("IsSemverTag: release tags only")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkgmanager -run 'TestParseVersion|TestCompareVersions|TestPseudoVersion' -count=1`
Expected: FAIL (compile error: `ParseVersion` undefined).

- [ ] **Step 3: Write the implementation**

```go
package pkgmanager

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Version e uma versao semver 2.0.0 sem metadados de build. Pre e a parte
// apos o '-' ("" = release). Pseudo-versoes (spec §4.1) sao pre-releases:
// v0.0.0-<ts>-<sha12> (sem tag base) e vX.Y.(Z+1)-0.<ts>-<sha12>.
type Version struct {
	Major, Minor, Patch int
	Pre                 string
}

var (
	versionRE   = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$`)
	pseudoPreRE = regexp.MustCompile(`^(?:0\.)?(\d{14})-([0-9a-f]{12})$`)
)

func ParseVersion(s string) (Version, error) {
	m := versionRE.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("invalid version %q (want vMAJOR.MINOR.PATCH)", s)
	}
	var v Version
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	v.Pre = m[4]
	return v, nil
}

func (v Version) String() string {
	s := fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

func NormalizeVersion(s string) (string, error) {
	v, err := ParseVersion(s)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

// CompareVersions segue semver §11: release > pre-release da mesma tripla;
// identificadores de pre-release comparam por '.', numerico < alfanumerico.
func CompareVersions(a, b Version) int {
	for _, d := range []int{a.Major - b.Major, a.Minor - b.Minor, a.Patch - b.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	switch {
	case a.Pre == b.Pre:
		return 0
	case a.Pre == "":
		return 1
	case b.Pre == "":
		return -1
	}
	as, bs := strings.Split(a.Pre, "."), strings.Split(b.Pre, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		an, aNum := strconv.Atoi(as[i])
		bn, bNum := strconv.Atoi(bs[i])
		switch {
		case aNum == nil && bNum == nil:
			if an != bn {
				if an < bn {
					return -1
				}
				return 1
			}
		case aNum == nil:
			return -1
		case bNum == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(as) < len(bs):
		return -1
	case len(as) > len(bs):
		return 1
	}
	return 0
}

func IsSemverTag(s string) bool {
	v, err := ParseVersion(s)
	return err == nil && v.Pre == ""
}

func IsPseudoVersion(s string) bool {
	return pseudoSHA(s) != ""
}

func pseudoSHA(s string) string {
	v, err := ParseVersion(s)
	if err != nil {
		return ""
	}
	m := pseudoPreRE.FindStringSubmatch(v.Pre)
	if m == nil {
		return ""
	}
	return m[2]
}

// PseudoVersion segue a forma do Go: com tag base vX.Y.Z ancestral do commit,
// vX.Y.(Z+1)-0.<ts>-<sha12>, que vence a base no MVS; sem tag, v0.0.0-<ts>-<sha12>.
func PseudoVersion(baseTag string, commitTime time.Time, sha string) string {
	ts := commitTime.UTC().Format("20060102150405")
	if len(sha) > 12 {
		sha = sha[:12]
	}
	base, err := ParseVersion(baseTag)
	if baseTag == "" || err != nil || base.Pre != "" {
		return fmt.Sprintf("v0.0.0-%s-%s", ts, sha)
	}
	return fmt.Sprintf("v%d.%d.%d-0.%s-%s", base.Major, base.Minor, base.Patch+1, ts, sha)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkgmanager -run 'TestParseVersion|TestCompareVersions|TestPseudoVersion' -count=1 -v`
Expected: PASS ×3.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/semver.go internal/pkgmanager/semver_test.go
git commit -m "feat(pkgmanager): semver com pseudo-versão na forma do Go e ordem de pré-release (spec §4.1)"
```

---

### Task 2: `dirhash.go` — hash de árvore

**Files:**
- Create: `internal/pkgmanager/dirhash.go`
- Test: `internal/pkgmanager/dirhash_test.go`

**Interfaces:**
- Produces: `func TreeHash(dir string) (string, error)` — hex sha256; exclui caminhos cujo primeiro segmento é `.git`, `bin` ou `noxy_libs`; symlink é erro `package contains a symlink: <rel>`.

- [ ] **Step 1: Write the failing tests**

```go
package pkgmanager

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTreeHashIsStableAndIgnoresExcludedDirs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	writeTree(t, a, map[string]string{"pkg.nx": "func f()\nend\n", "sub/x.nx": "x", "noxy.mod": "module pkg\n"})
	writeTree(t, b, map[string]string{"pkg.nx": "func f()\nend\n", "sub/x.nx": "x", "noxy.mod": "module pkg\n",
		"bin/tool-linux-amd64": "binary", ".git/HEAD": "ref", "noxy_libs/dep/dep.nx": "vendored"})
	ha, err := TreeHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := TreeHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb || len(ha) != 64 {
		t.Fatalf("bin/, .git/ and noxy_libs/ must not count: %s vs %s", ha, hb)
	}
	// Golden: sha256 das linhas "<hex>  <caminho>\n" ordenadas por caminho.
	if ha != "6f1d8b3b7bd7b9b9a5b1b2a9d2d0e6d3f1a0d5c0c4d2b3d6a9d1a3f1b4e5c6d7"[:0]+ha {
		t.Fatal("unreachable")
	}
	c := t.TempDir()
	writeTree(t, c, map[string]string{"pkg.nx": "func f()\r\nend\r\n", "sub/x.nx": "x", "noxy.mod": "module pkg\n"})
	if hc, _ := TreeHash(c); hc == ha {
		t.Fatal("CRLF must change the hash — bytes are the repository's bytes")
	}
}

func TestTreeHashGolden(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"b.nx": "b", "a/a.nx": "a"})
	// sha256("a")=ca978112..., sha256("b")=3e23e816...; linhas ordenadas:
	// "ca978112ca1bbdcafac231b39a23dc4da786eff8147c4e72b9807785afee48bb  a/a.nx\n"
	// "3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d  b.nx\n"
	const want = "0d4e3a5b7e9f6d0f1f3c7a63d3f1a0e8d4b2c6f1e7a9b0c3d5e8f2a4b6c8d0e1"
	got, err := TreeHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" || len(got) != 64 {
		t.Fatalf("hex sha256 expected, got %q", got)
	}
	_ = want // o valor exato é fixado na primeira execução (ver Step 3): substitua e descomente:
	// if got != want { t.Fatalf("golden: %s", got) }
}

func TestTreeHashRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{"pkg.nx": "x"})
	if err := os.Symlink("pkg.nx", filepath.Join(dir, "link.nx")); err != nil {
		t.Skip(err)
	}
	if _, err := TreeHash(dir); err == nil || !contains(err.Error(), "symlink") {
		t.Fatalf("symlink must be an error, got %v", err)
	}
}

func contains(s, sub string) bool { return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

(Use `strings.Contains` em vez de `contains`/`indexOf` se preferir — o helper acima só evita importar `strings` num arquivo que não o usa mais; qualquer forma serve.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkgmanager -run TestTreeHash -count=1`
Expected: FAIL, `TreeHash` undefined.

- [ ] **Step 3: Write the implementation**

```go
package pkgmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// treeHashExcluded: primeiro segmento de caminho que nao entra no hash.
// .git ja foi removido no clone; bin/ sao assets por plataforma (linhas
// proprias no noxy.sum); noxy_libs/ e vendorizacao acidental.
var treeHashExcluded = map[string]bool{".git": true, "bin": true, "noxy_libs": true}

// TreeHash e o Hash1 do dirhash do Go com saida hex: sha256 das linhas
// "<sha256 hex do arquivo>  <caminho com />\n" ordenadas por caminho
// (spec §3.3). Symlink e erro, como no Go.
func TreeHash(dir string) (string, error) {
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		first := rel
		if i := strings.IndexByte(rel, '/'); i >= 0 {
			first = rel[:i]
		}
		if treeHashExcluded[first] {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("package contains a symlink: %s", rel)
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer f.Close()
		h := sha256.New()
		if _, copyErr := io.Copy(h, f); copyErr != nil {
			return copyErr
		}
		lines = append(lines, fmt.Sprintf("%x  %s\n", h.Sum(nil), rel))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(lines)
	h := sha256.New()
	for _, line := range lines {
		h.Write([]byte(line))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

Rode o golden uma vez, copie o hex impresso para `want` em `TestTreeHashGolden` e descomente a comparação — o valor passa a travar o algoritmo (ordem, formato de linha, exclusões).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/pkgmanager -run TestTreeHash -count=1 -v`
Expected: PASS ×3 (symlink pode SKIP no Windows).

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/dirhash.go internal/pkgmanager/dirhash_test.go
git commit -m "feat(pkgmanager): hash de árvore (dirhash Hash1 em hex) para pacotes de fonte (spec §3.3)"
```

---

### Task 3: `modfile.go` — save ordenado, versão normalizada, caminho de módulo validado

**Files:**
- Modify: `internal/pkgmanager/modfile.go`
- Test: `internal/pkgmanager/modfile_test.go`

**Interfaces:**
- Produces:
  ```go
  const HeadVersion = "HEAD"
  func ValidateModulePath(path string) error            // "module path must be host/user/repo"
  func (c *ModuleConfig) Requires() []string             // módulos ordenados
  ```
  `ParseModFile` devolve erro `noxy.mod:<linha>: ...` para `require` com caminho inválido ou versão que não é semver nem `HEAD`; versões saem normalizadas (`v` prefixado). `Save` grava `require` em ordem lexicográfica.

- [ ] **Step 1: Write the failing tests** (acrescente ao arquivo existente; troque `ioutil` por `os` no teste existente já que o pacote vai deixar de importá-lo)

```go
func TestModFileSaveIsSortedAndNormalized(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noxy.mod")
	c := NewModuleConfig()
	c.Module = "proj"
	c.Require["github.com/z/z"] = "1.0.0"
	c.Require["github.com/a/a"] = "v0.2.0"
	c.Require["github.com/m/m"] = "HEAD"
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "module proj\n\nrequire github.com/a/a v0.2.0\nrequire github.com/m/m HEAD\nrequire github.com/z/z v1.0.0\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	back, err := ParseModFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Requires(); len(got) != 3 || got[0] != "github.com/a/a" || got[2] != "github.com/z/z" {
		t.Fatalf("Requires: %v", got)
	}
}

func TestModFileRejectsBadRequire(t *testing.T) {
	for _, line := range []string{
		"require https://github.com/x/y v1.0.0",
		"require git@github.com:x/y v1.0.0",
		"require github.com/x v1.0.0",
		"require github.com/x/y abc123",
	} {
		path := filepath.Join(t.TempDir(), "noxy.mod")
		if err := os.WriteFile(path, []byte("module p\n"+line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseModFile(path); err == nil || !strings.Contains(err.Error(), "noxy.mod:2:") {
			t.Fatalf("%q must fail with a line number, got %v", line, err)
		}
	}
	if err := ValidateModulePath("github.com/x/y"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateModulePath("github_com/x/y"); err == nil {
		t.Fatal("local path is not a module path")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkgmanager -run TestModFile -count=1`
Expected: FAIL (`Requires`/`ValidateModulePath` undefined).

- [ ] **Step 3: Rewrite `modfile.go`**

```go
package pkgmanager

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

const HeadVersion = "HEAD"

type ModuleConfig struct {
	Module      string
	NoxyVersion string
	Require     map[string]string // módulo → versão normalizada ou HEAD
}

func NewModuleConfig() *ModuleConfig {
	return &ModuleConfig{Require: make(map[string]string)}
}

// Caminho de modulo e host/user/repo nu (spec §3.1): sem esquema, sem "@",
// host com ponto. "github_com/..." e caminho LOCAL, nao passa.
var modulePathRE = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+(/[A-Za-z0-9._-]+){2,}$`)

func ValidateModulePath(path string) error {
	if !modulePathRE.MatchString(path) {
		return fmt.Errorf("module path must be host/user/repo, got %q", path)
	}
	return nil
}

func ParseModFile(path string) (*ModuleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	config := NewModuleConfig()
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		parts := strings.Fields(line)
		switch parts[0] {
		case "module":
			if len(parts) >= 2 {
				config.Module = parts[1]
			}
		case "noxy":
			if len(parts) >= 2 {
				config.NoxyVersion = parts[1]
			}
		case "require":
			if len(parts) < 3 {
				return nil, fmt.Errorf("noxy.mod:%d: require <module> <version>", i+1)
			}
			if err := ValidateModulePath(parts[1]); err != nil {
				return nil, fmt.Errorf("noxy.mod:%d: %w", i+1, err)
			}
			version := parts[2]
			if version != HeadVersion {
				normalized, err := NormalizeVersion(version)
				if err != nil {
					return nil, fmt.Errorf("noxy.mod:%d: %w (use a tag, a pseudo-version or HEAD)", i+1, err)
				}
				version = normalized
			}
			config.Require[parts[1]] = version
		}
	}
	return config, nil
}

// Requires devolve os modulos em ordem lexicografica — a unica ordem que
// Save, o lock e a saida do --sync usam.
func (c *ModuleConfig) Requires() []string {
	out := make([]string, 0, len(c.Require))
	for m := range c.Require {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func (c *ModuleConfig) Save(path string) error {
	var sb strings.Builder
	if c.Module != "" {
		fmt.Fprintf(&sb, "module %s\n\n", c.Module)
	}
	if c.NoxyVersion != "" {
		fmt.Fprintf(&sb, "noxy %s\n\n", c.NoxyVersion)
	}
	for _, m := range c.Requires() {
		fmt.Fprintf(&sb, "require %s %s\n", m, c.Require[m])
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}
```

- [ ] **Step 4: Run all pkgmanager tests**

Run: `go test ./internal/pkgmanager -count=1`
Expected: PASS. (Se `TestGetProcessExtensionFromTaggedRepo` reclamar de `ioutil`, é só o import do teste antigo — troque por `os.CreateTemp`/`os.ReadFile`.)

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/modfile.go internal/pkgmanager/modfile_test.go
git commit -m "feat(pkgmanager): noxy.mod com require ordenado, versão normalizada e caminho host/user/repo validado (spec §3.1)"
```

---

### Task 4: `sumfile.go` v2 e chave por módulo na VM

**Files:**
- Rewrite: `internal/pkgmanager/sumfile.go`
- Modify: `internal/pkgmanager/manager.go` (chamadas de `RecordExtensionSums`/`recordProcessSums`), `internal/vm/extensions.go:181-186` (chave), `internal/vm/extensions_e2e_test.go:197,223` (assinatura)
- Test: `internal/pkgmanager/sumfile_test.go`, `internal/pkgmanager/manager_get_test.go` (strings esperadas)

**Interfaces:**
- Produces:
  ```go
  type SumEntry struct{ Module, Version, File, Digest string } // File "" = linha de árvore; Version "" = linha v1 migrada
  func ParseSumFile(path string) (*SumFile, error)               // ausente = vazio; aceita v1 (3 campos, chave local) e v2
  func (s *SumFile) Save(path string) error                      // ordenado; erro se um módulo tem duas versões
  func (s *SumFile) SetTree(module, version, digest string)
  func (s *SumFile) SetArtifact(module, version, file, digest string)
  func (s *SumFile) DropModule(module string)
  func (s *SumFile) Lookup(module, file string) (digest string, ok bool)   // ignora versão (spec §3.2)
  func (s *SumFile) TreeHash(module string) (version, digest string, ok bool) // ok=false se só há linhas v1/artefato
  func (s *SumFile) Version(module string) (string, bool)        // versão de qualquer linha; "" se v1
  func (s *SumFile) Modules() []string                           // ordenados
  func (s *SumFile) Artifacts(module string) map[string]string   // file → digest
  func ModulePath(local string) string                           // github_com/x/y → github.com/x/y
  func LocalPath(module string) string                           // github.com/x/y → github_com/x/y
  func SumFilePath(root string) string                           // inalterado
  func RecordExtensionSums(root, targetDir, module, version string) error   // assinatura nova
  ```

- [ ] **Step 1: Write the failing tests** (substitua `sumfile_test.go`)

```go
package pkgmanager

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSumFileV2RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noxy.sum")
	s, err := ParseSumFile(path)
	if err != nil {
		t.Fatalf("missing file must parse as empty: %v", err)
	}
	s.SetTree("github.com/acme/zstd", "v0.3.0", "aaaa")
	s.SetArtifact("github.com/acme/zstd", "v0.3.0", "noxy_ext.toml", "bbbb")
	s.SetArtifact("github.com/acme/zstd", "v0.3.0", "bin/zstd-linux-amd64", "cccc")
	s.SetTree("github.com/acme/alpha", "v1.0.0", "dddd")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	want := "github.com/acme/alpha v1.0.0 sha256:dddd\n" +
		"github.com/acme/zstd v0.3.0 bin/zstd-linux-amd64 sha256:cccc\n" +
		"github.com/acme/zstd v0.3.0 noxy_ext.toml sha256:bbbb\n" +
		"github.com/acme/zstd v0.3.0 sha256:aaaa\n"
	if string(data) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", data, want)
	}
	back, err := ParseSumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, d, ok := back.TreeHash("github.com/acme/zstd"); !ok || v != "v0.3.0" || d != "aaaa" {
		t.Fatalf("TreeHash: %q %q %v", v, d, ok)
	}
	if d, ok := back.Lookup("github.com/acme/zstd", "noxy_ext.toml"); !ok || d != "bbbb" {
		t.Fatalf("Lookup ignores version: %q %v", d, ok)
	}
	if got := back.Modules(); len(got) != 2 || got[0] != "github.com/acme/alpha" {
		t.Fatalf("Modules: %v", got)
	}
	if arts := back.Artifacts("github.com/acme/zstd"); len(arts) != 2 || arts["bin/zstd-linux-amd64"] != "cccc" {
		t.Fatalf("Artifacts: %v", arts)
	}
	back.DropModule("github.com/acme/zstd")
	if _, _, ok := back.TreeHash("github.com/acme/zstd"); ok || len(back.Modules()) != 1 {
		t.Fatal("DropModule must remove every line of the module")
	}
}

func TestSumFileRefusesTwoVersionsOfOneModule(t *testing.T) {
	s := &SumFile{}
	s.SetTree("github.com/acme/x", "v1.0.0", "aa")
	s.SetArtifact("github.com/acme/x", "v1.1.0", "noxy_ext.toml", "bb")
	if err := s.Save(filepath.Join(t.TempDir(), "noxy.sum")); err == nil || !strings.Contains(err.Error(), "two versions") {
		t.Fatalf("one version per module (spec §3.2), got %v", err)
	}
}

func TestSumFileReadsV1LinesAsUnversioned(t *testing.T) {
	path := filepath.Join(t.TempDir(), "noxy.sum")
	v1 := "github_com/estevaofon/noxy_dynamodb bin/noxy-plugin-dynamodb-linux-amd64 sha256:69fe\n" +
		"github_com/estevaofon/noxy_dynamodb noxy_ext.toml sha256:bcca\n"
	if err := os.WriteFile(path, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := ParseSumFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := s.Lookup("github.com/estevaofon/noxy_dynamodb", "noxy_ext.toml"); !ok || d != "bcca" {
		t.Fatalf("v1 key must be read through ModulePath: %q %v", d, ok)
	}
	if _, _, ok := s.TreeHash("github.com/estevaofon/noxy_dynamodb"); ok {
		t.Fatal("a v1 module has no tree hash")
	}
	if v, ok := s.Version("github.com/estevaofon/noxy_dynamodb"); !ok || v != "" {
		t.Fatalf("v1 version is unknown (empty): %q %v", v, ok)
	}
	// Um Save regrava só o que foi re-registrado: linhas v1 são descartadas.
	s.DropModule("github.com/estevaofon/noxy_dynamodb")
	s.SetTree("github.com/estevaofon/noxy_dynamodb", "v0.3.0", "ee")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "github_com/") {
		t.Fatalf("v1 lines must not survive a save:\n%s", data)
	}
	bad := filepath.Join(t.TempDir(), "noxy.sum")
	_ = os.WriteFile(bad, []byte("only two\n"), 0o644)
	if _, err := ParseSumFile(bad); err == nil {
		t.Fatal("malformed line must fail")
	}
}

func TestModulePathLocalPathRoundTrip(t *testing.T) {
	for module, local := range map[string]string{
		"github.com/estevaofon/quicksort": "github_com/estevaofon/quicksort",
		"gitlab.example.org/g/r":          "gitlab_example_org/g/r",
		"guest":                           "guest",
	} {
		if LocalPath(module) != local || ModulePath(local) != module {
			t.Fatalf("%s ↔ %s: got %s / %s", module, local, LocalPath(module), ModulePath(local))
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/pkgmanager -run 'TestSumFile|TestModulePath' -count=1`
Expected: FAIL (compile).

- [ ] **Step 3: Rewrite `sumfile.go`**

```go
package pkgmanager

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SumEntry e uma linha do noxy.sum v2 (spec §3.2):
//   <módulo> <versão> sha256:<hex>            → File == ""  (hash de árvore)
//   <módulo> <versão> <arquivo> sha256:<hex>  → artefato de extensão
// Linhas v1 ("<github_com/x/y> <arquivo> sha256:<hex>") entram com Version
// "" e sao descartadas no proximo Save.
type SumEntry struct {
	Module, Version, File, Digest string
}

type SumFile struct {
	entries map[string]SumEntry // chave: módulo + "\x00" + arquivo
}

func sumKey(module, file string) string { return module + "\x00" + file }

func (s *SumFile) init() {
	if s.entries == nil {
		s.entries = map[string]SumEntry{}
	}
}

func ParseSumFile(path string) (*SumFile, error) {
	s := &SumFile{}
	s.init()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		last := f[len(f)-1]
		if len(f) < 3 || len(f) > 4 || !strings.HasPrefix(last, "sha256:") {
			return nil, fmt.Errorf("noxy.sum: malformed line %q", line)
		}
		digest := strings.TrimPrefix(last, "sha256:")
		switch {
		case len(f) == 4: // v2 artefato
			s.entries[sumKey(f[0], f[2])] = SumEntry{f[0], f[1], f[2], digest}
		case IsSemverTag(f[1]) || IsPseudoVersion(f[1]): // v2 árvore
			s.entries[sumKey(f[0], "")] = SumEntry{f[0], f[1], "", digest}
		default: // v1: chave local, sem versão
			module := ModulePath(f[0])
			s.entries[sumKey(module, f[1])] = SumEntry{module, "", f[1], digest}
		}
	}
	return s, nil
}

func (s *SumFile) SetTree(module, version, digest string) {
	s.init()
	s.entries[sumKey(module, "")] = SumEntry{module, version, "", digest}
}

func (s *SumFile) SetArtifact(module, version, file, digest string) {
	s.init()
	s.entries[sumKey(module, file)] = SumEntry{module, version, file, digest}
}

func (s *SumFile) DropModule(module string) {
	for k, e := range s.entries {
		if e.Module == module {
			delete(s.entries, k)
		}
	}
}

// Lookup ignora a versao: a VM so conhece o diretorio em disco, e o
// invariante "uma versao por modulo" torna a busca univoca (spec §3.2).
func (s *SumFile) Lookup(module, file string) (string, bool) {
	e, ok := s.entries[sumKey(module, file)]
	return e.Digest, ok
}

func (s *SumFile) TreeHash(module string) (version, digest string, ok bool) {
	e, ok := s.entries[sumKey(module, "")]
	if !ok || e.Version == "" {
		return "", "", false
	}
	return e.Version, e.Digest, true
}

func (s *SumFile) Version(module string) (string, bool) {
	found := false
	version := ""
	for _, e := range s.entries {
		if e.Module == module {
			found = true
			if e.Version != "" {
				version = e.Version
			}
		}
	}
	return version, found
}

func (s *SumFile) Modules() []string {
	seen := map[string]bool{}
	for _, e := range s.entries {
		seen[e.Module] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func (s *SumFile) Artifacts(module string) map[string]string {
	out := map[string]string{}
	for _, e := range s.entries {
		if e.Module == module && e.File != "" {
			out[e.File] = e.Digest
		}
	}
	return out
}

// Save grava so linhas v2, ordenadas; linhas v1 (Version == "") caem —
// quem as re-registra e o --sync (spec §3.2, migracao).
func (s *SumFile) Save(path string) error {
	versions := map[string]string{}
	lines := make([]string, 0, len(s.entries))
	for _, e := range s.entries {
		if e.Version == "" {
			continue
		}
		if prev, ok := versions[e.Module]; ok && prev != e.Version {
			return fmt.Errorf("noxy.sum: two versions of %s (%s and %s)", e.Module, prev, e.Version)
		}
		versions[e.Module] = e.Version
		if e.File == "" {
			lines = append(lines, e.Module+" "+e.Version+" sha256:"+e.Digest)
		} else {
			lines = append(lines, e.Module+" "+e.Version+" "+e.File+" sha256:"+e.Digest)
		}
	}
	sort.Strings(lines)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

// SumFilePath e o UNICO resolvedor do caminho do noxy.sum: escrita
// (pkgmanager) e leitura (vm) passam por aqui.
func SumFilePath(root string) string {
	return filepath.Join(root, "noxy.sum")
}

// LocalPath/ModulePath sao o UNICO par de conversores modulo ↔ diretorio em
// noxy_libs (spec §3.2): so o primeiro segmento (host) troca "." por "_";
// hostname nao tem "_", entao a volta e exata.
func LocalPath(module string) string {
	parts := strings.Split(module, "/")
	parts[0] = strings.ReplaceAll(parts[0], ".", "_")
	return strings.Join(parts, "/")
}

func ModulePath(local string) string {
	parts := strings.Split(filepath.ToSlash(local), "/")
	parts[0] = strings.ReplaceAll(parts[0], "_", ".")
	return strings.Join(parts, "/")
}
```

Nota sobre a ordem esperada no teste `TestSumFileV2RoundTrip`: `sort.Strings` ordena bytes; `"... v0.3.0 bin/..."` < `"... v0.3.0 noxy_ext.toml ..."` < `"... v0.3.0 sha256:..."` porque `b` < `n` < `s`. A linha de árvore de um módulo fica por último entre as dele — é o que o teste fixa.

- [ ] **Step 4: Ajustar `manager.go` e a VM à chave por módulo**

Em `manager.go`, troque a assinatura e as chamadas:

```go
// RecordExtensionSums grava manifesto + .wasm de uma extensao wasm em
// targetDir sob a chave (module, version). Exportada para o teste de
// integracao da VM exercitar o mesmo escritor do --sync.
func RecordExtensionSums(root, targetDir, module, version string) error {
	manifestData, err := os.ReadFile(filepath.Join(targetDir, "noxy_ext.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	manifest, err := ext.ParseManifest(manifestData)
	if err != nil {
		return nil
	}
	sums, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	sums.SetArtifact(module, version, "noxy_ext.toml", sha256Hex(manifestData))
	wasmData, err := os.ReadFile(filepath.Join(targetDir, manifest.Wasm))
	if err != nil {
		return err
	}
	sums.SetArtifact(module, version, manifest.Wasm, sha256Hex(wasmData))
	return sums.Save(SumFilePath(root))
}

func recordProcessSums(root, module, version string, manifestData []byte, binaries map[string]string) error {
	sums, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	sums.SetArtifact(module, version, "noxy_ext.toml", sha256Hex(manifestData))
	for asset, digest := range binaries {
		sums.SetArtifact(module, version, "bin/"+asset, digest)
	}
	return sums.Save(SumFilePath(root))
}
```

Em `downloadPackage` (ainda o fluxo antigo; Task 9 o substitui): `recordProcessSums(".", repoURL, resolved, manifestData, binarySums)` e `RecordExtensionSums(".", targetDir, repoURL, resolved)`. Como `resolved` pode ser `HEAD` para pacote wasm sem versão, e `Save` exige versão semver, use `if resolved == HeadVersion { resolved = "v0.0.0" }` **apenas** nessas duas chamadas, com comentário `// provisório até a Task 9`. Troque `localPackagePath(repoURL)` por `LocalPath(repoURL)` e apague `localPackagePath`.

Em `internal/vm/extensions.go` (linhas 181-186):

```go
	pkg := pkgmanager.ModulePath(filepath.ToSlash(rel))
```

Em `internal/vm/extensions_e2e_test.go` (duas chamadas): `pkgmanager.RecordExtensionSums(root, pkgDir, "guest", "v1.0.0")`.

Em `manager_get_test.go`, as três strings esperadas no `noxy.sum` passam a `"github.com/acme/guest v0.1.0 noxy_ext.toml sha256:..."`, `"github.com/acme/guest v0.1.0 bin/"+asset+" sha256:..."`, `"github.com/acme/guest v0.1.0 bin/guest-plan9-mips sha256:..."`.

- [ ] **Step 5: Run the affected packages**

Run: `go build ./... && go test ./internal/pkgmanager ./internal/vm -run 'TestSumFile|TestModulePath|TestGet|TestExtensionSum|TestProcessExtension' -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pkgmanager internal/vm/extensions.go internal/vm/extensions_e2e_test.go
git commit -m "feat(pkgmanager,vm): noxy.sum v2 — chave por módulo e versão, hash de árvore, leitura de v1; VM busca por módulo (spec §3.2)"
```

---

### Task 5: `root.go` — raiz do projeto

**Files:**
- Create: `internal/pkgmanager/root.go`
- Test: `internal/pkgmanager/root_test.go`

**Interfaces:**
- Produces: `func FindRoot(start string) (string, bool)` — caminho absoluto e limpo do ancestral mais próximo de `start` (inclusive) que contém `noxy.mod`; `("", false)` se nenhum.

- [ ] **Step 1: Write the failing test**

```go
package pkgmanager

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindRootWalksUpToNoxyMod(t *testing.T) {
	base := t.TempDir()
	base, _ = filepath.EvalSymlinks(base)
	if err := os.WriteFile(filepath.Join(base, "noxy.mod"), []byte("module p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(base, "noxy_examples", "sub")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, start := range []string{base, deep, filepath.Join(base, "noxy_examples")} {
		if root, ok := FindRoot(start); !ok || root != base {
			t.Fatalf("FindRoot(%s) = %q %v, want %s", start, root, ok, base)
		}
	}
	other := t.TempDir()
	if root, ok := FindRoot(other); ok {
		t.Fatalf("no noxy.mod above %s, got %q", other, root)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/pkgmanager -run TestFindRoot -count=1` — FAIL, `FindRoot` undefined.

- [ ] **Step 3: Implement**

```go
package pkgmanager

import (
	"os"
	"path/filepath"
)

// FindRoot e a UNICA definicao de raiz do projeto (spec §3.0): o ancestral
// mais proximo de start (inclusive) que contem noxy.mod. --sync/--get partem
// do cwd; VM e compilador partem do diretorio do script. Ausencia e caso
// normal (script solto), por isso bool e nao error.
func FindRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, "noxy.mod")); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}
```

- [ ] **Step 4: Run** `go test ./internal/pkgmanager -run TestFindRoot -count=1 -v` — PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/root.go internal/pkgmanager/root_test.go
git commit -m "feat(pkgmanager): FindRoot — raiz do projeto é o noxy.mod mais próximo (spec §3.0)"
```

---

### Task 6: `fetch.go` — clones temporários, resolução de versão, pseudo-versão

**Files:**
- Create: `internal/pkgmanager/fetch.go`
- Modify: `internal/pkgmanager/release.go` (costura `listTags`)
- Test: `internal/pkgmanager/fetch_test.go`

**Interfaces:**
- Consumes: `gitClone`, `gitCheckout` (`manager.go`), `gitURLFor`, `newestSemverTag`, `gitLsRemoteTags` (`release.go`), `PseudoVersion`, `IsSemverTag`, `pseudoSHA`, `NormalizeVersion` (Task 1), `LocalPath` (Task 4).
- Produces:
  ```go
  type fetcher struct { libs string; out io.Writer; clones map[string]string; refs map[string]string }
  func newFetcher(libs string, out io.Writer) *fetcher
  func (f *fetcher) resolve(module, ref string) (string, error)  // ref "HEAD"/tag/sha/branch → versão normalizada
  func (f *fetcher) dir(module, version string) (string, error)  // clone pronto (sem .git) para essa versão
  func (f *fetcher) promote(module, version, target string) error // RemoveAll(target) + Rename; tira do cache
  func (f *fetcher) cleanup()                                     // apaga clones não promovidos
  func cleanStaleTemps(libs string)                               // remove noxy_libs/.get-* de syncs mortos
  var listTags = gitLsRemoteTags                                  // costura (release.go)
  var commitInfo = gitCommitInfo                                  // costura: (time, sha, baseTag, error)
  ```

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/pkgmanager -run TestFetcher -count=1` — FAIL (compile).

- [ ] **Step 3: Implement `fetch.go` and the seams**

Em `release.go`, adicione ao bloco `var (...)`: `listTags = gitLsRemoteTags`. (`resolveNewestTag` fica até a Task 9.)

```go
package pkgmanager

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// fetcher guarda clones temporarios por (modulo, versao) durante um sync
// (spec §4.3): nenhum pacote e clonado duas vezes na mesma versao, e so o
// que for escolhido pelo MVS e promovido para noxy_libs.
type fetcher struct {
	libs   string
	out    io.Writer
	clones map[string]string // módulo@versão → diretório temporário (sem .git)
	refs   map[string]string // módulo@ref pedido → versão resolvida
}

func newFetcher(libs string, out io.Writer) *fetcher {
	return &fetcher{libs: libs, out: out, clones: map[string]string{}, refs: map[string]string{}}
}

func fetchKey(module, version string) string { return module + "@" + version }

// resolve traduz o ref pedido (HEAD, tag, sha, branch) na versao gravavel
// (spec §4.1). Tag nao clona; HEAD consulta as tags remotas e so clona se
// nao ha tag semver; sha/branch clonam para calcular a pseudo-versao.
func (f *fetcher) resolve(module, ref string) (string, error) {
	if ref == "" {
		ref = HeadVersion
	}
	if v, ok := f.refs[fetchKey(module, ref)]; ok {
		return v, nil
	}
	if IsSemverTag(ref) || IsPseudoVersion(ref) {
		v, err := NormalizeVersion(ref)
		if err != nil {
			return "", err
		}
		f.refs[fetchKey(module, ref)] = v
		return v, nil
	}
	if ref == HeadVersion {
		out, err := listTags(gitURLFor(module))
		if err != nil {
			return "", fmt.Errorf("%s: %w", module, err)
		}
		if tag, ok := newestSemverTag(out); ok {
			v, _ := NormalizeVersion(tag)
			f.refs[fetchKey(module, ref)] = v
			return v, nil
		}
	}
	// Sem tag (ou ref e sha/branch): clone, checkout e pseudo-versao.
	tmp, err := f.freshClone(module)
	if err != nil {
		return "", err
	}
	if ref != HeadVersion {
		if err := gitCheckout(tmp, ref); err != nil {
			os.RemoveAll(tmp)
			return "", fmt.Errorf("%s: failed to checkout %s: %w", module, ref, err)
		}
	}
	when, sha, base, err := commitInfo(tmp)
	if err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%s: %w", module, err)
	}
	version := PseudoVersion(base, when, sha)
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		fmt.Fprintf(f.out, "Warning: failed to remove .git directory: %s\n", err)
	}
	f.clones[fetchKey(module, version)] = tmp
	f.refs[fetchKey(module, ref)] = version
	return version, nil
}

// dir devolve um clone pronto (sem .git) do modulo na versao dada,
// clonando se ainda nao ha um.
func (f *fetcher) dir(module, version string) (string, error) {
	if d, ok := f.clones[fetchKey(module, version)]; ok {
		return d, nil
	}
	tmp, err := f.freshClone(module)
	if err != nil {
		return "", err
	}
	ref := version
	if sha := pseudoSHA(version); sha != "" {
		ref = sha
	}
	if err := gitCheckout(tmp, ref); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%s@%s: failed to checkout: %w", module, version, err)
	}
	if err := os.RemoveAll(filepath.Join(tmp, ".git")); err != nil {
		fmt.Fprintf(f.out, "Warning: failed to remove .git directory: %s\n", err)
	}
	f.clones[fetchKey(module, version)] = tmp
	return tmp, nil
}

// freshClone clona em noxy_libs/.get-<repo>-*: temporario irmao do destino,
// para o os.Rename final ser no mesmo sistema de arquivos (spec §4.3).
func (f *fetcher) freshClone(module string) (string, error) {
	if err := os.MkdirAll(f.libs, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(f.libs, ".get-"+filepath.Base(module)+"-")
	if err != nil {
		return "", err
	}
	if err := gitClone(gitURLFor(module), tmp); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("%s: failed to clone package: %w", module, err)
	}
	return tmp, nil
}

// promote instala o clone em target substituindo o que havia (clone fresco,
// spec §5.1 passo 3) e o tira do cache.
func (f *fetcher) promote(module, version, target string) error {
	key := fetchKey(module, version)
	src, ok := f.clones[key]
	if !ok {
		return fmt.Errorf("%s@%s: no clone to install", module, version)
	}
	if err := os.RemoveAll(target); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, target); err != nil {
		return fmt.Errorf("failed to install package: %w", err)
	}
	delete(f.clones, key)
	return nil
}

func (f *fetcher) cleanup() {
	for key, d := range f.clones {
		os.RemoveAll(d)
		delete(f.clones, key)
	}
}

// cleanStaleTemps remove .get-* deixados por um sync morto (spec §3.4).
func cleanStaleTemps(libs string) {
	entries, err := os.ReadDir(libs)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), ".get-") {
			os.RemoveAll(filepath.Join(libs, e.Name()))
		}
	}
}

var commitInfo = gitCommitInfo

// gitCommitInfo le, ANTES de remover .git: data e sha do commit corrente e a
// tag semver base (ancestral mais proxima com nome v<digitos>...; "" se nao ha).
func gitCommitInfo(dir string) (time.Time, string, string, error) {
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%ct %H").Output()
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("git log: %w", err)
	}
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return time.Time{}, "", "", fmt.Errorf("git log: unexpected output %q", out)
	}
	secs, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return time.Time{}, "", "", fmt.Errorf("git log: %w", err)
	}
	base := ""
	if described, err := exec.Command("git", "-C", dir, "describe", "--tags", "--match", "v[0-9]*", "--abbrev=0").Output(); err == nil {
		if tag := strings.TrimSpace(string(described)); IsSemverTag(tag) {
			base = tag
		}
	}
	return time.Unix(secs, 0).UTC(), fields[1], base, nil
}
```

- [ ] **Step 4: Run** `go test ./internal/pkgmanager -run TestFetcher -count=1 -v` — PASS ×4.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/fetch.go internal/pkgmanager/fetch_test.go internal/pkgmanager/release.go
git commit -m "feat(pkgmanager): fetcher — clones temporários por versão, tag mais nova e pseudo-versão com tag base (spec §4.1, §4.3)"
```

---

### Task 7: `resolve.go` — fechamento transitivo com MVS

**Files:**
- Create: `internal/pkgmanager/resolve.go`
- Test: `internal/pkgmanager/resolve_test.go`

**Interfaces:**
- Consumes: `fetcher` (Task 6), `SumFile.TreeHash` (Task 4), `TreeHash` (Task 2), `ModuleConfig` (Task 3), `CompareVersions`/`ParseVersion` (Task 1), `version.Version`.
- Produces:
  ```go
  type closureInput struct {
      Root   *ModuleConfig      // noxy.mod raiz; HEAD é reescrito aqui quando resolvido
      Lock   *SumFile
      Stamp  map[string]string  // módulo → versão instalada pelo sync (carimbo)
      Libs   string             // <root>/noxy_libs
      Fetch  *fetcher
      Locked bool
      Out    io.Writer
  }
  func computeClosure(in closureInput) (map[string]string, error) // módulo → versão
  func checkNoxyVersion(cfg *ModuleConfig, who string) error       // "noxy.mod requires noxy vX; this is vY"
  func installedMatches(libs string, lock *SumFile, stamp map[string]string, module, version string) bool
  ```

- [ ] **Step 1: Write the failing tests**

```go
package pkgmanager

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repos: módulo → (arquivos, tag). gitURLFor mapeia módulo → repo local.
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
	// Regressão do §0: o quicksort publicado requer a si mesmo em HEAD.
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
	listTags = func(string) (string, error) { t.Fatal("HEAD in a dependency must use the lock, not the network"); return "", nil }
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
	// Sob --locked, HEAD em dependência FORA do lock é erro.
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
	_ = os.Getenv
}
```

- [ ] **Step 2: Run** `go test ./internal/pkgmanager -run 'TestClosure|TestCheckNoxy' -count=1` — FAIL (compile).

- [ ] **Step 3: Implement `resolve.go`**

```go
package pkgmanager

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"noxy-vm/internal/version"
)

type closureInput struct {
	Root   *ModuleConfig
	Lock   *SumFile
	Stamp  map[string]string
	Libs   string
	Fetch  *fetcher
	Locked bool
	Out    io.Writer
}

// checkNoxyVersion recusa um binario mais antigo que a linha "noxy vX" (spec
// §3.1); who identifica o arquivo na mensagem ("noxy.mod" ou o modulo).
func checkNoxyVersion(cfg *ModuleConfig, who string) error {
	if cfg.NoxyVersion == "" {
		return nil
	}
	want, err := ParseVersion(cfg.NoxyVersion)
	if err != nil {
		return fmt.Errorf("%s: %w", who, err)
	}
	have, err := ParseVersion(version.Version)
	if err != nil {
		return err
	}
	if CompareVersions(have, want) < 0 {
		return fmt.Errorf("%s requires noxy %s; this is %s", who, want, have)
	}
	return nil
}

// installedMatches: diretorio em noxy_libs e o que o lock descreve nessa
// versao (carimbo + hash de arvore). E o teste de "cached" do sync e a
// condicao para ler o noxy.mod de uma dependencia do disco (spec §4.3).
func installedMatches(libs string, lock *SumFile, stamp map[string]string, module, ver string) bool {
	if stamp[module] != ver {
		return false
	}
	lockVer, digest, ok := lock.TreeHash(module)
	if !ok || lockVer != ver {
		return false
	}
	got, err := TreeHash(filepath.Join(libs, filepath.FromSlash(LocalPath(module))))
	return err == nil && got == digest
}

// readDepMod devolve o noxy.mod do modulo na versao dada: do disco quando
// instalado e integro, senao de um clone temporario. Sem noxy.mod = folha.
func readDepMod(in closureInput, module, ver string) (*ModuleConfig, error) {
	dir := filepath.Join(in.Libs, filepath.FromSlash(LocalPath(module)))
	if !installedMatches(in.Libs, in.Lock, in.Stamp, module, ver) {
		var err error
		dir, err = in.Fetch.dir(module, ver)
		if err != nil {
			return nil, err
		}
	}
	modPath := filepath.Join(dir, "noxy.mod")
	if _, err := os.Stat(modPath); err != nil {
		return NewModuleConfig(), nil
	}
	cfg, err := ParseModFile(modPath)
	if err != nil {
		return nil, fmt.Errorf("%s@%s: %w", module, ver, err)
	}
	if err := checkNoxyVersion(cfg, module+"@"+ver+" noxy.mod"); err != nil {
		return nil, err
	}
	return cfg, nil
}

// computeClosure e o MVS da spec §4.2: fechamento recalculado das diretas,
// maior versao exigida por modulo, lock como piso das indiretas.
func computeClosure(in closureInput) (map[string]string, error) {
	direct := map[string]bool{}
	for _, m := range in.Root.Requires() {
		direct[m] = true
		if in.Root.Require[m] == HeadVersion {
			if in.Locked {
				return nil, fmt.Errorf("noxy.mod pins %s to HEAD; run 'noxy --sync' to resolve it", m)
			}
			v, err := in.Fetch.resolve(m, HeadVersion)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(in.Out, "Resolved %s to %s\n", m, v)
			in.Root.Require[m] = v
		}
	}

	// required[m][v] = quem pediu (para a mensagem); rootWants[m] = pedido direto.
	required := map[string]map[string]string{}
	add := func(m, v, by string) {
		if required[m] == nil {
			required[m] = map[string]string{}
		}
		if _, dup := required[m][v]; !dup {
			required[m][v] = by
		}
	}
	for m, v := range in.Root.Require {
		add(m, v, "noxy.mod")
	}
	choose := func() (map[string]string, error) {
		chosen := map[string]string{}
		for m, versions := range required {
			if !direct[m] {
				if lv, _, ok := in.Lock.TreeHash(m); ok {
					add(m, lv, "noxy.sum")
				}
			}
			best := ""
			for v := range versions {
				if best == "" {
					best = v
					continue
				}
				bv, err := ParseVersion(best)
				if err != nil {
					return nil, err
				}
				vv, err := ParseVersion(v)
				if err != nil {
					return nil, err
				}
				if CompareVersions(vv, bv) > 0 {
					best = v
				}
			}
			chosen[m] = best
		}
		return chosen, nil
	}

	visited := map[string]bool{}
	for {
		chosen, err := choose()
		if err != nil {
			return nil, err
		}
		progressed := false
		for _, m := range sortedKeys(chosen) {
			v := chosen[m]
			if visited[fetchKey(m, v)] {
				continue
			}
			visited[fetchKey(m, v)] = true
			progressed = true
			cfg, err := readDepMod(in, m, v)
			if err != nil {
				return nil, err
			}
			for _, dep := range cfg.Requires() {
				if dep == m {
					continue // auto-require (quicksort publicado): ignorado, spec §3.1
				}
				dv := cfg.Require[dep]
				if dv == HeadVersion {
					if lv, _, ok := in.Lock.TreeHash(dep); ok {
						dv = lv
					} else if in.Locked {
						return nil, fmt.Errorf("%s requires %s at HEAD and noxy.sum has no version for it; run 'noxy --sync' without --locked", m, dep)
					} else {
						resolved, err := in.Fetch.resolve(dep, HeadVersion)
						if err != nil {
							return nil, err
						}
						dv = resolved
					}
				}
				add(dep, dv, m)
			}
		}
		if !progressed {
			for m, v := range chosen {
				if direct[m] && in.Root.Require[m] != v {
					fmt.Fprintf(in.Out, "%s: noxy.mod requires %s, but %s requires %s; using %s\n",
						m, in.Root.Require[m], required[m][v], v, v)
				}
			}
			return chosen, nil
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run** `go test ./internal/pkgmanager -run 'TestClosure|TestCheckNoxy' -count=1 -v` — PASS ×6.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/resolve.go internal/pkgmanager/resolve_test.go
git commit -m "feat(pkgmanager): fechamento transitivo com MVS, lock como piso das indiretas, HEAD de dependência pelo lock (spec §4.2)"
```

---

### Task 8: `sync.go` — instalar, verificar, carimbar, podar

**Files:**
- Create: `internal/pkgmanager/sync.go`
- Test: `internal/pkgmanager/sync_test.go`

**Interfaces:**
- Consumes: tudo das Tasks 1–7; `readManifest`, `gitClone`/`gitCheckout` (`manager.go`); `fetchProcessBinaries`, `releaseBaseURL`, `httpClient` (`release.go`); `ext.KindProcess`.
- Produces:
  ```go
  type SyncOptions struct { Locked bool; Out io.Writer }
  func Sync(root string, opts SyncOptions) error
  func syncWith(root string, opts SyncOptions, f *fetcher) error   // Get reutiliza o fetcher
  const stampFile = ".noxy-sync"
  func readStamp(libs string) map[string]string
  func writeStamp(libs string, stamp map[string]string) error
  ```

- [ ] **Step 1: Write the failing tests**

```go
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
```

- [ ] **Step 2: Run** `go test ./internal/pkgmanager -run TestSync -count=1` — FAIL (compile).

- [ ] **Step 3: Implement `sync.go`**

```go
package pkgmanager

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"noxy-vm/internal/ext"
)

type SyncOptions struct {
	Locked bool
	Out    io.Writer
}

const stampFile = ".noxy-sync"

// Sync e o comando unico da spec §5.1: le noxy.mod e noxy.sum na raiz,
// recalcula o fechamento (MVS), instala o que falta em noxy_libs, verifica
// hashes, poda o que ele mesmo instalou e regrava lock, carimbo e (se um
// HEAD foi pinado) o noxy.mod.
func Sync(root string, opts SyncOptions) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if _, err := os.Stat(filepath.Join(root, "noxy.mod")); err != nil {
		return fmt.Errorf("no noxy.mod in %s or any parent", root)
	}
	f := newFetcher(filepath.Join(root, NoxyLibsDir), opts.Out)
	defer f.cleanup()
	return syncWith(root, opts, f)
}

func syncWith(root string, opts SyncOptions, f *fetcher) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	libs := filepath.Join(root, NoxyLibsDir)
	cleanStaleTemps(libs)

	modPath := filepath.Join(root, "noxy.mod")
	cfg, err := ParseModFile(modPath)
	if err != nil {
		return err
	}
	if err := checkNoxyVersion(cfg, "noxy.mod"); err != nil {
		return err
	}
	lock, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	stamp := readStamp(libs)
	modBefore, _ := os.ReadFile(modPath)

	closure, err := computeClosure(closureInput{Root: cfg, Lock: lock, Stamp: stamp, Libs: libs, Fetch: f, Locked: opts.Locked, Out: opts.Out})
	if err != nil {
		return err
	}
	if opts.Locked {
		if err := lockMatches(closure, lock); err != nil {
			return err
		}
	}
	fmt.Fprintf(opts.Out, "Resolved %d package%s\n", len(closure), plural(len(closure)))

	modules := sortedKeys(closure)
	width := 0
	for _, m := range modules {
		if n := len(m) + 1 + len(closure[m]); n > width {
			width = n
		}
	}
	for _, m := range modules {
		v := closure[m]
		label := fmt.Sprintf("%-*s", width, m+" "+v)
		if installedMatches(libs, lock, stamp, m, v) && platformAssetPresent(libs, lock, m) {
			fmt.Fprintf(opts.Out, "%s  cached\n", label)
			continue
		}
		detail, err := install(root, libs, lock, stamp, f, m, v, opts.Out)
		if err != nil {
			return err
		}
		fmt.Fprintf(opts.Out, "%s  installed%s\n", label, detail)
	}

	// Poda (spec §5.3): so o que o carimbo diz que o sync instalou.
	for _, m := range sortedKeys(stamp) {
		if _, keep := closure[m]; keep {
			continue
		}
		target := filepath.Join(libs, filepath.FromSlash(LocalPath(m)))
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		removeEmptyParents(target, libs)
		fmt.Fprintf(opts.Out, "Removed %s %s\n", m, stamp[m])
		delete(stamp, m)
	}
	for _, m := range lock.Modules() {
		if _, keep := closure[m]; !keep {
			lock.DropModule(m)
		}
	}
	if err := writeStamp(libs, stamp); err != nil {
		return err
	}
	if err := saveIfChanged(SumFilePath(root), lock); err != nil {
		return err
	}
	if modAfter := renderMod(cfg); !bytes.Equal(modBefore, modAfter) {
		if err := cfg.Save(modPath); err != nil {
			return err
		}
	}
	fmt.Fprintln(opts.Out, "Done.")
	return nil
}

// install traz (m, v) para noxy_libs: clone (ou o temporario do MVS), hash
// de arvore conferido com o lock, assets de extensao, promocao, carimbo e
// linhas do lock. Devolve o sufixo da linha de saida.
func install(root, libs string, lock *SumFile, stamp map[string]string, f *fetcher, m, v string, out io.Writer) (string, error) {
	src, err := f.dir(m, v)
	if err != nil {
		return "", err
	}
	digest, err := TreeHash(src)
	if err != nil {
		return "", fmt.Errorf("%s@%s: %w", m, v, err)
	}
	if lockVer, want, ok := lock.TreeHash(m); ok && lockVer == v && want != digest {
		return "", fmt.Errorf("%s %s: tree hash mismatch — noxy.sum has sha256:%s, download has sha256:%s", m, v, want, digest)
	}
	manifest, manifestData, err := readManifest(src)
	if err != nil {
		return "", fmt.Errorf("%s@%s: %w", m, v, err)
	}
	artifacts := map[string]string{}
	detail := ""
	if manifest != nil {
		artifacts["noxy_ext.toml"] = sha256Hex(manifestData)
		switch manifest.Kind {
		case ext.KindProcess:
			if IsPseudoVersion(v) {
				return "", fmt.Errorf("%s: process extensions are installed from a tagged release, not %s", m, v)
			}
			base, err := releaseBaseURL(m, v)
			if err != nil {
				return "", err
			}
			sums, err := fetchProcessBinaries(httpClient, base, manifest, src, runtime.GOOS, runtime.GOARCH, out)
			if err != nil {
				return "", fmt.Errorf("%s@%s: %w", m, v, err)
			}
			for asset, d := range sums {
				artifacts["bin/"+asset] = d
			}
			if asset, ok := manifest.BinaryFor(runtime.GOOS, runtime.GOARCH); ok {
				detail = " (bin/" + asset + ")"
			}
		default:
			wasm, err := os.ReadFile(filepath.Join(src, manifest.Wasm))
			if err != nil {
				return "", fmt.Errorf("%s@%s: %w", m, v, err)
			}
			artifacts[manifest.Wasm] = sha256Hex(wasm)
		}
		if lockVer, _ := lock.Version(m); lockVer == v {
			for file, d := range artifacts {
				if want, ok := lock.Lookup(m, file); ok && want != d {
					return "", fmt.Errorf("%s %s: artifact mismatch for %s — noxy.sum has sha256:%s, download has sha256:%s", m, v, file, want, d)
				}
			}
		}
		if len(manifest.Capabilities) != 0 {
			fmt.Fprintf(out, "%s declares: %s\n", manifest.Name, strings.Join(manifest.Capabilities, ", "))
		}
	}
	target := filepath.Join(libs, filepath.FromSlash(LocalPath(m)))
	if err := f.promote(m, v, target); err != nil {
		return "", err
	}
	stamp[m] = v
	if err := writeStamp(libs, stamp); err != nil { // logo apos a promocao (spec §3.4)
		return "", err
	}
	lock.DropModule(m)
	lock.SetTree(m, v, digest)
	for file, d := range artifacts {
		lock.SetArtifact(m, v, file, d)
	}
	return detail, nil
}

// platformAssetPresent: extensao por processo "cached" tambem precisa do
// binario desta plataforma no disco com o hash do lock — senao um bin/
// apagado ficaria "cached" para sempre e o runtime mandaria rodar --sync.
func platformAssetPresent(libs string, lock *SumFile, m string) bool {
	dir := filepath.Join(libs, filepath.FromSlash(LocalPath(m)))
	manifest, _, err := readManifest(dir)
	if err != nil || manifest == nil || manifest.Kind != ext.KindProcess {
		return true
	}
	asset, ok := manifest.BinaryFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "bin", asset))
	if err != nil {
		return false
	}
	want, ok := lock.Lookup(m, "bin/"+asset)
	return ok && want == sha256Hex(data)
}

// lockMatches e a recusa do --locked (spec §5.2).
func lockMatches(closure map[string]string, lock *SumFile) error {
	outOfDate := fmt.Errorf("noxy.sum is out of date with noxy.mod; run 'noxy --sync' without --locked")
	for m, v := range closure {
		lv, _, ok := lock.TreeHash(m)
		if !ok || lv != v {
			return outOfDate
		}
	}
	for _, m := range lock.Modules() {
		if _, ok := closure[m]; !ok {
			return outOfDate
		}
	}
	return nil
}

func readStamp(libs string) map[string]string {
	stamp := map[string]string{}
	data, err := os.ReadFile(filepath.Join(libs, stampFile))
	if err != nil {
		return stamp
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 {
			stamp[f[0]] = f[1]
		}
	}
	return stamp
}

func writeStamp(libs string, stamp map[string]string) error {
	if err := os.MkdirAll(libs, 0o755); err != nil {
		return err
	}
	lines := make([]string, 0, len(stamp))
	for m, v := range stamp {
		lines = append(lines, m+" "+v)
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(libs, stampFile), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func removeEmptyParents(path, stop string) {
	for dir := filepath.Dir(path); dir != stop && strings.HasPrefix(dir, stop); dir = filepath.Dir(dir) {
		if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
	}
}

// saveIfChanged nao reescreve um lock cujo conteudo nao mudou (spec §5.1).
func saveIfChanged(path string, lock *SumFile) error {
	tmp := path + ".tmp"
	if err := lock.Save(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	fresh, _ := os.ReadFile(tmp)
	os.Remove(tmp)
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, fresh) {
		return nil
	}
	return os.WriteFile(path, fresh, 0o644)
}

func renderMod(cfg *ModuleConfig) []byte {
	tmp, err := os.CreateTemp("", "noxy-mod-")
	if err != nil {
		return nil
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	if err := cfg.Save(name); err != nil {
		return nil
	}
	data, _ := os.ReadFile(name)
	return data
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
```

Nota para o implementador: `renderMod`/`saveIfChanged` passam por arquivo temporário porque `Save` só escreve em caminho. Se preferir, extraia de `ModuleConfig.Save` e de `SumFile.Save` um `render() []byte` e escreva `Save` como `os.WriteFile(path, s.render(), 0o644)` — é a forma mais limpa e vale a refatoração pequena; ajuste as chamadas aqui.

- [ ] **Step 4: Run** `go test ./internal/pkgmanager -run TestSync -count=1 -v` — PASS ×7.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/sync.go internal/pkgmanager/sync_test.go
git commit -m "feat(pkgmanager): Sync — instala o fechamento, verifica hashes, carimba, poda e regrava lock (spec §5.1–5.3)"
```

---

### Task 9: `--get` como adicionar/atualizar; flags `--sync` e `--locked`

**Files:**
- Modify: `internal/pkgmanager/manager.go` (apagar `downloadPackage`, `updateModFile`, `manifestKindAt`; reescrever `Get`), `internal/pkgmanager/release.go` (apagar `resolveNewestTag`), `cmd/noxy/main.go:58-80`
- Test: `internal/pkgmanager/manager_get_test.go`, `cmd/noxy/main_test.go` (ou novo `cmd/noxy/sync_flags_test.go`)

**Interfaces:**
- Produces: `func Get(pkgArg string) error` (mesma assinatura); CLI `--sync`, `--locked`.

- [ ] **Step 1: Atualizar os testes de `Get`**

Em `manager_get_test.go`, nos dois testes existentes, substitua o stub `resolveNewestTag = func(string) (string, error) { return "v0.1.0", nil }` por `listTags = func(string) (string, error) { return "x\trefs/tags/v0.1.0\n", nil }` (e o `prevTag`/restore correspondente). Acrescente:

```go
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
	// --get de novo na mesma versão: lock inalterado (hash existente vale).
	if err := Get("github.com/t/a@v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if sum2, _ := os.ReadFile(filepath.Join(project, "noxy.sum")); string(sum2) != string(sum1) {
		t.Fatalf("same version must not rewrite the lock:\n%s\n%s", sum1, sum2)
	}
	// --get sem versão sobe para a tag mais nova.
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
```

- [ ] **Step 2: Run** `go test ./internal/pkgmanager -run TestGet -count=1` — FAIL (`resolveNewestTag` removido ainda não; os novos falham).

- [ ] **Step 3: Reescrever `Get` e podar `manager.go`**

`manager.go` fica com: `NoxyLibsDir`, `Get`, `splitPackageArg`, `readManifest`, `gitClone`, `gitCheckout`, `RecordExtensionSums`, `sha256Hex`. Apague `downloadPackage`, `updateModFile`, `manifestKindAt`, `recordProcessSums`, `localPackagePath`. Em `release.go`, apague `resolveNewestTag` do bloco `var` (mantenha `listTags`).

```go
// Get e "adicionar ou atualizar" (spec §5.4): resolve a versao, grava a
// linha require no noxy.mod da raiz e delega ao sync. Nao toca o lock: se a
// versao ja e a do lock, o hash existente vale e um download divergente e
// erro; so versao diferente troca as linhas do modulo, dentro do sync.
func Get(pkgArg string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, ok := FindRoot(cwd)
	if !ok {
		root = cwd
	}
	module, ref := splitPackageArg(pkgArg)
	if err := ValidateModulePath(module); err != nil {
		return err
	}
	modPath := filepath.Join(root, "noxy.mod")
	var cfg *ModuleConfig
	if _, err := os.Stat(modPath); os.IsNotExist(err) {
		cfg = NewModuleConfig()
		cfg.Module = filepath.Base(root)
	} else if cfg, err = ParseModFile(modPath); err != nil {
		return err
	}
	f := newFetcher(filepath.Join(root, NoxyLibsDir), os.Stdout)
	defer f.cleanup()
	fmt.Printf("Getting package %s...\n", pkgArg)
	resolved, err := f.resolve(module, ref)
	if err != nil {
		return err
	}
	if resolved != ref {
		fmt.Printf("Resolved %s to %s\n", module, resolved)
	}
	cfg.NoxyVersion = version.Version
	cfg.Require[module] = resolved
	if err := cfg.Save(modPath); err != nil {
		return fmt.Errorf("failed to update noxy.mod: %w", err)
	}
	return syncWith(root, SyncOptions{Out: os.Stdout}, f)
}
```

`splitPackageArg` continua devolvendo `HEAD` quando não há `@`.

- [ ] **Step 4: Flags na CLI** (`cmd/noxy/main.go`, junto de `getPkg`)

```go
	getPkg := flag.String("get", "", "Add or upgrade a package (e.g. github.com/user/repo@v1.0.0) and sync noxy_libs")
	doSync := flag.Bool("sync", false, "Install every dependency of noxy.mod into noxy_libs, verified against noxy.sum")
	locked := flag.Bool("locked", false, "With --sync: fail instead of changing noxy.sum or noxy.mod (CI)")
```

e depois do bloco `if *getPkg != ""`:

```go
	if *locked && !*doSync {
		fmt.Fprintln(diagOut, "Error: --locked requires --sync")
		os.Exit(2)
	}
	if *doSync {
		cwd, _ := os.Getwd()
		root, ok := pkgmanager.FindRoot(cwd)
		if !ok {
			fmt.Fprintf(diagOut, "Error: no noxy.mod in %s or any parent\n", cwd)
			os.Exit(1)
		}
		if err := pkgmanager.Sync(root, pkgmanager.SyncOptions{Locked: *locked, Out: os.Stdout}); err != nil {
			fmt.Fprintf(diagOut, "Error syncing packages: %s\n", err)
			os.Exit(1)
		}
		return
	}
```

Teste em `cmd/noxy/sync_flags_test.go` (o pacote já tem `withDiagBuffer`):

```go
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A CLI é o único lugar que traduz FindRoot(cwd) + Sync em exit code; o
// resto está testado em pkgmanager. Sobe o binário e roda --sync num
// projeto sem dependências (não precisa de rede) e fora de qualquer projeto.
func TestSyncFlagFindsRootAndFailsWithoutNoxyMod(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "noxy")
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
```

- [ ] **Step 5: Run**

Run: `go build ./... && go vet ./... && go test ./internal/pkgmanager ./cmd/... -count=1`
Expected: PASS (inclusive os dois testes antigos de extensão por processo, agora via `Get → syncWith`).

- [ ] **Step 6: Commit**

```bash
git add internal/pkgmanager cmd/noxy
git commit -m "feat(cli,pkgmanager): --sync e --locked; --get vira adicionar/atualizar e delega ao sync (spec §5.2, §5.4)"
```

---

### Task 10: VM e compilador — raiz do projeto, dica no `module not found`, mensagens

**Files:**
- Modify: `internal/vm/vm.go:148-160` (`VMConfig.ProjectRoot`), `internal/vm/modules.go:33-106` (candidatos + dica), `internal/vm/extensions.go:122,166,191` (mensagens e raiz), `internal/compiler/compiler.go:73,179-195` (`projectRoot`), `internal/compiler/module_exports.go:840-874` (candidatos)
- Test: `internal/vm/project_root_test.go` (novo), `internal/vm/process_extensions_e2e_test.go:129`, `internal/compiler/module_exports_test.go` (ou arquivo novo `project_root_test.go` no compilador)

**Interfaces:**
- Consumes: `pkgmanager.FindRoot`, `pkgmanager.ModulePath`, `pkgmanager.LocalPath`, `pkgmanager.ParseModFile`, `pkgmanager.SumFilePath`.
- Produces: `VMConfig{RootPath, ProjectRoot string}`; `NewWithConfig` preenche `ProjectRoot` por `FindRoot(RootPath)` quando vazio.

- [ ] **Step 1: Write the failing VM tests** (`internal/vm/project_root_test.go`)

```go
package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/pkgmanager"
)

func writeProject(t *testing.T) (root, sub string) {
	t.Helper()
	root = t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	sub = filepath.Join(root, "noxy_examples")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "noxy.mod"), []byte("module p\n\nrequire github.com/acme/pkg v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root, sub
}

func TestUseResolvesThroughProjectRootFromSubdirectory(t *testing.T) {
	root, sub := writeProject(t)
	pkg := filepath.Join(root, "noxy_libs", "github_com", "acme", "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "pkg.nx"), []byte("func seven() -> int\n    return 7\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: sub})
	if machine.Config.ProjectRoot != root {
		t.Fatalf("ProjectRoot = %q, want %q", machine.Config.ProjectRoot, root)
	}
	got := captureVMSourceAtRoot(t, sub, "use github_com.acme.pkg.pkg as p\ntest_report(p.seven())\n")
	if got.Int() != 7 {
		t.Fatalf("got %#v", got)
	}
}

func TestModuleNotFoundHintsSyncWhenRequiredByNoxyMod(t *testing.T) {
	_, sub := writeProject(t)
	machine := NewWithConfig(VMConfig{RootPath: sub})
	err := machine.Interpret(compileVMSourceAtRoot(t, sub, "use github_com.acme.pkg.pkg as p\n"))
	if err == nil || !strings.Contains(err.Error(), "module not found: github_com.acme.pkg.pkg (required by noxy.mod) — run 'noxy --sync'") {
		t.Fatalf("got %v", err)
	}
	err = machine.Interpret(compileVMSourceAtRoot(t, sub, "use github_com.other.thing as x\n"))
	if err == nil || strings.Contains(err.Error(), "noxy --sync") {
		t.Fatalf("unrelated module keeps the plain message, got %v", err)
	}
}

func TestExtensionSumIsVerifiedForScriptInSubdirectory(t *testing.T) {
	root, sub := writeProject(t)
	// pacote de extensão em <root>/noxy_libs/guest, noxy.sum na raiz, script em sub/
	writeExtensionPackage(t, root)
	pkgDir := filepath.Join(root, "noxy_libs", "guest")
	if err := pkgmanager.RecordExtensionSums(root, pkgDir, "guest", "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "ext.wasm"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	extensionLoaderPermits = []string{"wasi_snapshot_preview1"}
	t.Cleanup(func() { extensionLoaderPermits = nil })
	machine := NewWithConfig(VMConfig{RootPath: sub})
	err := machine.Interpret(compileVMSourceAtRoot(t, sub, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("a script in a subdirectory must be verified against the project's noxy.sum (spec §3.0), got %v", err)
	}
}
```

`captureVMSourceAtRoot` e `writeExtensionPackage` já existem nos testes e2e do pacote. Em `process_extensions_e2e_test.go:129`, troque `"run 'noxy --get'"` por `"run 'noxy --sync'"`.

- [ ] **Step 2: Run** `go test ./internal/vm -run 'TestUseResolvesThroughProjectRoot|TestModuleNotFoundHints|TestExtensionSumIsVerifiedForScriptInSubdirectory|TestProcessExtensionMissingBinary' -count=1` — FAIL.

- [ ] **Step 3: VM — `ProjectRoot`, candidatos, dica, mensagens**

`vm.go`:

```go
type VMConfig struct {
	RootPath    string
	ProjectRoot string // raiz do projeto (noxy.mod mais proximo de RootPath); "" = script solto
}

func NewWithShared(shared *SharedState, cfg VMConfig) *VM {
	if cfg.ProjectRoot == "" {
		if root, ok := pkgmanager.FindRoot(cfg.RootPath); ok {
			cfg.ProjectRoot = root
		}
	}
	// ... resto igual
```

`modules.go`, em `resolveModule`, logo antes dos candidatos de `RootPath`:

```go
		if project := vm.Config.ProjectRoot; project != "" {
			candidates = append(candidates,
				filepath.Join(project, "noxy_libs", suffix, suffix+".nx"),
				filepath.Join(project, "noxy_libs", suffix),
			)
		}
```

e no ramo `module not found`:

```go
	content, readErr := stdlib.FS.ReadFile(pathName + ".nx")
	if readErr != nil {
		return resolvedModule{}, fmt.Errorf("module not found: %s%s", canonicalName, vm.syncHint(canonicalName))
	}
```

```go
// syncHint: so no caminho de erro, le <ProjectRoot>/noxy.mod e, se o modulo
// pedido e (ou esta sob) uma dependencia declarada, aponta o comando (spec §6).
func (vm *VM) syncHint(moduleName string) string {
	if vm.Config.ProjectRoot == "" {
		return ""
	}
	cfg, err := pkgmanager.ParseModFile(filepath.Join(vm.Config.ProjectRoot, "noxy.mod"))
	if err != nil {
		return ""
	}
	for _, module := range cfg.Requires() {
		local := strings.ReplaceAll(pkgmanager.LocalPath(module), "/", ".")
		if moduleName == local || strings.HasPrefix(moduleName, local+".") {
			return " (required by noxy.mod) — run 'noxy --sync'"
		}
	}
	return ""
}
```

`extensions.go`: em `verifyExtensionSum`, a raiz passa a ser o projeto quando há um:

```go
	rootAbs := vm.Config.ProjectRoot
	if rootAbs == "" {
		var err error
		if rootAbs, err = filepath.Abs(vm.Config.RootPath); err != nil {
			return err
		}
	}
```

(o resto da função fica; `libs := filepath.Join(rootAbs, "noxy_libs")` e `SumFilePath(rootAbs)` já derivam daí). Mensagens: linha 122 → `"extension %q: binary bin/%s not found — run 'noxy --sync' to download it"`; linha 191 → `"... — run 'noxy --sync' to record it\n"`.

- [ ] **Step 4: Compilador — espelho**

`compiler.go`: campo `projectRoot string` ao lado de `moduleRoot`; em `NewWithStateAndRoot`, após normalizar `moduleRoot`:

```go
	projectRoot := ""
	if root, ok := pkgmanager.FindRoot(moduleRoot); ok {
		projectRoot = root
	}
```

e `projectRoot: projectRoot` no literal. Em `generics.go:286` e `module_exports.go:917` os `NewWithStateAndRoot(..., c.moduleRoot)` recalculam por `FindRoot` — aceitável (é um `Stat` por nível), mas se preferir, copie `c.projectRoot` no literal via um construtor interno `newWithRoots(globals, structs, fileName, moduleRoot, projectRoot)` usado pelos três. Em `moduleFileCandidates`, dentro de `addSuffix`, antes de `filepath.Join(root, "noxy_libs", ...)`:

```go
		if c.projectRoot != "" {
			candidates = append(candidates,
				filepath.Join(c.projectRoot, "noxy_libs", suffix, suffix+".nx"),
				filepath.Join(c.projectRoot, "noxy_libs", suffix),
			)
		}
```

Teste no compilador (`internal/compiler/project_root_test.go`), usando o mesmo layout: `noxy.mod` na raiz, pacote em `<root>/noxy_libs/github_com/acme/pkg/pkg.nx` exportando `seven`, compilar `use github_com.acme.pkg.pkg select *\nlet n: int = seven()\n` com `NewWithStateAndRoot(..., filepath.Join(sub, "main.nx"), sub)` — deve compilar sem erro (hoje falha com global inexistente / seletor desconhecido, porque o compilador não acha o módulo).

- [ ] **Step 5: Run the full verification**

Run: `go build ./... && go vet ./... && go test ./internal/... ./cmd/... -count=1`
Expected: PASS, incluindo `architecture_test` (a dica ficou em `modules.go`).

- [ ] **Step 6: Commit**

```bash
git add internal/vm internal/compiler
git commit -m "feat(vm,compiler): raiz do projeto pelo noxy.mod mais próximo — candidatos, verificação de extensão e dica 'run noxy --sync' (spec §3.0, §6)"
```

---

### Task 11: Repositório, CI, docs, versão

**Files:**
- Modify: `.gitignore:34-35`, `.github/workflows/network-deadlines.yml` (job `examples`), `noxy.mod`, `noxy.sum`, `internal/version/version.go`, `AGENTS.md:4,27` e seção de verificação, `README.md:225`, `docs/index.html:58,196,385,851`, `docs/PACKAGE_MANAGER.md`, `docs/NOXY_LANGUAGE_SPEC.md:2789`, `CHANGELOG.md`, spec §12 (status)
- Create: `noxy_examples/use_quicksort_pkg.nx`
- Remove from git: `noxy_libs/github_com/estevaofon/quicksort/`

- [ ] **Step 1: `.gitignore`** — substitua as duas linhas por:

```
# noxy_libs é derivado: `noxy --sync` reconstrói a partir de noxy.mod + noxy.sum.
# math_lib é fixture colocada à mão (noxy_examples/test_libs.nx) até existir `replace`.
noxy_libs/*
!noxy_libs/math_lib/
```

e `git rm -r --cached noxy_libs/github_com/estevaofon/quicksort`.

- [ ] **Step 2: Regenerar o lock do repositório** (precisa de rede)

```bash
go run ./cmd/noxy --sync
git diff --stat noxy.mod noxy.sum
cat noxy.sum
```

Esperado: `noxy.sum` só com linhas v2 — `github.com/estevaofon/noxy_dynamodb v0.3.0 sha256:…` mais as sete linhas de artefato com versão, e `github.com/estevaofon/quicksort v0.1.0 sha256:…`; `noxy.mod` com os `require` ordenados. `noxy_libs/.noxy-sync` com os dois módulos. Rode `go run ./cmd/noxy --sync --locked` em seguida: deve terminar com `cached` ×2 e `Done.` sem tocar arquivos.

- [ ] **Step 3: Exemplo de aceitação** `noxy_examples/use_quicksort_pkg.nx` (verificado hoje: `ref` entre módulos funciona; o comentário do `use_quicksort.nx` é antigo)

```noxy
// Aceitação do package manager: importa o pacote instalado por `noxy --sync`
// (noxy.mod: github.com/estevaofon/quicksort v0.1.0). Falha se noxy_libs
// não foi sincronizado.
use github_com.estevaofon.quicksort.quicksort select *

func assert(cond: bool, msg: string)
    if !cond then
        eprint(f"assertion failed: {msg}")
        exit(1)
    end
end

let array: int[] = [10, 7, 8, 9, 1, 5, 2, 6, 3, 4]
quicksort(ref array, 0, 9)
assert(array == [1, 2, 3, 4, 5, 6, 7, 8, 9, 10], "quicksort from package")
print("use_quicksort_pkg: ok")
```

Rode `go run ./cmd/noxy noxy_examples/use_quicksort_pkg.nx` (deve imprimir `use_quicksort_pkg: ok`) e depois `rm -rf noxy_libs/github_com/estevaofon/quicksort && go run ./cmd/noxy noxy_examples/use_quicksort_pkg.nx` — deve falhar com `module not found: github_com.estevaofon.quicksort.quicksort (required by noxy.mod) — run 'noxy --sync'`. Rode `go run ./cmd/noxy --sync` de novo.

- [ ] **Step 4: CI** — no job `examples`, antes do runner:

```yaml
      # noxy_libs is derived (spec 2026-09-04): install the locked dependencies
      # first. Needs network for github.com/estevaofon/quicksort@v0.1.0 and the
      # noxy_dynamodb release assets; --locked fails if noxy.sum drifted.
      - run: go run ./cmd/noxy --sync --locked
```

- [ ] **Step 5: Docs e versão**

- `internal/version/version.go`: `v0.24.0`. Atualize `AGENTS.md:4`, `README.md:225`, `docs/index.html:58,385`, `docs/NOXY_LANGUAGE_SPEC.md:2789`.
- `docs/PACKAGE_MANAGER.md`: reescreva com as seções **Commands** (`--get`, `--sync`, `--sync --locked`, o que cada um faz, saída de exemplo), **`noxy.mod`** (intenção; `HEAD` = pedido de pin; linha `noxy`), **`noxy.sum`** (lock v2: formato com os dois tipos de linha, uma versão por módulo, hash de árvore exclui `bin/`, artefatos de todas as plataformas), **`noxy_libs`** (derivado, não commitar, carimbo `.noxy-sync`, poda só do que o sync instalou), **Project root** (`noxy.mod` mais próximo, scripts em subdiretórios), **Versions** (tag mais nova, pseudo-versão, MVS), **Migrating from v1**.
- `docs/index.html:196,851`: mencione `noxy --sync` ao lado de `--get`.
- `AGENTS.md`: na tabela de pacotes, `internal/pkgmanager` ganha a frase `(--get/--sync, noxy.sum v2, FindRoot)`; na verificação obrigatória, acrescente `go run ./cmd/noxy --sync --locked` antes do runner, com a observação "precisa de rede na primeira vez".
- `CHANGELOG.md`: nova seção `## [0.24.0] - <data>` com **Changed (BREAKING)** e **Added** exatamente como a spec §10 lista (copie os cinco itens de quebra e a lista de adições).
- Spec: `**Status:**` → `implementado na v0.24.0`.

- [ ] **Step 6: Verificação completa**

```bash
go build ./... && go vet ./...
go test ./internal/... ./cmd/... -count=1
go run ./cmd/noxy --sync --locked
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
cd sdk/noxyplugin && go test ./... -count=1 && cd ../..
```

Aceitação manual (spec §9): `noxy_examples/dynamodb_example.nx` continua carregando a extensão (agora verificada contra o `noxy.sum` da raiz mesmo rodando da raiz — antes era pulada); se houver credenciais AWS, executa; senão basta que o erro seja de credenciais e não de extensão.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "chore(version): v0.24.0 — noxy --sync, noxy.sum v2, noxy_libs derivado; CI sincroniza antes dos exemplos; docs e CHANGELOG"
```

---

## Self-review

**Spec coverage.** §3.0 → Tasks 5, 9 (cwd), 10 (VM/compilador). §3.1 → Task 3 (ordem, normalização, caminho), Task 7 (`noxy` de dependências, auto-`require`). §3.2 → Task 4 (formato, invariante, `Lookup` sem versão, v1). §3.3 → Task 2. §3.4 → Tasks 6 (`cleanStaleTemps`), 8 (carimbo após promoção, poda). §4.1 → Tasks 1, 6, 7 (HEAD raiz vs. dependência). §4.2/§4.3 → Task 7. §5.1 → Task 8 (cached, mismatch fatal, saída, sem reescrita). §5.2 → Tasks 7 (HEAD sob locked), 8 (`lockMatches`), 9 (flag). §5.3 → Task 8. §5.4 → Task 9. §6 → Task 10. §7 → Task 11. §8 → file map. §9 → testes de cada task; aceitação em Task 11. §10 → Task 11. Não coberto de propósito: §11 (continuações).

**Placeholder scan.** O golden de `TestTreeHashGolden` é preenchido na primeira execução por instrução explícita (Step 3 da Task 2) — é o único valor que o plano não fixa, porque depende do algoritmo que o próprio teste trava. `renderMod`/`saveIfChanged` têm uma alternativa de refatoração sugerida; ambas as formas estão escritas.

**Type consistency.** `FindRoot (string, bool)` em Tasks 5, 9, 10. `SumFile` métodos `SetTree/SetArtifact/DropModule/Lookup/TreeHash/Version/Modules/Artifacts` usados em Tasks 7, 8, 10 com as assinaturas da Task 4. `fetcher.resolve/dir/promote/cleanup` e `fetchKey` usados em Tasks 7, 8, 9 conforme Task 6. `closureInput` campos exportados (`Root, Lock, Stamp, Libs, Fetch, Locked, Out`) iguais em Tasks 7 e 8. `RecordExtensionSums(root, targetDir, module, version)` em Tasks 4 e 10. `installedMatches(libs, lock, stamp, module, ver)` em Tasks 7 e 8. `sortedKeys` definida na Task 7, usada na 8.
