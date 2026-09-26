# `noxy build` — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `noxy build entry.nx -o app` gera um executável único (o próprio `noxy` + payload zip + trailer `NOXYAPP1`) que, numa máquina sem `noxy` nem `noxy_libs`, extrai o projeto para o cache do usuário uma vez e roda o programa como `noxy entry.nx <args>`.

**Architecture:** uma interface única de origem de módulos (`internal/modsrc.Source`, implementação `DiskSource`) substitui as duas resoluções duplicadas (VM e compilador); `internal/bundle` é o formato (trailer, manifesto, zip determinístico, extração atômica); `internal/build` caminha o grafo de `use`, compila cada módulo no build (sem executar) e monta o payload; `cmd/noxy` ganha o subcomando `build` e o modo aplicação (detecção do trailer antes de `flag.Parse`, `os.Args` remontado, `DiskSource` selada). `sys.executable()` + `NOXY_INTERPRETER=1` são o caminho do F5 do editor.

**Tech Stack:** Go 1.25, `archive/zip`, `encoding/json`, `crypto/sha256`. Sem dependências novas.

**Spec:** `docs/superpowers/specs/2026-09-26-noxy-build-design.md` — o plano argumenta a partir dela; leia as duas.

## Global Constraints

- Verificação obrigatória após cada tarefa (AGENTS.md): `go build ./... && go vet ./...`, `gofmt -d` nos arquivos tocados, `go test ./internal/... -count=1`, `go test ./cmd/... -count=1`, `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx` (a partir da Task 10, quando o exemplo novo existe).
- Versão fica `v0.26.0` (`internal/version/version.go` intocado); CHANGELOG dentro da seção `[0.26.0]`.
- Guardas de arquitetura: `loadModule` continua em `internal/vm/modules.go`; `moduleCache` em `module_cache.go`; nativo novo `sys_executable` em `builtins_sys.go` e no snapshot ordenado de `builtins_registry_test.go`; nenhum map global cru no runtime.
- Diagnóstico nunca em stdout: erros do build em `diagOut`; avisos do compilador de módulo em `os.Stderr` (como hoje); `--list` e o resumo do build são produto do comando e vão em stdout.
- Textos literais (testes checam `strings.Contains`): `app payload trailer is inconsistent with the file size`; `app payload is corrupted: sha256 mismatch`; `app payload has an invalid path: <p>`; `cannot extract app payload to <dir>: <err>`; `cannot determine the user cache directory; set NOXY_APP_CACHE`; `unsupported app payload format N`; `module <nome> resolves to <p>, outside the project root <root>`; `<arquivo>:<linha>: module not found: <nome>`; `include <p> not found`; `include "<p>" is outside the project root`; `extension "<nome>": binary bin/<asset> not found — run 'noxy --sync' to download it`; `extension "<nome>" has no binary for <goos>/<goarch> (published: …)`; `sys_load_plugin is not supported in built executables (removed in v0.27.0)`; `warning: macOS output is experimental and was not validated on this platform (see docs/BUILD.md)`.
- Trailer: 64 bytes, `[sha256 32][offset u64 LE][size u64 LE][8 zero][NOXYAPP1]`. Cache: `<NOXY_APP_CACHE | UserCacheDir/noxy/apps>/<16 hex do sha256>`, marcador `.noxy-app-ok`, hash conferido **só** na extração.
- Payload: caminhos com `/`, relativos à raiz do projeto, ordem lexicográfica, deflate, timestamps zerados, `0755` para `bin/*` e arquivos executáveis no disco (Unix).
- Modo aplicação: `os.Args = [os.Args[0], <appdir>/<entry>, args...]`, cwd inalterado, `NOXY_PATH` ignorada, sem REPL nem flags; `NOXY_INTERPRETER` não vazia desliga o modo aplicação.
- `go build` do binário nos testes de `cmd/noxy` segue `sync_flags_test.go` (`.exe` no Windows). Processos de extensão: `t.Cleanup(machine.CloseExtensions)` antes do `RemoveAll` do `TempDir` (Windows).

## Review Focus

1. **Programa rodado de um cwd com módulos homônimos e `NOXY_PATH` apontando para outro `math_lib`**: o app selado ignora os dois e usa o `math_lib` embutido (Task 10, `TestBuildAppRunsWithoutNoxyLibs`: `NOXY_PATH` de isca com `add` devolvendo -1).
2. **`-o dist/app` com `dist/` inexistente**: o build cria o diretório e grava (Task 9, `TestWriteCreatesOutputDirectoryAndIsDeterministic`).
3. **`--include ./web/` e `--include web/vendor/x.js` sobrepostos**: normalização e nenhum arquivo duplicado no zip (Task 8, `TestPlanIncludesUnionWithoutDuplicates`).
4. **Módulo de diretório com arquivos que não são `.nx`** (README, assets): entram só os `.nx` e subdiretórios que a VM listaria (Task 8, `TestPlanWalksTransitiveUsesAndDirectoryModules`).
5. **App invocado por symlink** (`/usr/local/bin/noxy-editor -> ~/dist/noxy-editor`): `os.Executable` + `EvalSymlinks` acham o trailer (Task 10, `TestBuildAppRunsThroughASymlink`, Unix).

---

## File map

| Arquivo | Estado | Responsabilidade |
|---|---|---|
| `internal/bundle/trailer.go` | novo | `Trailer`, `Marshal`/`ParseTrailer`, `Open` (payload do próprio executável), `Payload.Reader/Close/CacheKey`, `RuntimeBytes` |
| `internal/bundle/manifest.go` | novo | `Manifest`, `ModuleEntry`, `ExtensionEntry`, `Encode`/`DecodeManifest`, nomes reservados |
| `internal/bundle/pack.go` | novo | `File`, `ValidPath`, `Pack` (zip determinístico) |
| `internal/bundle/extract.go` | novo | `CacheBase`, `Extract` (hash, temporário, marcador, rename, corrida), `ErrCorrupted` |
| `internal/bundle/*_test.go` | novo | testes do formato |
| `internal/modsrc/source.go` | novo | `Source`, `Module`, `Kind`, `Entry`, `ErrNotFound` |
| `internal/modsrc/disk.go` | novo | `DiskSource`, `NewDisk`, `NewSealed`, ordem de candidatos, `Key` |
| `internal/modsrc/disk_test.go` | novo | ordem, selo, chave |
| `internal/compiler/compiler.go`, `generics.go`, `module_exports.go` | modificar | `moduleSource` + `SetModuleSource`; descoberta via `Source`; `moduleFileCandidates` removido |
| `internal/compiler/module_source_test.go` | novo | `Source` de memória |
| `internal/vm/vm.go`, `modules.go`, `extensions.go` | modificar | `VMConfig.Source`; `resolveModule` via `Source`; `compileModule`/`runModule`; `CompileModule` público; manifesto e `.wasm` via `Source` |
| `internal/vm/module_source_test.go` | novo | `Source` de memória na VM |
| `internal/pkgmanager/modfile.go`, `modfile_test.go` | modificar | `Include`, `ValidateIncludePath`, `Save` preserva |
| `internal/vm/builtins_sys.go`, `internal/stdlib/sys.nx`, `builtins_registry_test.go`, `builtins_sys_executable_test.go` | modificar/novo | `sys_executable` / `sys.executable()` |
| `internal/build/plan.go`, `list.go`, `write.go`, `*_test.go` | novo | `Options`, `Plan`, `MakePlan`, `List`, `Write`, `HumanSize` |
| `cmd/noxy/build.go`, `appmode.go`, `main.go` | novo/modificar | subcomando `build`, `parseBuildArgs`, `appModeExitCode`, `runWithVMConfig` |
| `cmd/noxy/build_args_test.go`, `build_test.go` | novo | parser das flags; integração com o binário |
| `noxy_examples/build_app.nx`, `noxy_examples/build_app_assets/greeting.txt` | novo | exemplo/fixture |
| `docs/BUILD.md`, `README.md`, `CHANGELOG.md`, `AGENTS.md`, `docs/NOXY_LANGUAGE_SPEC.md` | novo/modificar | documentação |
| `../noxy_projects/Noxy-Editor/noxy.mod`, `src/runner.nx`, `README.md` | modificar (outro repositório) | fecho: `include web`, F5 por `sys.executable()` |

---

### Task 1: `internal/bundle` — trailer e leitura do payload

**Files:**
- Create: `internal/bundle/trailer.go`
- Test: `internal/bundle/trailer_test.go`

**Interfaces:**
- Produces: `const Magic = "NOXYAPP1"`, `const TrailerSize = 64`, `var ErrInconsistentTrailer`, `type Trailer struct { SHA256 [32]byte; Offset, Size uint64 }`, `func (t Trailer) Marshal() []byte`, `func ParseTrailer(b []byte) (Trailer, bool)`, `type Payload struct { Path string; Trailer Trailer }`, `func Open(path string) (*Payload, error)` (`nil, nil` = sem payload), `func (p *Payload) Reader() *io.SectionReader`, `func (p *Payload) Close() error`, `func (p *Payload) CacheKey() string` (16 hex), `func RuntimeBytes(path string) ([]byte, error)`.

- [ ] **Step 1: testes (falham: pacote inexistente)**

```go
// internal/bundle/trailer_test.go
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// writeApp grava runtime + payload + trailer(hash real do payload) e devolve o caminho.
func writeApp(t *testing.T, runtime, payload []byte) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	tr := Trailer{SHA256: sum, Offset: uint64(len(runtime)), Size: uint64(len(payload))}
	path := filepath.Join(t.TempDir(), "app")
	data := append(append(append([]byte{}, runtime...), payload...), tr.Marshal()...)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTrailerRoundTrip(t *testing.T) {
	var sum [32]byte
	for i := range sum {
		sum[i] = byte(i)
	}
	in := Trailer{SHA256: sum, Offset: 12345, Size: 678}
	b := in.Marshal()
	if len(b) != TrailerSize || string(b[56:]) != Magic {
		t.Fatalf("marshal: len %d, tail %q", len(b), b[56:])
	}
	for _, zero := range b[48:56] {
		if zero != 0 {
			t.Fatalf("reserved bytes must be zero: %v", b[48:56])
		}
	}
	out, ok := ParseTrailer(b)
	if !ok || out != in {
		t.Fatalf("round trip: ok=%v %+v", ok, out)
	}
	if _, ok := ParseTrailer(b[:63]); ok {
		t.Fatal("short trailer must not parse")
	}
	b[63] = 'X'
	if _, ok := ParseTrailer(b); ok {
		t.Fatal("wrong magic must not parse")
	}
}

func TestOpenWithoutPayloadIsNotAnError(t *testing.T) {
	for _, content := range []string{"abc", string(bytes.Repeat([]byte("plain noxy bytes "), 10))} {
		path := filepath.Join(t.TempDir(), "noxy")
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
		p, err := Open(path)
		if err != nil || p != nil {
			t.Fatalf("plain file: payload=%v err=%v", p, err)
		}
	}
}

func TestOpenRejectsInconsistentTrailer(t *testing.T) {
	runtime := bytes.Repeat([]byte("R"), 100)
	payload := []byte("PAYLOAD")
	tr := Trailer{Offset: 100, Size: 999}
	path := filepath.Join(t.TempDir(), "app")
	data := append(append(append([]byte{}, runtime...), payload...), tr.Marshal()...)
	if err := os.WriteFile(path, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrInconsistentTrailer) {
		t.Fatalf("want ErrInconsistentTrailer, got %v", err)
	}
	if _, err := RuntimeBytes(path); !errors.Is(err, ErrInconsistentTrailer) {
		t.Fatalf("RuntimeBytes: want ErrInconsistentTrailer, got %v", err)
	}
}

func TestOpenReadsPayloadSectionAndCacheKey(t *testing.T) {
	runtime := bytes.Repeat([]byte("R"), 100)
	payload := []byte("ZIPDATA")
	path := writeApp(t, runtime, payload)
	p, err := Open(path)
	if err != nil || p == nil {
		t.Fatalf("open: %v %v", p, err)
	}
	defer p.Close()
	got, err := io.ReadAll(p.Reader())
	if err != nil || string(got) != "ZIPDATA" {
		t.Fatalf("section: %q %v", got, err)
	}
	sum := sha256.Sum256(payload)
	if p.CacheKey() != hex.EncodeToString(sum[:8]) {
		t.Fatalf("cache key %q", p.CacheKey())
	}
	if p.Trailer.Offset != 100 || p.Trailer.Size != 7 {
		t.Fatalf("trailer %+v", p.Trailer)
	}
}

func TestRuntimeBytesStopAtPayload(t *testing.T) {
	runtime := []byte("RUNTIME-BYTES")
	app := writeApp(t, runtime, []byte("payload"))
	got, err := RuntimeBytes(app)
	if err != nil || !bytes.Equal(got, runtime) {
		t.Fatalf("app: %q %v", got, err)
	}
	plain := filepath.Join(t.TempDir(), "noxy")
	if err := os.WriteFile(plain, runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = RuntimeBytes(plain)
	if err != nil || !bytes.Equal(got, runtime) {
		t.Fatalf("plain: %q %v", got, err)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/bundle/ -count=1`
Expected: FAIL (build failed: undefined: Trailer, Open, ...).

- [ ] **Step 3: implementação**

```go
// internal/bundle/trailer.go
// Package bundle e o formato do executavel gerado por `noxy build` (spec
// 2026-09-26 §4): [runtime][payload zip][trailer de 64 bytes]. O trailer
// localiza o payload; o hash so e conferido na extracao (§5.2).
package bundle

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
)

const (
	Magic       = "NOXYAPP1"
	TrailerSize = 64
)

var ErrInconsistentTrailer = errors.New("app payload trailer is inconsistent with the file size")

// Trailer e o rodape fixo: sha256 do payload, offset e tamanho (u64 LE),
// 8 bytes reservados, magic.
type Trailer struct {
	SHA256 [32]byte
	Offset uint64
	Size   uint64
}

func (t Trailer) Marshal() []byte {
	b := make([]byte, TrailerSize)
	copy(b[0:32], t.SHA256[:])
	binary.LittleEndian.PutUint64(b[32:40], t.Offset)
	binary.LittleEndian.PutUint64(b[40:48], t.Size)
	copy(b[56:64], Magic)
	return b
}

// ParseTrailer aceita exatamente 64 bytes terminados pelo magic.
func ParseTrailer(b []byte) (Trailer, bool) {
	if len(b) != TrailerSize || string(b[56:64]) != Magic {
		return Trailer{}, false
	}
	var t Trailer
	copy(t.SHA256[:], b[0:32])
	t.Offset = binary.LittleEndian.Uint64(b[32:40])
	t.Size = binary.LittleEndian.Uint64(b[40:48])
	return t, true
}

// consistent: o payload cabe exatamente entre o runtime e o trailer.
func (t Trailer) consistent(total uint64) bool {
	if total < TrailerSize || t.Offset >= total || t.Size > total-TrailerSize {
		return false
	}
	return t.Offset == total-TrailerSize-t.Size
}

// Payload e o trecho zip do executavel em path, aberto para leitura.
type Payload struct {
	Path    string
	Trailer Trailer
	file    *os.File
}

// Open le o trailer de path. Sem magic (ou arquivo curto): (nil, nil) — e o
// noxy comum. Magic com offset/tamanho que nao batem com o arquivo:
// ErrInconsistentTrailer.
func Open(path string) (*Payload, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() < TrailerSize {
		f.Close()
		return nil, nil
	}
	buf := make([]byte, TrailerSize)
	if _, err := f.ReadAt(buf, info.Size()-TrailerSize); err != nil {
		f.Close()
		return nil, err
	}
	t, ok := ParseTrailer(buf)
	if !ok {
		f.Close()
		return nil, nil
	}
	if !t.consistent(uint64(info.Size())) {
		f.Close()
		return nil, ErrInconsistentTrailer
	}
	return &Payload{Path: path, Trailer: t, file: f}, nil
}

func (p *Payload) Reader() *io.SectionReader {
	return io.NewSectionReader(p.file, int64(p.Trailer.Offset), int64(p.Trailer.Size))
}

func (p *Payload) Close() error { return p.file.Close() }

// CacheKey: os 16 primeiros hex do sha256 do payload — o nome do diretorio
// de extracao (spec §5.2).
func (p *Payload) CacheKey() string { return hex.EncodeToString(p.Trailer.SHA256[:8]) }

// RuntimeBytes devolve os bytes do runtime em path: o arquivo inteiro se
// ele nao carrega payload, senao [0:offset) — `noxy build` rodando de
// dentro de um app (NOXY_INTERPRETER=1) nao aninha payloads (spec §4.1).
func RuntimeBytes(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) < TrailerSize {
		return data, nil
	}
	t, ok := ParseTrailer(data[len(data)-TrailerSize:])
	if !ok {
		return data, nil
	}
	if !t.consistent(uint64(len(data))) {
		return nil, ErrInconsistentTrailer
	}
	return data[:t.Offset], nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/bundle/ -count=1 && go vet ./internal/bundle/`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/bundle/trailer.go internal/bundle/trailer_test.go
git commit -m "feat(bundle): trailer NOXYAPP1 e leitura do payload do proprio executavel"
```

---

### Task 2: `internal/bundle` — manifesto, zip determinístico e extração atômica

**Files:**
- Create: `internal/bundle/manifest.go`, `internal/bundle/pack.go`, `internal/bundle/extract.go`
- Test: `internal/bundle/pack_test.go`, `internal/bundle/extract_test.go`

**Interfaces:**
- Consumes: `Trailer`, `Payload`, `Open`, `writeApp` (teste) da Task 1.
- Produces: `const FormatVersion = 1`, `KindSource = "source"`, `ManifestName = "noxy-app.json"`, `MarkerName = ".noxy-app-ok"`; `type ModuleEntry struct { Name, Path string; Dir bool }`; `type ExtensionEntry struct { Name, Kind, Module, Dir, Artifact, SHA256 string }`; `type Manifest struct { Format int; Kind, Noxy, Target, Entry string; Modules []ModuleEntry; Extensions []ExtensionEntry; Includes []string }`; `func (m *Manifest) Encode() ([]byte, error)`; `func DecodeManifest(data []byte) (*Manifest, error)`; `type File struct { Path string; Data []byte; Executable bool }`; `func ValidPath(p string) bool`; `func Pack(m *Manifest, files []File) ([]byte, error)`; `var ErrCorrupted`; `func CacheBase() (string, error)`; `func Extract(p *Payload, base string) (appDir string, m *Manifest, err error)`.

- [ ] **Step 1: testes (falham)**

```go
// internal/bundle/pack_test.go
package bundle

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func sampleManifest() *Manifest {
	return &Manifest{Format: FormatVersion, Kind: KindSource, Noxy: "v0.26.0", Target: "linux/amd64", Entry: "main.nx",
		Modules: []ModuleEntry{{Name: "src.a", Path: "src/a.nx"}}, Extensions: []ExtensionEntry{}, Includes: []string{"web"}}
}

func TestManifestRoundTripAndFormatCheck(t *testing.T) {
	data, err := sampleManifest().Encode()
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeManifest(data)
	if err != nil || m.Entry != "main.nx" || m.Modules[0].Name != "src.a" || m.Includes[0] != "web" {
		t.Fatalf("decode: %+v %v", m, err)
	}
	if _, err := DecodeManifest([]byte(`{"format": 2, "entry": "main.nx"}`)); err == nil || !strings.Contains(err.Error(), "unsupported app payload format 2") {
		t.Fatalf("format 2: %v", err)
	}
	if _, err := DecodeManifest([]byte(`{"format": 1, "entry": "../x.nx"}`)); err == nil {
		t.Fatal("entry escaping the root must be rejected")
	}
}

func TestValidPath(t *testing.T) {
	for _, ok := range []string{"main.nx", "src/a.nx", "noxy_libs/github_com/x/y/bin/z", "web/vendor/x.js"} {
		if !ValidPath(ok) {
			t.Errorf("%q must be valid", ok)
		}
	}
	for _, bad := range []string{"", "/abs", "C:/x", "a\\b", "../x", "a/../b", "./a", "a/", "a//b"} {
		if ValidPath(bad) {
			t.Errorf("%q must be invalid", bad)
		}
	}
}

func TestPackIsDeterministicSortedAndCarriesTheManifest(t *testing.T) {
	files := []File{{Path: "src/a.nx", Data: []byte("func a() -> int\n    return 1\nend\n")}, {Path: "bin/tool", Data: []byte("BIN"), Executable: true}, {Path: "main.nx", Data: []byte("print(1)\n")}}
	one, err := Pack(sampleManifest(), files)
	if err != nil {
		t.Fatal(err)
	}
	two, err := Pack(sampleManifest(), files)
	if err != nil || !bytes.Equal(one, two) {
		t.Fatalf("two packs of the same input differ (err %v)", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(one), int64(len(one)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
		if f.Method != zip.Deflate {
			t.Errorf("%s: method %d, want deflate", f.Name, f.Method)
		}
		if !f.Modified.IsZero() && f.Modified.Year() > 1980 {
			t.Errorf("%s: timestamp %v must be zeroed", f.Name, f.Modified)
		}
	}
	want := []string{"bin/tool", "main.nx", ManifestName, "src/a.nx"}
	if strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("entries %v, want %v", names, want)
	}
	if zr.File[0].Mode()&0o111 == 0 {
		t.Fatal("bin/tool must be executable in the zip")
	}
	if zr.File[1].Mode()&0o111 != 0 {
		t.Fatal("main.nx must not be executable")
	}
}

func TestPackRejectsInvalidAndDuplicatePaths(t *testing.T) {
	if _, err := Pack(sampleManifest(), []File{{Path: "../x", Data: nil}}); err == nil || !strings.Contains(err.Error(), "invalid payload path") {
		t.Fatalf("invalid: %v", err)
	}
	if _, err := Pack(sampleManifest(), []File{{Path: "a.nx"}, {Path: "a.nx"}}); err == nil || !strings.Contains(err.Error(), "duplicate payload path") {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := Pack(sampleManifest(), []File{{Path: ManifestName}}); err == nil {
		t.Fatal("a file named noxy-app.json must collide with the manifest")
	}
}
```

```go
// internal/bundle/extract_test.go
package bundle

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func packedApp(t *testing.T) (string, []byte) {
	t.Helper()
	payload, err := Pack(sampleManifest(), []File{
		{Path: "main.nx", Data: []byte("print(1)\n")},
		{Path: "src/a.nx", Data: []byte("func a() -> int\n    return 1\nend\n")},
		{Path: "noxy_libs/p/bin/tool", Data: []byte("BIN"), Executable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	return writeApp(t, []byte("RUNTIME"), payload), payload
}

func openApp(t *testing.T, path string) *Payload {
	t.Helper()
	p, err := Open(path)
	if err != nil || p == nil {
		t.Fatalf("open %s: %v %v", path, p, err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func TestExtractWritesFilesMarkerAndReturnsTheManifest(t *testing.T) {
	app, _ := packedApp(t)
	base := t.TempDir()
	dir, m, err := Extract(openApp(t, app), base)
	if err != nil {
		t.Fatal(err)
	}
	if m.Entry != "main.nx" || filepath.Dir(dir) != base {
		t.Fatalf("dir %s manifest %+v", dir, m)
	}
	for _, rel := range []string{"main.nx", "src/a.nx", "noxy_libs/p/bin/tool", ManifestName, MarkerName} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s missing: %v", rel, err)
		}
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, "noxy_libs", "p", "bin", "tool"))
		if info.Mode()&0o111 == 0 {
			t.Fatal("bin/tool must be executable after extraction")
		}
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 {
		t.Fatalf("base must hold exactly the app dir, got %d entries", len(entries))
	}
}

func TestExtractChecksTheHashOnlyOnFirstExtraction(t *testing.T) {
	app, payload := packedApp(t)
	data, _ := os.ReadFile(app)
	corrupted := append([]byte{}, data...)
	corrupted[len("RUNTIME")+len(payload)/2] ^= 0xFF // dentro do payload, trailer intacto
	corruptedPath := filepath.Join(t.TempDir(), "app-corrupted")
	if err := os.WriteFile(corruptedPath, corrupted, 0o755); err != nil {
		t.Fatal(err)
	}

	fresh := t.TempDir()
	if _, _, err := Extract(openApp(t, corruptedPath), fresh); !errors.Is(err, ErrCorrupted) {
		t.Fatalf("corrupted payload on a fresh cache: want ErrCorrupted, got %v", err)
	}
	if entries, _ := os.ReadDir(fresh); len(entries) != 0 {
		t.Fatalf("a failed extraction must leave nothing behind, got %v", entries)
	}

	base := t.TempDir()
	dir, _, err := Extract(openApp(t, app), base)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, MarkerName)
	before, _ := os.Stat(marker)
	// Mesmo trailer, payload corrompido: o marcador basta, nenhum hash e conferido.
	if _, _, err := Extract(openApp(t, corruptedPath), base); err != nil {
		t.Fatalf("second start must trust the marker, got %v", err)
	}
	after, _ := os.Stat(marker)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("second start must not re-extract")
	}
}

func TestExtractRejectsEscapingPaths(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../escape.txt")
	w.Write([]byte("x"))
	m, _ := sampleManifest().Encode()
	w, _ = zw.Create(ManifestName)
	w.Write(m)
	zw.Close()
	app := writeApp(t, []byte("RUNTIME"), buf.Bytes())
	base := t.TempDir()
	_, _, err := Extract(openApp(t, app), base)
	if err == nil || !strings.Contains(err.Error(), "app payload has an invalid path: ../escape.txt") {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Fatalf("nothing may remain after a rejected payload, got %v", entries)
	}
}

func TestExtractConcurrentStartsAgreeOnOneDirectory(t *testing.T) {
	app, _ := packedApp(t)
	base := t.TempDir()
	var wg sync.WaitGroup
	dirs := make([]string, 4)
	errs := make([]error, 4)
	for i := range dirs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, err := Open(app)
			if err != nil {
				errs[i] = err
				return
			}
			defer p.Close()
			dirs[i], _, errs[i] = Extract(p, base)
		}(i)
	}
	wg.Wait()
	for i := range dirs {
		if errs[i] != nil || dirs[i] != dirs[0] {
			t.Fatalf("extractor %d: dir %q err %v (first %q)", i, dirs[i], errs[i], dirs[0])
		}
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 1 {
		t.Fatalf("temporaries must be gone, got %d entries", len(entries))
	}
}

func TestCacheBaseHonoursOverride(t *testing.T) {
	t.Setenv("NOXY_APP_CACHE", filepath.Join(t.TempDir(), "override"))
	base, err := CacheBase()
	if err != nil || !strings.HasSuffix(base, "override") {
		t.Fatalf("%q %v", base, err)
	}
	t.Setenv("NOXY_APP_CACHE", "")
	base, err = CacheBase()
	if err != nil || !strings.HasSuffix(filepath.ToSlash(base), "noxy/apps") {
		t.Fatalf("default base %q %v", base, err)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/bundle/ -count=1`
Expected: FAIL (undefined: Manifest, Pack, Extract, ...).

- [ ] **Step 3: implementação**

```go
// internal/bundle/manifest.go
package bundle

import (
	"encoding/json"
	"fmt"
)

const (
	FormatVersion = 1
	KindSource    = "source"
	ManifestName  = "noxy-app.json"
	MarkerName    = ".noxy-app-ok"
)

type ModuleEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
}

type ExtensionEntry struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Module   string `json:"module"`
	Dir      string `json:"dir"`
	Artifact string `json:"artifact"`
	SHA256   string `json:"sha256"`
}

// Manifest e o noxy-app.json do payload (spec §4.4).
type Manifest struct {
	Format     int              `json:"format"`
	Kind       string           `json:"kind"`
	Noxy       string           `json:"noxy"`
	Target     string           `json:"target"`
	Entry      string           `json:"entry"`
	Modules    []ModuleEntry    `json:"modules"`
	Extensions []ExtensionEntry `json:"extensions"`
	Includes   []string         `json:"includes"`
}

func (m *Manifest) Encode() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// DecodeManifest recusa formato desconhecido (um runtime antigo diante de
// um payload novo) e entry fora da raiz.
func DecodeManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", ManifestName, err)
	}
	if m.Format != FormatVersion {
		return nil, fmt.Errorf("unsupported app payload format %d", m.Format)
	}
	if !ValidPath(m.Entry) {
		return nil, fmt.Errorf("%s: invalid entry %q", ManifestName, m.Entry)
	}
	return &m, nil
}
```

```go
// internal/bundle/pack.go
package bundle

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
)

// File e um arquivo do payload: caminho relativo a raiz do projeto com "/".
type File struct {
	Path       string
	Data       []byte
	Executable bool
}

// ValidPath: relativo, com "/", ja limpo (sem ".", "..", "//", barra final),
// sem "\" nem letra de unidade.
func ValidPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	if len(p) >= 2 && p[1] == ':' {
		return false
	}
	if path.Clean(p) != p {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// Pack monta o zip do payload: manifesto + arquivos, ordem lexicografica,
// deflate, timestamps zerados (spec §4.3) — mesmo conteudo, mesmos bytes.
func Pack(m *Manifest, files []File) ([]byte, error) {
	manifest, err := m.Encode()
	if err != nil {
		return nil, err
	}
	all := make([]File, 0, len(files)+1)
	all = append(all, File{Path: ManifestName, Data: manifest})
	all = append(all, files...)
	sort.SliceStable(all, func(i, j int) bool { return all[i].Path < all[j].Path })
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	seen := make(map[string]bool, len(all))
	for _, f := range all {
		if !ValidPath(f.Path) {
			return nil, fmt.Errorf("invalid payload path %q", f.Path)
		}
		if seen[f.Path] {
			return nil, fmt.Errorf("duplicate payload path %q", f.Path)
		}
		seen[f.Path] = true
		header := &zip.FileHeader{Name: f.Path, Method: zip.Deflate}
		mode := os.FileMode(0o644)
		if f.Executable {
			mode = 0o755
		}
		header.SetMode(mode)
		w, err := zw.CreateHeader(header)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(f.Data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
```

```go
// internal/bundle/extract.go
package bundle

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

var ErrCorrupted = errors.New("app payload is corrupted: sha256 mismatch")

// CacheBase: NOXY_APP_CACHE ou <UserCacheDir>/noxy/apps (spec §3.3, §5.2).
func CacheBase() (string, error) {
	if dir := os.Getenv("NOXY_APP_CACHE"); dir != "" {
		return dir, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", errors.New("cannot determine the user cache directory; set NOXY_APP_CACHE")
	}
	return filepath.Join(base, "noxy", "apps"), nil
}

// Extract garante <base>/<CacheKey> extraido e devolve o diretorio e o
// manifesto. Com o marcador presente nao le o payload nem confere hash
// (decisao da spec §5.2). Sem marcador: hash, zip para um temporario ao
// lado, marcador, rename — outra instancia que vencer a corrida e aceita.
func Extract(p *Payload, base string) (string, *Manifest, error) {
	appDir := filepath.Join(base, p.CacheKey())
	if m, err := readExtracted(appDir); err == nil {
		return appDir, m, nil
	}
	data, err := io.ReadAll(p.Reader())
	if err != nil {
		return "", nil, fmt.Errorf("app payload: %w", err)
	}
	if sha256.Sum256(data) != p.Trailer.SHA256 {
		return "", nil, ErrCorrupted
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, fmt.Errorf("app payload: %w", err)
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	tmp, err := os.MkdirTemp(base, p.CacheKey()+".tmp-"+strconv.Itoa(os.Getpid())+"-")
	if err != nil {
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	if err := unzipTo(zr, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", nil, err
	}
	marker := hex.EncodeToString(p.Trailer.SHA256[:]) + "\n"
	if err := os.WriteFile(filepath.Join(tmp, MarkerName), []byte(marker), 0o644); err != nil {
		os.RemoveAll(tmp)
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	if err := os.Rename(tmp, appDir); err != nil {
		os.RemoveAll(tmp)
		if m, readErr := readExtracted(appDir); readErr == nil {
			return appDir, m, nil
		}
		return "", nil, fmt.Errorf("cannot extract app payload to %s: %w", appDir, err)
	}
	m, err := readExtracted(appDir)
	if err != nil {
		return "", nil, err
	}
	return appDir, m, nil
}

func readExtracted(appDir string) (*Manifest, error) {
	if _, err := os.Stat(filepath.Join(appDir, MarkerName)); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(appDir, ManifestName))
	if err != nil {
		return nil, err
	}
	return DecodeManifest(data)
}

func unzipTo(zr *zip.Reader, dir string) error {
	for _, f := range zr.File {
		if !ValidPath(f.Name) || f.Name == MarkerName {
			return fmt.Errorf("app payload has an invalid path: %s", f.Name)
		}
		dest := filepath.Join(dir, filepath.FromSlash(f.Name))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.Mode()&0o111 != 0 {
			mode = 0o755
		}
		if err := writeEntry(f, dest, mode); err != nil {
			return err
		}
	}
	return nil
}

func writeEntry(f *zip.File, dest string, mode os.FileMode) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, rc); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/bundle/ -count=1 -race && go vet ./internal/bundle/`
Expected: PASS (inclusive o teste concorrente sob `-race`).

- [ ] **Step 5: commit**

```bash
git add internal/bundle/
git commit -m "feat(bundle): manifesto noxy-app.json, zip deterministico e extracao atomica para o cache"
```

---

### Task 3: `internal/modsrc` — a origem única de módulos

**Files:**
- Create: `internal/modsrc/source.go`, `internal/modsrc/disk.go`
- Test: `internal/modsrc/disk_test.go`

**Interfaces:**
- Produces: `type Kind uint8` (`KindFile`, `KindDirectory`, `KindEmbedded`); `type Module struct { Name string; Kind Kind; Path string; Content string }`; `type Entry struct { Name string; IsDir bool }`; `var ErrNotFound`; `type Source interface { Key() string; Resolve(name string) (Module, error); ReadFile(path string) ([]byte, error); ReadDir(path string) ([]Entry, error) }`; `type DiskSource struct { Root, ProjectRoot string; SearchPaths []string; SearchCwd bool }`; `func NewDisk(root, projectRoot string) *DiskSource`; `func NewSealed(root, projectRoot string) *DiskSource`.

- [ ] **Step 1: testes (falham)**

```go
// internal/modsrc/disk_test.go
package modsrc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiskResolveOrderAndKinds(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "noxy_libs", "pkg", "pkg.nx"), "let v: int = 1\n")
	write(t, filepath.Join(root, "src", "a.nx"), "let a: int = 1\n")
	write(t, filepath.Join(root, "src", "dir", "x.nx"), "let x: int = 1\n")
	write(t, filepath.Join(root, "withmain", "main.nx"), "let m: int = 1\n")
	write(t, filepath.Join(root, "strings.nx"), "func shadow() -> int\n    return 1\nend\n")
	d := &DiskSource{Root: root, ProjectRoot: root}

	cases := []struct {
		name string
		kind Kind
		path string
	}{
		{"pkg", KindFile, filepath.Join(root, "noxy_libs", "pkg", "pkg.nx")},
		{"src.a", KindFile, filepath.Join(root, "src", "a.nx")},
		{"src.dir", KindDirectory, filepath.Join(root, "src", "dir")},
		{"src", KindDirectory, filepath.Join(root, "src")},
		{"withmain", KindFile, filepath.Join(root, "withmain", "main.nx")},
		{"strings", KindFile, filepath.Join(root, "strings.nx")},
	}
	for _, tc := range cases {
		m, err := d.Resolve(tc.name)
		if err != nil || m.Kind != tc.kind || m.Path != tc.path || m.Name != tc.name {
			t.Errorf("%s: got %+v err %v, want kind %d path %s", tc.name, m, err, tc.kind, tc.path)
		}
	}
	io, err := d.Resolve("io")
	if err != nil || io.Kind != KindEmbedded || io.Content == "" || io.Path != "" {
		t.Fatalf("io must come from the embedded stdlib: %+v %v", io, err)
	}
	if _, err := d.Resolve("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	entries, err := d.ReadDir(filepath.Join(root, "src"))
	if err != nil || len(entries) != 2 || entries[0].Name != "a.nx" || entries[0].IsDir || entries[1].Name != "dir" || !entries[1].IsDir {
		t.Fatalf("ReadDir: %+v %v", entries, err)
	}
}

func TestDiskProjectLibsBeforeRootLibs(t *testing.T) {
	project := t.TempDir()
	sub := filepath.Join(project, "examples")
	write(t, filepath.Join(project, "noxy_libs", "m", "m.nx"), "let where: string = \"project\"\n")
	write(t, filepath.Join(sub, "noxy_libs", "m", "m.nx"), "let where: string = \"root\"\n")
	d := &DiskSource{Root: sub, ProjectRoot: project}
	m, err := d.Resolve("m")
	if err != nil || m.Path != filepath.Join(project, "noxy_libs", "m", "m.nx") {
		t.Fatalf("project noxy_libs must win: %+v %v", m, err)
	}
	if m, err := (&DiskSource{Root: sub}).Resolve("m"); err != nil || m.Path != filepath.Join(sub, "noxy_libs", "m", "m.nx") {
		t.Fatalf("without a project root the script dir wins: %+v %v", m, err)
	}
}

func TestSealedIgnoresNoxyPathAndCwd(t *testing.T) {
	root := t.TempDir()
	searchPath := t.TempDir()
	write(t, filepath.Join(searchPath, "far", "far.nx"), "let f: int = 1\n")
	cwd := t.TempDir()
	write(t, filepath.Join(cwd, "noxy_libs", "near", "near.nx"), "let n: int = 1\n")
	t.Setenv("NOXY_PATH", searchPath)
	t.Chdir(cwd)

	open := NewDisk(root, "")
	if m, err := open.Resolve("far"); err != nil || m.Path != filepath.Join(searchPath, "far", "far.nx") {
		t.Fatalf("open source must honour NOXY_PATH: %+v %v", m, err)
	}
	if _, err := open.Resolve("near"); err != nil {
		t.Fatalf("open source must search the cwd: %v", err)
	}
	sealed := NewSealed(root, "")
	if _, err := sealed.Resolve("far"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sealed source must ignore NOXY_PATH, got %v", err)
	}
	if _, err := sealed.Resolve("near"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sealed source must ignore the cwd, got %v", err)
	}
	if open.Key() == sealed.Key() {
		t.Fatal("keys must differ between an open and a sealed source")
	}
	t.Setenv("NOXY_PATH", "")
	if NewDisk(root, "").Key() == open.Key() {
		t.Fatal("key must change with the search paths")
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/modsrc/ -count=1`
Expected: FAIL (pacote sem fonte / símbolos indefinidos).

- [ ] **Step 3: implementação**

```go
// internal/modsrc/source.go
// Package modsrc e a UNICA origem de modulos do Noxy: compilador (descoberta
// de exports e tipos) e VM (carga em runtime) resolvem `use` por Source e
// nao sabem de onde o fonte veio (spec 2026-09-26 §7). Na v1 ha uma
// implementacao, DiskSource; a v2 (bytecode) adiciona a leitura do payload.
package modsrc

import "errors"

type Kind uint8

const (
	KindFile      Kind = iota // arquivo .nx (inclusive <dir>/<dir>.nx e <dir>/main.nx)
	KindDirectory             // diretorio sem entrada: cada .nx e subdiretorio e submodulo
	KindEmbedded              // stdlib embutida (stdlib.FS); Content preenchido
)

type Module struct {
	Name    string // "src.session"
	Kind    Kind
	Path    string // absoluto e limpo; "" para KindEmbedded
	Content string // so KindEmbedded
}

type Entry struct {
	Name  string
	IsDir bool
}

// ErrNotFound: nenhum candidato no disco e nada na stdlib embutida. Quem
// chama acrescenta a dica de `noxy --sync` (pkgmanager.SyncHint).
var ErrNotFound = errors.New("module not found")

type Source interface {
	// Key identifica a origem no cache de modulos da VM (raiz real + search
	// paths + selo): duas fontes com a mesma chave resolvem igual.
	Key() string
	Resolve(name string) (Module, error)
	ReadFile(path string) ([]byte, error)
	ReadDir(path string) ([]Entry, error)
}
```

```go
// internal/modsrc/disk.go
package modsrc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/estevaofon/noxy/internal/stdlib"
)

// DiskSource resolve na ordem que a VM e o compilador sempre usaram (spec
// §0): NOXY_PATH → <projeto>/noxy_libs → <raiz>/noxy_libs → <raiz>/stdlib →
// <raiz>/<nome> → os mesmos relativos ao cwd → stdlib embutida. Selada
// (modo aplicacao) nao tem SearchPaths nem SearchCwd: nada fora da raiz
// sombreia o app.
type DiskSource struct {
	Root        string
	ProjectRoot string
	SearchPaths []string
	SearchCwd   bool
}

func NewDisk(root, projectRoot string) *DiskSource {
	d := &DiskSource{Root: root, ProjectRoot: projectRoot, SearchCwd: true}
	if noxyPath := os.Getenv("NOXY_PATH"); noxyPath != "" {
		d.SearchPaths = filepath.SplitList(noxyPath)
	}
	return d
}

func NewSealed(root, projectRoot string) *DiskSource {
	return &DiskSource{Root: root, ProjectRoot: projectRoot}
}

func (d *DiskSource) Key() string {
	root, err := filepath.Abs(d.Root)
	if err != nil {
		root = d.Root
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	} else {
		root = filepath.Clean(root)
	}
	seal := "open"
	if !d.SearchCwd {
		seal = "sealed"
	}
	return root + "\x00" + strings.Join(d.SearchPaths, string(os.PathListSeparator)) + "\x00" + seal
}

func (d *DiskSource) candidates(suffix string) []string {
	out := make([]string, 0, 16)
	for _, searchRoot := range d.SearchPaths {
		out = append(out,
			filepath.Join(searchRoot, suffix, suffix+".nx"),
			filepath.Join(searchRoot, suffix),
			filepath.Join(searchRoot, suffix+".nx"),
		)
	}
	if d.ProjectRoot != "" {
		out = append(out,
			filepath.Join(d.ProjectRoot, "noxy_libs", suffix, suffix+".nx"),
			filepath.Join(d.ProjectRoot, "noxy_libs", suffix),
		)
	}
	out = append(out,
		filepath.Join(d.Root, "noxy_libs", suffix, suffix+".nx"),
		filepath.Join(d.Root, "noxy_libs", suffix),
		filepath.Join(d.Root, "stdlib", suffix),
		filepath.Join(d.Root, suffix),
	)
	if d.SearchCwd {
		out = append(out,
			filepath.Join("noxy_libs", suffix, suffix+".nx"),
			filepath.Join("noxy_libs", suffix),
			filepath.Join("stdlib", suffix),
			suffix,
		)
	}
	return out
}

// locate devolve o primeiro candidato existente (absoluto, limpo) e se e
// diretorio.
func (d *DiskSource) locate(suffix string) (string, bool, bool) {
	for _, candidate := range d.candidates(suffix) {
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			return "", false, false
		}
		return filepath.Clean(absolute), info.IsDir(), true
	}
	return "", false, false
}

func (d *DiskSource) Resolve(name string) (Module, error) {
	name = strings.TrimSpace(name)
	pathName := strings.ReplaceAll(name, ".", string(filepath.Separator))
	path, isDir, found := d.locate(pathName + ".nx")
	if !found || isDir {
		path, isDir, found = d.locate(pathName)
	}
	if found {
		if !isDir {
			return Module{Name: name, Kind: KindFile, Path: path}, nil
		}
		base := filepath.Base(path)
		for _, entry := range []string{base + ".nx", "main.nx"} {
			entryPath := filepath.Join(path, entry)
			if info, err := os.Stat(entryPath); err == nil && !info.IsDir() {
				return Module{Name: name, Kind: KindFile, Path: entryPath}, nil
			}
		}
		return Module{Name: name, Kind: KindDirectory, Path: path}, nil
	}
	content, err := stdlib.FS.ReadFile(strings.ReplaceAll(name, ".", "/") + ".nx")
	if err != nil {
		return Module{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return Module{Name: name, Kind: KindEmbedded, Content: string(content)}, nil
}

func (d *DiskSource) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }

func (d *DiskSource) ReadDir(path string) ([]Entry, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, Entry{Name: entry.Name(), IsDir: entry.IsDir()})
	}
	return out, nil
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/modsrc/ -count=1 && go vet ./internal/modsrc/`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/modsrc/
git commit -m "feat(modsrc): interface Source e DiskSource com a ordem de resolucao unificada"
```

---

### Task 4: compilador resolve módulos pela `Source`

**Files:**
- Modify: `internal/compiler/compiler.go` (struct `Compiler` ~linha 74; `NewWithStateAndRoot` ~186; `NewChild` ~214), `internal/compiler/generics.go` (`newPass1Compiler` ~273), `internal/compiler/module_exports.go` (`resolveModuleDeclarations` ~801, `moduleFileCandidates` ~850, `parseModuleDeclarationsFile` ~891, validator em `parseModuleDeclarations` ~932)
- Test: `internal/compiler/module_source_test.go`

**Interfaces:**
- Consumes: `modsrc.Source`, `modsrc.NewDisk`, `modsrc.Kind*`.
- Produces: `func (c *Compiler) SetModuleSource(src modsrc.Source)`; campo privado `moduleSource`; todo compilador filho (`NewChild`, `newPass1Compiler`, validador de módulo) herda a mesma `Source`.

- [ ] **Step 1: teste (falha: `SetModuleSource` inexistente)**

```go
// internal/compiler/module_source_test.go
package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
)

// memSource: modulos so na memoria, chave "/mem/<a/b>.nx". Nenhum arquivo
// no disco — prova que o compilador nao resolve `use` fora da Source.
type memSource struct{ files map[string]string }

func (m memSource) Key() string { return "mem" }

func (m memSource) Resolve(name string) (modsrc.Module, error) {
	p := "/mem/" + strings.ReplaceAll(name, ".", "/") + ".nx"
	if _, ok := m.files[p]; ok {
		return modsrc.Module{Name: name, Kind: modsrc.KindFile, Path: p}, nil
	}
	return modsrc.Module{}, modsrc.ErrNotFound
}

func (m memSource) ReadFile(p string) ([]byte, error) {
	if s, ok := m.files[p]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func (m memSource) ReadDir(string) ([]modsrc.Entry, error) { return nil, os.ErrNotExist }

func compileWithSource(t *testing.T, src modsrc.Source, program string) error {
	t.Helper()
	root := t.TempDir()
	p := parser.New(lexer.New(program))
	parsed := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root)
	c.SetModuleSource(src)
	_, _, err := c.Compile(parsed)
	return err
}

func TestCompilerDiscoversModulesThroughTheSourceOnly(t *testing.T) {
	src := memSource{files: map[string]string{
		"/mem/helper.nx":  "use util.twice select twice\nfunc quad(n: int) -> int\n    return twice(twice(n))\nend\n",
		"/mem/util/twice.nx": "func twice(n: int) -> int\n    return n * 2\nend\n",
	}}
	if err := compileWithSource(t, src, "use helper select quad\nlet x: int = quad(2)\n"); err != nil {
		t.Fatalf("modules from memory must compile: %v", err)
	}
	if err := compileWithSource(t, src, "use helper select nope\n"); err == nil {
		t.Fatal("a selector the module does not export must fail")
	}
	if err := compileWithSource(t, src, "use missing select *\n"); err == nil || !strings.Contains(err.Error(), "failed to resolve wildcard module 'missing'") {
		t.Fatalf("unknown module: %v", err)
	}
}

func TestCompilerDefaultSourceIsTheDisk(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "disk.nx"), []byte("func one() -> int\n    return 1\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := parser.New(lexer.New("use disk select one\nlet x: int = one()\n"))
	parsed := p.ParseProgram()
	c := NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root)
	if _, _, err := c.Compile(parsed); err != nil {
		t.Fatalf("without SetModuleSource the compiler reads the disk: %v", err)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/compiler/ -run 'TestCompiler(DiscoversModulesThroughTheSourceOnly|DefaultSourceIsTheDisk)' -count=1`
Expected: FAIL (c.SetModuleSource undefined).

- [ ] **Step 3: implementação**

`internal/compiler/compiler.go` — no struct `Compiler`, logo após `projectRoot`:

```go
	// moduleSource: a UNICA origem de modulos (spec 2026-09-26 §7) —
	// descoberta de exports/tipos passa por ela, nunca por os.*. Herdada por
	// NewChild, newPass1Compiler e pelo validador de modulo.
	moduleSource modsrc.Source
```

Em `NewWithStateAndRoot`, no literal `&Compiler{...}`, junto de `projectRoot: projectRoot,`:

```go
		moduleSource: modsrc.NewDisk(moduleRoot, projectRoot),
```

Em `NewChild`, no literal, junto de `moduleRoot: parent.moduleRoot,`:

```go
		moduleSource:     parent.moduleSource,
```

Novo método (ao lado de `SetKnownGlobals` em `known_globals.go` ou no fim de `compiler.go`):

```go
// SetModuleSource troca a origem de modulos deste compilador (a VM passa a
// sua; o `noxy build` e o modo aplicacao passam a deles). nil restaura o
// disco relativo a moduleRoot.
func (c *Compiler) SetModuleSource(src modsrc.Source) {
	if src == nil {
		src = modsrc.NewDisk(c.moduleRoot, c.projectRoot)
	}
	c.moduleSource = src
}
```

`internal/compiler/generics.go`, em `newPass1Compiler`, após `scratch.knownGlobals = c.knownGlobals`:

```go
	scratch.moduleSource = c.moduleSource
```

`internal/compiler/module_exports.go` — substituir `resolveModuleDeclarations`, apagar `moduleFileCandidates`, reescrever `parseModuleDeclarationsFile`, e no validador:

```go
// resolveModuleDeclarations e o corpo nao-memoizado de loadModuleDeclarations:
// resolve pelo moduleSource e delega o parse+validacao.
func (c *Compiler) resolveModuleDeclarations(module string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	m, err := c.moduleSource.Resolve(module)
	if err != nil {
		return nil, nil, false
	}
	switch m.Kind {
	case modsrc.KindEmbedded:
		return c.parseModuleDeclarations([]byte(m.Content), module, state)
	case modsrc.KindFile:
		return c.parseModuleDeclarationsFile(m.Path, state)
	case modsrc.KindDirectory:
		entries, err := c.moduleSource.ReadDir(m.Path)
		if err != nil {
			return nil, nil, false
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir {
				if _, _, loadable := c.loadModuleDeclarations(module+"."+entry.Name, state); loadable {
					names = append(names, entry.Name)
				}
				continue
			}
			if !strings.HasSuffix(entry.Name, ".nx") {
				continue
			}
			name := strings.TrimSuffix(entry.Name, ".nx")
			if _, _, loadable := c.loadModuleDeclarations(module+"."+name, state); !loadable {
				return nil, nil, false
			}
			names = append(names, name)
		}
		return nil, names, true
	}
	return nil, nil, false
}

func (c *Compiler) parseModuleDeclarationsFile(path string, state *moduleDiscoveryState) (*ast.Program, []string, bool) {
	content, err := c.moduleSource.ReadFile(path)
	if err != nil {
		return nil, nil, false
	}
	return c.parseModuleDeclarations(content, path, state)
}
```

No fim de `parseModuleDeclarations`, logo após `validator.moduleDiscovery = state`:

```go
	validator.moduleSource = c.moduleSource
```

Remova de `module_exports.go` os imports que ficarem sem uso (`os`, `path/filepath`, `stdlib`) e adicione `"github.com/estevaofon/noxy/internal/modsrc"`; em `compiler.go` adicione o import de `modsrc`.

- [ ] **Step 4: rodar e ver passar**

Run: `go build ./... && go vet ./internal/compiler/ && go test ./internal/compiler/ ./internal/vm/ -count=1`
Expected: PASS — os testes de módulos existentes (`module_exports_test.go`, `generics_modules_test.go`, `generics_modules_e2e_test.go`) continuam verdes sobre a `Source`.

- [ ] **Step 5: commit**

```bash
git add internal/compiler/
git commit -m "refactor(compiler): descoberta de modulos pela modsrc.Source, sem os.* proprio"
```

---

### Task 5: VM resolve, lê e compila módulos pela `Source`; `CompileModule` para o build

**Files:**
- Modify: `internal/vm/vm.go` (`VMConfig` ~150, `NewWithShared` ~163), `internal/vm/modules.go` (tudo exceto `loadModule`), `internal/vm/extensions.go` (`ensureExtensionLoaded` ~39, `loadWasmBackend` ~96)
- Test: `internal/vm/module_source_test.go`

**Interfaces:**
- Consumes: `modsrc.Source`, `modsrc.NewDisk`, `compiler.SetModuleSource` (Task 4).
- Produces: `VMConfig.Source modsrc.Source` (nil → `NewDisk(RootPath, ProjectRoot)` em `NewWithShared`); `func (vm *VM) CompileModule(m modsrc.Module) error` (carrega a extensão ao lado sem subir processo, compila com known globals, descarta; nada executa); internos `resolvedModule{Key moduleKey; Module modsrc.Module}`, `prepareFileModule`, `compileModule`, `runModule`.

- [ ] **Step 1: teste (falha)**

```go
// internal/vm/module_source_test.go
package vm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
	"github.com/estevaofon/noxy/internal/value"
)

type memSource struct{ files map[string]string }

func (m memSource) Key() string { return "mem" }

func (m memSource) Resolve(name string) (modsrc.Module, error) {
	p := "/mem/" + strings.ReplaceAll(name, ".", "/") + ".nx"
	if _, ok := m.files[p]; ok {
		return modsrc.Module{Name: name, Kind: modsrc.KindFile, Path: p}, nil
	}
	return modsrc.Module{}, modsrc.ErrNotFound
}

func (m memSource) ReadFile(p string) ([]byte, error) {
	if s, ok := m.files[p]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func (m memSource) ReadDir(string) ([]modsrc.Entry, error) { return nil, os.ErrNotExist }

func TestVMLoadsModulesThroughTheSourceOnly(t *testing.T) {
	root := t.TempDir() // vazio no disco
	src := memSource{files: map[string]string{
		"/mem/helper.nx": "func twice(n: int) -> int\n    return n * 2\nend\n",
	}}
	machine := NewWithConfig(VMConfig{RootPath: root, Source: src})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	p := parser.New(lexer.New("use helper\ntest_report(helper.twice(21))\n"))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filepath.Join(root, "main.nx"), root)
	c.SetModuleSource(machine.Config.Source)
	c.SetKnownGlobals(machine.GlobalNames())
	code, _, err := c.Compile(program)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if err := machine.Interpret(code); err != nil {
		t.Fatalf("run: %v", err)
	}
	if captured.Type != value.VAL_INT || captured.Int() != 42 {
		t.Fatalf("got %#v, want 42", captured)
	}
}

func TestCompileModuleReportsErrorsWithoutRunning(t *testing.T) {
	src := memSource{files: map[string]string{
		"/mem/bad.nx":  "let x: int = \"s\"\n",
		"/mem/side.nx": "print(\"must not run\")\n",
	}}
	machine := NewWithConfig(VMConfig{RootPath: t.TempDir(), Source: src})
	bad, _ := src.Resolve("bad")
	if err := machine.CompileModule(bad); err == nil {
		t.Fatal("a type error in the module must surface")
	}
	side, _ := src.Resolve("side")
	out := captureStdout(t, func() {
		if err := machine.CompileModule(side); err != nil {
			t.Errorf("side: %v", err)
		}
	})
	if strings.Contains(out, "must not run") {
		t.Fatal("CompileModule must not execute the module body")
	}
}
```

Se `captureStdout` não existir em `internal/vm`, adicione ao mesmo arquivo de teste a cópia de `cmd/noxy/main_test.go:15-27` (troca `os.Stdout` por um pipe e devolve o que foi escrito).

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/vm/ -run 'TestVMLoadsModulesThroughTheSourceOnly|TestCompileModuleReportsErrorsWithoutRunning' -count=1`
Expected: FAIL (VMConfig sem campo Source; CompileModule indefinido).

- [ ] **Step 3: implementação**

`internal/vm/vm.go`:

```go
type VMConfig struct {
	RootPath    string
	ProjectRoot string // raiz do projeto (noxy.mod mais proximo de RootPath); "" = script solto
	// Source e a origem de modulos (spec 2026-09-26 §7). nil = disco a
	// partir de RootPath/ProjectRoot; o modo aplicacao passa uma selada.
	Source modsrc.Source
}
```

Em `NewWithShared`, após o bloco que preenche `cfg.ProjectRoot` (e adicione `"github.com/estevaofon/noxy/internal/modsrc"` aos imports de `vm.go`):

```go
	if cfg.Source == nil {
		cfg.Source = modsrc.NewDisk(cfg.RootPath, cfg.ProjectRoot)
	}
```

`internal/vm/modules.go` — substituir o arquivo do `type resolvedModuleKind` até o fim, mantendo `loadModule` idêntico:

```go
type resolvedModule struct {
	Key    moduleKey
	Module modsrc.Module
}

// resolveModule: Source.Resolve + chave do cache; "module not found" ganha
// a dica de --sync como sempre.
func (vm *VM) resolveModule(name string) (resolvedModule, error) {
	canonicalName := strings.TrimSpace(name)
	m, err := vm.Config.Source.Resolve(canonicalName)
	if err != nil {
		if errors.Is(err, modsrc.ErrNotFound) {
			return resolvedModule{}, fmt.Errorf("module not found: %s%s", canonicalName, pkgmanager.SyncHint(vm.Config.ProjectRoot, canonicalName))
		}
		return resolvedModule{}, err
	}
	return resolvedModule{Key: moduleKey{Root: vm.Config.Source.Key(), Name: canonicalName}, Module: m}, nil
}

func (vm *VM) loadModule(name string) (value.Value, error) {
	// ... corpo atual, sem alteracao ...
}

func (vm *VM) loadResolvedModule(source resolvedModule) (value.Value, error) {
	switch source.Module.Kind {
	case modsrc.KindDirectory:
		return vm.loadResolvedDirectory(source)
	case modsrc.KindEmbedded:
		code, err := vm.compileModule(source.Module, source.Module.Content)
		if err != nil {
			return value.NewNull(), err
		}
		return vm.runModule(source, code)
	case modsrc.KindFile:
		content, err := vm.prepareFileModule(source.Module)
		if err != nil {
			return value.NewNull(), err
		}
		code, err := vm.compileModule(source.Module, content)
		if err != nil {
			return value.NewNull(), err
		}
		return vm.runModule(source, code)
	default:
		return value.NewNull(), fmt.Errorf("unknown resolved module kind for %s", source.Module.Name)
	}
}

// prepareFileModule: se o pacote do modulo tem noxy_ext.toml ao lado,
// carrega a extensao e registra os exports como natives ANTES de compilar o
// wrapper .nx (que referencia esses natives); depois le e valida o fonte.
func (vm *VM) prepareFileModule(m modsrc.Module) (string, error) {
	dir := filepath.Dir(m.Path)
	if _, err := vm.Config.Source.ReadFile(filepath.Join(dir, "noxy_ext.toml")); err == nil {
		if err := vm.ensureExtensionLoaded(dir); err != nil {
			return "", fmt.Errorf("failed to load extension for module %s: %w", m.Name, err)
		}
	}
	content, err := vm.Config.Source.ReadFile(m.Path)
	if err != nil {
		return "", err
	}
	text := string(content)
	if err := requireValidUTF8("module "+m.Path, text); err != nil {
		return "", err
	}
	return text, nil
}

func (vm *VM) loadResolvedDirectory(source resolvedModule) (value.Value, error) {
	entries, err := vm.Config.Source.ReadDir(source.Module.Path)
	if err != nil {
		return value.NewNull(), err
	}
	moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
	for _, entry := range entries {
		if entry.IsDir {
			submoduleName := source.Module.Name + "." + entry.Name
			submodule, loadErr := vm.loadModule(submoduleName)
			if loadErr != nil {
				var cycleErr *moduleCycleError
				if errors.As(loadErr, &cycleErr) {
					return value.NewNull(), fmt.Errorf("failed to load submodule %s: %w", submoduleName, loadErr)
				}
				continue
			}
			moduleEnvironment.SetLocal(entry.Name, submodule)
			continue
		}
		if !strings.HasSuffix(entry.Name, ".nx") {
			continue
		}
		baseName := strings.TrimSuffix(entry.Name, ".nx")
		submoduleName := source.Module.Name + "." + baseName
		submodule, loadErr := vm.loadModule(submoduleName)
		if loadErr != nil {
			return value.NewNull(), fmt.Errorf("failed to load submodule %s: %w", submoduleName, loadErr)
		}
		moduleEnvironment.SetLocal(baseName, submodule)
	}
	return moduleEnvironment.ExportMap(), nil
}

// compileModule e a metade "compilar" da carga de um modulo: parse,
// compilador com os nativos ja registrados (inclusive os da extensao
// carregada em prepareFileModule) e a mesma Source da VM.
func (vm *VM) compileModule(m modsrc.Module, content string) (*chunk.Chunk, error) {
	l := lexer.New(content)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		if m.Kind == modsrc.KindEmbedded {
			return nil, fmt.Errorf("parse error in embedded module %s: %v", m.Name, p.Errors())
		}
		return nil, fmt.Errorf("parse error in module %s: %v", m.Name, p.Errors())
	}
	compilerPath := m.Path
	if m.Kind == modsrc.KindEmbedded {
		compilerPath = m.Name
	}
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), compilerPath, vm.Config.RootPath)
	c.SetModuleSource(vm.Config.Source)
	// Issue #47 parte 3: o modulo enxerga os nativos ja registrados na raiz
	// (inclusive os da extensao carregada logo acima) e os que o proprio
	// modulo registra via sys_load_plugin.
	c.SetKnownGlobals(append(vm.GlobalNames(), compiler.PluginNativeNames(program)...))
	code, _, err := c.Compile(program)
	// Aviso do compilador de um modulo carregado em runtime e diagnostico
	// da VM: os.Stderr (AGENTS.md, regra "Saida"), nunca stdout (issue #61 item 3).
	for _, warning := range c.Warnings() {
		fmt.Fprintln(os.Stderr, warning)
	}
	return code, err
}

// runModule executa o chunk do modulo num ambiente proprio e devolve o
// ExportMap — a metade "executar", inalterada.
func (vm *VM) runModule(source resolvedModule, code *chunk.Chunk) (value.Value, error) {
	moduleEnvironment := value.NewGlobalEnvironment(vm.shared.Root)
	modFn := &value.ObjFunction{Name: source.Module.Name, Arity: 0, Chunk: code, Environment: moduleEnvironment}
	modClosure := &value.ObjClosure{Function: modFn, Upvalues: []*value.ObjUpvalue{}, Environment: moduleEnvironment}
	callerFrameCount := vm.frameCount
	vm.push(value.Value{Type: value.VAL_FUNCTION, Obj: modClosure})
	if ok, callErr := vm.callValue(vm.peek(0), 0, nil, 0); !ok {
		return value.NewNull(), callErr
	}
	startFrameCount := vm.frameCount
	if err := vm.run(startFrameCount, nil); err != nil {
		return value.NewNull(), err
	}
	if callerFrameCount > 0 {
		vm.pop()
	}
	return moduleEnvironment.ExportMap(), nil
}

// CompileModule e a checagem de compilacao do `noxy build` (spec §6.3):
// carrega a extensao ao lado do modulo (sem subir processo), compila com os
// nativos conhecidos e descarta o chunk. Nada executa.
func (vm *VM) CompileModule(m modsrc.Module) error {
	content := m.Content
	if m.Kind == modsrc.KindFile {
		var err error
		if content, err = vm.prepareFileModule(m); err != nil {
			return err
		}
	}
	_, err := vm.compileModule(m, content)
	return err
}
```

Imports de `modules.go`: remover `"os"` e `".../internal/stdlib"`; adicionar `"github.com/estevaofon/noxy/internal/chunk"` e `"github.com/estevaofon/noxy/internal/modsrc"` (os demais ficam).

`internal/vm/extensions.go`: em `ensureExtensionLoaded`, `manifestData, err := os.ReadFile(filepath.Join(dir, "noxy_ext.toml"))` vira `vm.Config.Source.ReadFile(...)`; em `loadWasmBackend`, `os.ReadFile(filepath.Join(dir, manifest.Wasm))` vira `vm.Config.Source.ReadFile(...)`. `loadProcessBackend` continua com `os.ReadFile(binPath)` — o binário de processo é sempre um arquivo real (spec §7.3). Confira `vm.SetModule` (`vm.go`): usa `source.Key`, que continua existindo.

- [ ] **Step 4: rodar e ver passar**

Run: `go build ./... && go vet ./... && go test ./internal/... -count=1 && go test ./cmd/... -count=1`
Expected: PASS — inclusive `architecture_test.go` (`loadModule` segue em `modules.go`), `process_extensions_e2e_test.go`, `extensions_e2e_test.go`, `module_cache_test.go`.

- [ ] **Step 5: commit**

```bash
git add internal/vm/
git commit -m "refactor(vm): modulos pela modsrc.Source; CompileModule separa compilar de executar"
```

---

### Task 6: diretiva `include` no `noxy.mod`

**Files:**
- Modify: `internal/pkgmanager/modfile.go`
- Test: `internal/pkgmanager/modfile_test.go`

**Interfaces:**
- Produces: `ModuleConfig.Include []string` (ordem do arquivo, sem duplicata, caminhos limpos); `func ValidateIncludePath(p string) (string, error)` (devolve o caminho limpo com `/`; erros `include path is empty`, `include "<p>": use forward slashes`, `include "<p>" is outside the project root`); `Save` grava `include <p>` após os `require`, em ordem lexicográfica.

- [ ] **Step 1: testes (falham)**

```go
// acrescentar a internal/pkgmanager/modfile_test.go
func TestModFileIncludeIsParsedAndPreservedBySave(t *testing.T) {
	content := "module app\n\nnoxy v0.26.0\n\nrequire github.com/user/repo v1.0.0\n\ninclude web\ninclude ./assets/\ninclude web\n"
	path := filepath.Join(t.TempDir(), "noxy.mod")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ParseModFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(cfg.Include, ",") != "web,assets" {
		t.Fatalf("Include = %v", cfg.Include)
	}
	saved := filepath.Join(t.TempDir(), "noxy.mod")
	if err := cfg.Save(saved); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(saved)
	if !strings.HasSuffix(string(data), "require github.com/user/repo v1.0.0\n\ninclude assets\ninclude web\n") {
		t.Fatalf("Save must keep the includes, sorted, after the requires:\n%s", data)
	}
	again, err := ParseModFile(saved)
	if err != nil || strings.Join(again.Include, ",") != "assets,web" {
		t.Fatalf("round trip: %v %v", again.Include, err)
	}
}

func TestModFileIncludeRejectsEscapesAndAbsolutePaths(t *testing.T) {
	for _, line := range []string{"include ../x", "include /abs", "include C:/abs", "include a\\b", "include"} {
		path := filepath.Join(t.TempDir(), "noxy.mod")
		if err := os.WriteFile(path, []byte("module app\n"+line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ParseModFile(path); err == nil || !strings.Contains(err.Error(), "noxy.mod:2") {
			t.Errorf("%q: want a noxy.mod:2 error, got %v", line, err)
		}
	}
	if _, err := ValidateIncludePath("a/../../b"); err == nil || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("escape: %v", err)
	}
	if clean, err := ValidateIncludePath("./web/vendor/"); err != nil || clean != "web/vendor" {
		t.Fatalf("clean: %q %v", clean, err)
	}
}

func TestModFileWithoutIncludeSavesNoIncludeLine(t *testing.T) {
	cfg := NewModuleConfig()
	cfg.Module = "app"
	path := filepath.Join(t.TempDir(), "noxy.mod")
	if err := cfg.Save(path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "include") {
		t.Fatalf("no include expected:\n%s", data)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/pkgmanager/ -run 'TestModFileInclude|TestModFileWithoutInclude' -count=1`
Expected: FAIL (`cfg.Include` e `ValidateIncludePath` indefinidos).

- [ ] **Step 3: implementação**

```go
// internal/pkgmanager/modfile.go — trechos novos

type ModuleConfig struct {
	Module      string
	NoxyVersion string
	Require     map[string]string // modulo → versao normalizada ou HEAD
	// Include: diretiva `include <caminho>` (spec 2026-09-26 §3.2) — assets
	// que `noxy build` embute, relativos ao diretorio do noxy.mod, com "/".
	// --sync e --get nao a usam, mas Save a preserva.
	Include []string
}

// ValidateIncludePath aceita so caminho relativo com "/" que fique sob a
// raiz do projeto, e devolve-o limpo ("./web/" → "web").
func ValidateIncludePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", errors.New("include path is empty")
	}
	if strings.Contains(p, "\\") {
		return "", fmt.Errorf("include %q: use forward slashes", p)
	}
	if strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':') {
		return "", fmt.Errorf("include %q is outside the project root", p)
	}
	clean := path.Clean(p)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("include %q is outside the project root", p)
	}
	return clean, nil
}
```

Em `ParseModFile`, novo `case` no `switch parts[0]`:

```go
		case "include":
			if len(parts) < 2 {
				return nil, fmt.Errorf("noxy.mod:%d: include <path>", i+1)
			}
			clean, err := ValidateIncludePath(parts[1])
			if err != nil {
				return nil, fmt.Errorf("noxy.mod:%d: %w", i+1, err)
			}
			if !slices.Contains(config.Include, clean) {
				config.Include = append(config.Include, clean)
			}
```

Em `Save`, antes do `return os.WriteFile(...)`:

```go
	if len(c.Include) > 0 {
		includes := append([]string(nil), c.Include...)
		sort.Strings(includes)
		sb.WriteString("\n")
		for _, include := range includes {
			fmt.Fprintf(&sb, "include %s\n", include)
		}
	}
```

Imports novos em `modfile.go`: `"errors"`, `"path"`, `"slices"`.

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/pkgmanager/ -count=1 && go vet ./internal/pkgmanager/`
Expected: PASS (inclusive `sync_test.go`/`manager_get_test.go`, que passam por `Save`).

- [ ] **Step 5: commit**

```bash
git add internal/pkgmanager/modfile.go internal/pkgmanager/modfile_test.go
git commit -m "feat(pkgmanager): diretiva include no noxy.mod, preservada por Save"
```

---

### Task 7: `sys.executable()`

**Files:**
- Modify: `internal/vm/builtins_sys.go` (dentro de `defineSystemBuiltins`, após `sys_getcwd`), `internal/stdlib/sys.nx` (após `argv`), `internal/vm/builtins_registry_test.go` (snapshot), `docs/NOXY_LANGUAGE_SPEC.md` (§12, `sys`)
- Test: `internal/vm/builtins_sys_executable_test.go`

**Interfaces:**
- Produces: nativo `sys_executable() -> string`; wrapper `sys.executable() -> string`.

- [ ] **Step 1: teste (falha)**

```go
// internal/vm/builtins_sys_executable_test.go
package vm

import (
	"os"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func TestSysExecutableIsTheRunningBinary(t *testing.T) {
	want, err := os.Executable()
	if err != nil {
		t.Skip("os.Executable unavailable here")
	}
	got := captureVMSourceAtRoot(t, t.TempDir(), "use sys\ntest_report(sys.executable())\n")
	if got.Type != value.VAL_STRING || got.String() != want {
		t.Fatalf("sys.executable() = %#v, want %q", got, want)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/vm/ -run TestSysExecutableIsTheRunningBinary -count=1`
Expected: FAIL (compiler error: `executable` não existe em `sys`).

- [ ] **Step 3: implementação**

`internal/vm/builtins_sys.go`, após o `sys_getcwd`:

```go
	// sys_executable: o binario que roda este programa — o noxy, ou o
	// proprio app gerado por `noxy build` (spec 2026-09-26 §3.4). Cru, sem
	// EvalSymlinks; "" em falha, como sys_getcwd.
	vm.DefineNative("sys_executable", func(args []value.Value) value.Value {
		exe, err := os.Executable()
		if err != nil {
			return value.NewString("")
		}
		return value.NewString(exe)
	})
```

`internal/stdlib/sys.nx`, após `argv`:

```noxy
// Caminho do executavel que roda este programa: o noxy, ou o proprio app
// gerado por `noxy build`. Para rodar outro arquivo Noxy com o mesmo
// interpretador: NOXY_INTERPRETER=1 <executable()> arquivo.nx, com a
// variavel no ambiente do FILHO (ela e herdada). "" se o sistema nao souber.
func executable() -> string
    return sys_executable()
end
```

`internal/vm/builtins_registry_test.go`: inserir `"sys_executable"` entre `"sys_exec_output_bytes"` e `"sys_exit"` (ordem lexicográfica: `_` < `u`, `e` < `i`).

`docs/NOXY_LANGUAGE_SPEC.md`, §12 `sys`, após o parágrafo de `sys.version`:

```markdown
`sys.executable() -> string` is the path of the binary running the program:
the `noxy` interpreter, or the executable produced by `noxy build`
(`docs/BUILD.md`). To run another Noxy file with the same interpreter, spawn
`<executable> file.nx` with `NOXY_INTERPRETER=1` in the **child's**
environment — the variable is inherited, and it makes a built executable
ignore its embedded program and behave as plain `noxy`. `""` when the OS
cannot tell.
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/vm/ -run 'TestSysExecutable|TestBuiltinRegistry|TestBuiltinSourceLayout' -count=1 && go test ./internal/compiler/ -count=1`
Expected: PASS (snapshot atualizado; `known_globals_test.go` continua verde).

- [ ] **Step 5: commit**

```bash
git add internal/vm/builtins_sys.go internal/vm/builtins_sys_executable_test.go internal/vm/builtins_registry_test.go internal/stdlib/sys.nx docs/NOXY_LANGUAGE_SPEC.md
git commit -m "feat(sys): sys.executable() devolve o binario que roda o programa"
```

---

### Task 8: `internal/build` — plano: grafo de `use`, checagem de compilação, extensões, includes, `--list`

**Files:**
- Create: `internal/build/plan.go`, `internal/build/list.go`
- Test: `internal/build/plan_test.go`

**Interfaces:**
- Consumes: `modsrc` (Task 3), `compiler.SetModuleSource` (Task 4), `vm.VMConfig.Source`/`vm.CompileModule` (Task 5), `pkgmanager.ValidateIncludePath`/`ModuleConfig.Include` (Task 6), `bundle.ValidPath`/`Manifest`/`ManifestName`/`MarkerName` (Task 2), `ext.ParseManifest`/`Manifest.BinaryFor`/`PublishedPlatforms`/`KindProcess`, `compiler.PluginNativeNames`, `ast.Inspect`.
- Produces: `type Options struct { Entry, Output string; Includes []string; Runtime string; Out, Diag io.Writer }`; `type Module struct { Name, Path string; Dir bool }`; `type Extension struct { Name, Kind, Module, Dir, Artifact, SHA256 string; Size int64 }`; `type File struct { Path string; Size int64; Executable bool; Abs string }`; `type Plan struct { Root, Entry, Target, Runtime string; Modules []Module; Extensions []Extension; Includes []string; Files []File }`; `func MakePlan(opts Options) (*Plan, error)`; `func (p *Plan) Manifest() *bundle.Manifest`; `func (p *Plan) IncludedFiles() []File`; `func (p *Plan) List(w io.Writer) error`; `func HumanSize(n int64) string`.

- [ ] **Step 1: testes (falham)**

```go
// internal/build/plan_test.go
package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/ext/exttest"
)

// writeProject grava os arquivos (chaves com "/") num diretorio novo.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// FindRoot resolve symlinks (macOS: /var → /private/var); alinhe o root.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	return root
}

func planOf(t *testing.T, root string, entry string, includes ...string) (*Plan, error) {
	t.Helper()
	return MakePlan(Options{Entry: filepath.Join(root, filepath.FromSlash(entry)), Includes: includes, Runtime: os.Args[0], Diag: &bytes.Buffer{}})
}

func names(mods []Module) string {
	out := make([]string, len(mods))
	for i, m := range mods {
		out[i] = m.Name
		if m.Dir {
			out[i] += "/"
		}
	}
	return strings.Join(out, " ")
}

func paths(files []File) string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.Path
	}
	return strings.Join(out, " ")
}

func TestPlanWalksTransitiveUsesAndDirectoryModules(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":         "module app\n",
		"main.nx":          "use src.a as a\nprint(a.quad(2))\n",
		"src/a.nx":         "use src.b select helper\nuse lib as l\nfunc quad(n: int) -> int\n    return helper(helper(n))\nend\n",
		"src/b.nx":         "func helper(n: int) -> int\n    return n * 2\nend\n",
		"lib/x.nx":         "func one() -> int\n    return 1\nend\n",
		"lib/README.md":    "not a module\n",
		"lib/sub/y.nx":     "let y: int = 2\n",
		"lib/assets/f.txt": "not a module either\n",
	})
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Root != root || plan.Entry != "main.nx" || plan.Target != runtime.GOOS+"/"+runtime.GOARCH {
		t.Fatalf("plan %+v", plan)
	}
	if got := names(plan.Modules); got != "src.a src.b lib/ lib.assets/ lib.sub/ lib.sub.y lib.x" {
		t.Fatalf("modules %q", got)
	}
	if got := paths(plan.Files); got != "lib/sub/y.nx lib/x.nx main.nx noxy.mod src/a.nx src/b.nx" {
		t.Fatalf("files %q", got)
	}
}

func TestPlanSkipsEmbeddedStdlibButKeepsLocalShadow(t *testing.T) {
	root := writeProject(t, map[string]string{
		"main.nx":    "use io\nuse strings select shadow\nprint(shadow())\n",
		"strings.nx": "func shadow() -> int\n    return 1\nend\n",
	})
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(plan.Files); got != "main.nx strings.nx" {
		t.Fatalf("files %q (io is embedded, strings.nx shadows the stdlib)", got)
	}
}

func TestPlanReportsCompileErrorWithTheModulePath(t *testing.T) {
	root := writeProject(t, map[string]string{
		"main.nx":    "use src.bad as bad\nprint(1)\n",
		"src/bad.nx": "let x: int = \"not an int\"\n",
	})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.HasPrefix(err.Error(), "src/bad.nx: ") {
		t.Fatalf("want the module path as prefix, got %v", err)
	}
}

func TestPlanRejectsModuleOutsideTheRoot(t *testing.T) {
	far := t.TempDir()
	if err := os.MkdirAll(filepath.Join(far, "far"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(far, "far", "far.nx"), []byte("let f: int = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOXY_PATH", far)
	root := writeProject(t, map[string]string{"main.nx": "use far\nprint(far.f)\n"})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "module far resolves to") || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanMissingModuleNamesTheUseSite(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "print(1)\nuse nope\n"})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "main.nx:2: module not found: nope") {
		t.Fatalf("got %v", err)
	}
}

func TestPlanIncludesUnionWithoutDuplicates(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":         "module app\n\ninclude web\n",
		"main.nx":          "print(1)\n",
		"web/index.html":   "<html></html>\n",
		"web/vendor/x.js":  "1\n",
		"docs/readme.txt":  "r\n",
	})
	plan, err := planOf(t, root, "main.nx", "./web/", "web/vendor/x.js", "docs/readme.txt")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(plan.Includes, " ") != "docs/readme.txt web web/vendor/x.js" {
		t.Fatalf("includes %v", plan.Includes)
	}
	if got := paths(plan.Files); got != "docs/readme.txt main.nx noxy.mod web/index.html web/vendor/x.js" {
		t.Fatalf("files %q", got)
	}
	if got := paths(plan.IncludedFiles()); got != "docs/readme.txt web/index.html web/vendor/x.js" {
		t.Fatalf("included %q", got)
	}
	if _, err := planOf(t, root, "main.nx", "assets"); err == nil || !strings.Contains(err.Error(), "include assets not found") {
		t.Fatalf("missing include: %v", err)
	}
	if _, err := planOf(t, root, "main.nx", "../x"); err == nil || !strings.Contains(err.Error(), "outside the project root") {
		t.Fatalf("escaping include: %v", err)
	}
}

func TestPlanEntryInASubdirectoryUsesTheProjectRoot(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":                  "module app\n",
		"noxy.sum":                  "",
		"examples/app.nx":           "use math\nprint(math.add(1, 2))\n",
		"noxy_libs/math/math.nx":    "func add(a: int, b: int) -> int\n    return a + b\nend\n",
	})
	plan, err := planOf(t, root, "examples/app.nx")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Entry != "examples/app.nx" || plan.Root != root {
		t.Fatalf("entry %q root %q", plan.Entry, plan.Root)
	}
	if got := paths(plan.Files); got != "examples/app.nx noxy.mod noxy.sum noxy_libs/math/math.nx" {
		t.Fatalf("files %q", got)
	}
}

func TestPlanRefusesSysLoadPlugin(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "sys_load_plugin(\"old\", \"./old\")\n"})
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "sys_load_plugin is not supported in built executables (removed in v0.27.0)") {
		t.Fatalf("got %v", err)
	}
}

const testExtManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
%s = "%s"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"
`

// writeGuestExtension instala noxy_libs/guest com o guest do SDK em bin/<asset>.
func writeGuestExtension(t *testing.T, root string, platform string) string {
	t.Helper()
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(testExtManifest, platform, asset)
	if err := os.WriteFile(filepath.Join(pkg, "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	wrapper := "func add(a: int, b: int) -> int\n    return guest_add(a, b)\nend\n"
	if err := os.WriteFile(filepath.Join(pkg, "guest.nx"), []byte(wrapper), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "bin", asset), bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return asset
}

func TestPlanRecordsTheProcessExtensionOfThisPlatform(t *testing.T) {
	root := writeProject(t, map[string]string{"noxy.mod": "module app\n", "main.nx": "use guest as g\nprint(g.add(2, 3))\n"})
	asset := writeGuestExtension(t, root, runtime.GOOS+"-"+runtime.GOARCH)
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Extensions) != 1 {
		t.Fatalf("extensions %+v", plan.Extensions)
	}
	e := plan.Extensions[0]
	bin, _ := os.ReadFile(filepath.Join(root, "noxy_libs", "guest", "bin", asset))
	sum := sha256.Sum256(bin)
	if e.Name != "guest" || e.Kind != "process" || e.Module != "guest" || e.Dir != "noxy_libs/guest" || e.Artifact != "bin/"+asset || e.SHA256 != hex.EncodeToString(sum[:]) || e.Size != int64(len(bin)) {
		t.Fatalf("extension %+v", e)
	}
	if got := paths(plan.Files); got != "main.nx noxy.mod noxy_libs/guest/bin/"+asset+" noxy_libs/guest/guest.nx noxy_libs/guest/noxy_ext.toml" {
		t.Fatalf("files %q", got)
	}
	for _, f := range plan.Files {
		if strings.HasPrefix(f.Path, "noxy_libs/guest/bin/") && !f.Executable {
			t.Fatalf("%s must be executable", f.Path)
		}
	}
	m := plan.Manifest()
	if m.Extensions[0].SHA256 != e.SHA256 || m.Entry != "main.nx" || m.Format != 1 || m.Kind != "source" {
		t.Fatalf("manifest %+v", m)
	}

	if err := os.RemoveAll(filepath.Join(root, "noxy_libs", "guest", "bin")); err != nil {
		t.Fatal(err)
	}
	_, err = planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "binary bin/"+asset+" not found — run 'noxy --sync' to download it") {
		t.Fatalf("missing binary: %v", err)
	}
}

func TestPlanRejectsAnExtensionWithoutThisPlatform(t *testing.T) {
	root := writeProject(t, map[string]string{"noxy.mod": "module app\n", "main.nx": "use guest as g\nprint(g.add(2, 3))\n"})
	writeGuestExtension(t, root, "plan9-mips")
	_, err := planOf(t, root, "main.nx")
	if err == nil || !strings.Contains(err.Error(), "has no binary for "+runtime.GOOS+"/"+runtime.GOARCH) || !strings.Contains(err.Error(), "published: plan9/mips") {
		t.Fatalf("got %v", err)
	}
}

func TestListPrintsModulesExtensionsAndIncludes(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":       "module app\n\ninclude web\n",
		"main.nx":        "use src.a as a\nuse guest as g\nprint(a.one() + g.add(1, 1))\n",
		"src/a.nx":       "func one() -> int\n    return 1\nend\n",
		"web/index.html": "<html></html>\n",
	})
	asset := writeGuestExtension(t, root, runtime.GOOS+"-"+runtime.GOARCH)
	plan, err := planOf(t, root, "main.nx")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := plan.List(&out); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"entry      main.nx", "target     " + runtime.GOOS + "/" + runtime.GOARCH, "modules    2", "  src/a.nx", "  noxy_libs/guest/guest.nx", "extensions 1", "  guest  process  " + runtime.GOOS + "/" + runtime.GOARCH + "  noxy_libs/guest/bin/" + asset, "includes   1 files", "  web/index.html"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
	if HumanSize(1536) != "1.5 KB" || HumanSize(23826816) != "22.7 MB" || HumanSize(12) != "12 B" {
		t.Fatalf("HumanSize: %s %s %s", HumanSize(1536), HumanSize(23826816), HumanSize(12))
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/build/ -count=1`
Expected: FAIL (pacote sem fonte).

- [ ] **Step 3: implementação**

```go
// internal/build/plan.go
// Package build monta o payload do `noxy build` (spec 2026-09-26 §6): caminha
// o grafo de `use` com a MESMA resolucao de `noxy entry.nx`, compila cada
// modulo no build (sem executar) e recolhe extensoes da plataforma e assets.
package build

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/estevaofon/noxy/internal/ast"
	"github.com/estevaofon/noxy/internal/bundle"
	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/ext"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/parser"
	"github.com/estevaofon/noxy/internal/pkgmanager"
	"github.com/estevaofon/noxy/internal/version"
	"github.com/estevaofon/noxy/internal/vm"
)

type Options struct {
	Entry    string    // entry.nx (qualquer forma; vira absoluto)
	Output   string    // -o; "" = nome do entry sem .nx, no cwd
	Includes []string  // --include, relativos a raiz do projeto
	Runtime  string    // binario a copiar (o noxy que esta rodando)
	Out      io.Writer // progresso (stdout); nil = io.Discard
	Diag     io.Writer // avisos (stderr); nil = os.Stderr
}

type Module struct {
	Name string
	Path string // relativo a raiz, "/"
	Dir  bool
}

type Extension struct {
	Name, Kind, Module, Dir, Artifact, SHA256 string
	Size                                      int64
}

type File struct {
	Path       string // relativo a raiz, "/"
	Size       int64
	Executable bool
	Abs        string
}

type Plan struct {
	Root       string // raiz do projeto (dir do noxy.mod, ou do entry)
	Entry      string // relativo a raiz
	Target     string // "goos/goarch" (v1: o host)
	Runtime    string
	Modules    []Module
	Extensions []Extension
	Includes   []string
	Files      []File
}

var errSysLoadPlugin = errors.New("sys_load_plugin is not supported in built executables (removed in v0.27.0)")

type walker struct {
	plan  *Plan
	src   modsrc.Source
	vm    *vm.VM
	seen  map[string]bool
	files map[string]File
}

// MakePlan resolve a raiz, compila o entry, caminha e compila os modulos,
// recolhe extensoes e includes. Nao escreve nada.
func MakePlan(opts Options) (*Plan, error) {
	diag := opts.Diag
	if diag == nil {
		diag = os.Stderr
	}
	entryAbs, err := filepath.Abs(opts.Entry)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(entryAbs); err != nil || info.IsDir() {
		return nil, fmt.Errorf("entry file not found: %s", opts.Entry)
	}
	if resolved, err := filepath.EvalSymlinks(entryAbs); err == nil {
		entryAbs = resolved
	}
	entryDir := filepath.Dir(entryAbs)
	projectRoot, hasMod := pkgmanager.FindRoot(entryDir)
	root := entryDir
	if hasMod {
		root = projectRoot
	} else {
		projectRoot = ""
	}
	p := &Plan{Root: root, Target: runtime.GOOS + "/" + runtime.GOARCH, Runtime: opts.Runtime}
	entryRel, err := relUnder(root, entryAbs)
	if err != nil {
		return nil, fmt.Errorf("entry %s is outside the project root %s", opts.Entry, root)
	}
	p.Entry = entryRel

	machine := vm.NewWithConfig(vm.VMConfig{RootPath: entryDir, ProjectRoot: projectRoot})
	defer machine.CloseExtensions()
	w := &walker{plan: p, src: machine.Config.Source, vm: machine, seen: map[string]bool{}, files: map[string]File{}}

	content, err := os.ReadFile(entryAbs)
	if err != nil {
		return nil, err
	}
	program, err := parseSource(entryRel, string(content))
	if err != nil {
		return nil, err
	}
	if len(compiler.PluginNativeNames(program)) != 0 {
		return nil, errSysLoadPlugin
	}
	if err := w.addFile(entryRel, entryAbs, false); err != nil {
		return nil, err
	}
	// O entry compila como em runWithConfig: so os nativos da VM sao
	// conhecidos (as extensoes so entram quando os modulos carregam).
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), entryAbs, entryDir)
	c.SetModuleSource(machine.Config.Source)
	c.SetKnownGlobals(machine.GlobalNames())
	_, _, entryErr := c.Compile(program)
	for _, warning := range c.Warnings() {
		fmt.Fprintln(diag, warning)
	}
	// Um erro num modulo e mais especifico que o do entry (que pode ser so
	// "failed to resolve wildcard module"): a caminhada roda mesmo assim e
	// tem prioridade.
	if err := w.visitUses(program, entryRel); err != nil {
		return nil, err
	}
	if entryErr != nil {
		return nil, fmt.Errorf("%s: %v", entryRel, entryErr)
	}
	for _, name := range []string{"noxy.mod", "noxy.sum"} {
		abs := filepath.Join(root, name)
		if info, err := os.Stat(abs); err == nil && !info.IsDir() {
			if err := w.addFile(name, abs, false); err != nil {
				return nil, err
			}
		}
	}
	var modIncludes []string
	if hasMod {
		cfg, err := pkgmanager.ParseModFile(filepath.Join(root, "noxy.mod"))
		if err != nil {
			return nil, err
		}
		modIncludes = cfg.Include
	}
	if err := w.addIncludes(modIncludes, opts.Includes); err != nil {
		return nil, err
	}
	p.Files = make([]File, 0, len(w.files))
	for _, f := range w.files {
		p.Files = append(p.Files, f)
	}
	sort.Slice(p.Files, func(i, j int) bool { return p.Files[i].Path < p.Files[j].Path })
	return p, nil
}

func parseSource(rel, content string) (*ast.Program, error) {
	p := parser.New(lexer.New(content))
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		return nil, fmt.Errorf("%s: %s", rel, strings.Join(p.Errors(), "; "))
	}
	return program, nil
}

// relUnder: caminho relativo a root, "/"; lexical primeiro (um package
// linkado por symlink DENTRO de noxy_libs continua valendo), com
// EvalSymlinks como segunda chance (macOS /var → /private/var).
func relUnder(root, abs string) (string, error) {
	try := func(p string) (string, bool) {
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}
	if rel, ok := try(abs); ok {
		return rel, nil
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		if rel, ok := try(resolved); ok {
			return rel, nil
		}
	}
	return "", fmt.Errorf("%s is outside %s", abs, root)
}

// visitUses percorre TODO UseStmt do programa (inclusive dentro de corpos
// de funcao), na ordem do fonte.
func (w *walker) visitUses(program *ast.Program, from string) error {
	var walkErr error
	ast.Inspect(program, func(node ast.Node) bool {
		if walkErr != nil {
			return false
		}
		use, ok := node.(*ast.UseStmt)
		if !ok {
			return true
		}
		walkErr = w.visitModule(use.Module, from, use.Token.Line)
		return walkErr == nil
	})
	return walkErr
}

func (w *walker) visitModule(name, from string, line int) error {
	name = strings.TrimSpace(name)
	if w.seen[name] {
		return nil
	}
	w.seen[name] = true
	m, err := w.src.Resolve(name)
	if err != nil {
		if errors.Is(err, modsrc.ErrNotFound) {
			return fmt.Errorf("%s:%d: module not found: %s%s", from, line, name, pkgmanager.SyncHint(w.vm.Config.ProjectRoot, name))
		}
		return err
	}
	switch m.Kind {
	case modsrc.KindEmbedded:
		return nil
	case modsrc.KindFile:
		rel, err := relUnder(w.plan.Root, m.Path)
		if err != nil {
			return fmt.Errorf("module %s resolves to %s, outside the project root %s", name, m.Path, w.plan.Root)
		}
		w.plan.Modules = append(w.plan.Modules, Module{Name: name, Path: rel})
		if err := w.addFile(rel, m.Path, false); err != nil {
			return err
		}
		if err := w.visitExtension(m, rel); err != nil {
			return err
		}
		if err := w.vm.CompileModule(m); err != nil {
			return fmt.Errorf("%s: %v", rel, err)
		}
		content, err := w.src.ReadFile(m.Path)
		if err != nil {
			return err
		}
		program, err := parseSource(rel, string(content))
		if err != nil {
			return err
		}
		if len(compiler.PluginNativeNames(program)) != 0 {
			return errSysLoadPlugin
		}
		return w.visitUses(program, rel)
	case modsrc.KindDirectory:
		rel, err := relUnder(w.plan.Root, m.Path)
		if err != nil {
			return fmt.Errorf("module %s resolves to %s, outside the project root %s", name, m.Path, w.plan.Root)
		}
		w.plan.Modules = append(w.plan.Modules, Module{Name: name, Path: rel, Dir: true})
		entries, err := w.src.ReadDir(m.Path)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.IsDir {
				// A VM ignora um subdiretorio que nao resolve (loadResolvedDirectory).
				if _, err := w.src.Resolve(name + "." + entry.Name); err != nil {
					continue
				}
				if err := w.visitModule(name+"."+entry.Name, rel, 0); err != nil {
					return err
				}
				continue
			}
			if !strings.HasSuffix(entry.Name, ".nx") {
				continue
			}
			if err := w.visitModule(name+"."+strings.TrimSuffix(entry.Name, ".nx"), rel, 0); err != nil {
				return err
			}
		}
		return nil
	}
	return nil
}

// visitExtension: noxy_ext.toml ao lado do modulo → manifesto + artefato da
// plataforma no payload (spec §6.4).
func (w *walker) visitExtension(m modsrc.Module, rel string) error {
	dir := filepath.Dir(m.Path)
	manifestPath := filepath.Join(dir, "noxy_ext.toml")
	manifestData, err := w.src.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	manifest, err := ext.ParseManifest(manifestData)
	if err != nil {
		return err
	}
	relDir := path.Dir(rel)
	if err := w.addFile(relDir+"/noxy_ext.toml", manifestPath, false); err != nil {
		return err
	}
	artifact := manifest.Wasm
	executable := false
	if manifest.Kind == ext.KindProcess {
		goos, goarch, _ := strings.Cut(w.plan.Target, "/")
		asset, ok := manifest.BinaryFor(goos, goarch)
		if !ok {
			return fmt.Errorf("extension %q has no binary for %s (published: %s)", manifest.Name, w.plan.Target, strings.Join(manifest.PublishedPlatforms(), ", "))
		}
		artifact = "bin/" + asset
		executable = true
	}
	abs := filepath.Join(dir, filepath.FromSlash(artifact))
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) && manifest.Kind == ext.KindProcess {
			return fmt.Errorf("extension %q: binary %s not found — run 'noxy --sync' to download it", manifest.Name, artifact)
		}
		return fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	sum := sha256.Sum256(data)
	w.plan.Extensions = append(w.plan.Extensions, Extension{Name: manifest.Name, Kind: manifest.Kind, Module: m.Name, Dir: relDir, Artifact: artifact, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	return w.addFile(relDir+"/"+artifact, abs, executable)
}

func (w *walker) addFile(rel, abs string, executable bool) error {
	rel = path.Clean(filepath.ToSlash(rel))
	if !bundle.ValidPath(rel) {
		return fmt.Errorf("invalid payload path %q", rel)
	}
	if rel == bundle.ManifestName || rel == bundle.MarkerName {
		return fmt.Errorf("%s is a reserved name in the project root", rel)
	}
	if _, dup := w.files[rel]; dup {
		return nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if runtime.GOOS != "windows" && info.Mode()&0o111 != 0 {
		executable = true
	}
	w.files[rel] = File{Path: rel, Size: info.Size(), Executable: executable, Abs: abs}
	return nil
}

// addIncludes: uniao de `include` do noxy.mod e --include, sem duplicata,
// validados; diretorio entra recursivamente (arquivos regulares).
func (w *walker) addIncludes(modIncludes, flagIncludes []string) error {
	seen := map[string]bool{}
	for _, raw := range append(append([]string{}, modIncludes...), flagIncludes...) {
		clean, err := pkgmanager.ValidateIncludePath(raw)
		if err != nil {
			return err
		}
		if seen[clean] {
			continue
		}
		seen[clean] = true
		w.plan.Includes = append(w.plan.Includes, clean)
		abs := filepath.Join(w.plan.Root, filepath.FromSlash(clean))
		info, err := os.Stat(abs)
		if err != nil {
			return fmt.Errorf("include %s not found", clean)
		}
		if !info.IsDir() {
			if err := w.addFile(clean, abs, false); err != nil {
				return err
			}
			continue
		}
		err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(w.plan.Root, p)
			if err != nil {
				return err
			}
			return w.addFile(filepath.ToSlash(rel), p, false)
		})
		if err != nil {
			return err
		}
	}
	sort.Strings(w.plan.Includes)
	return nil
}

// IncludedFiles: os arquivos do payload que vieram dos includes.
func (p *Plan) IncludedFiles() []File {
	out := make([]File, 0)
	for _, f := range p.Files {
		for _, include := range p.Includes {
			if f.Path == include || strings.HasPrefix(f.Path, include+"/") {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

func (p *Plan) Manifest() *bundle.Manifest {
	m := &bundle.Manifest{Format: bundle.FormatVersion, Kind: bundle.KindSource, Noxy: version.Version, Target: p.Target, Entry: p.Entry,
		Modules: make([]bundle.ModuleEntry, 0, len(p.Modules)), Extensions: make([]bundle.ExtensionEntry, 0, len(p.Extensions)), Includes: append([]string{}, p.Includes...)}
	for _, mod := range p.Modules {
		m.Modules = append(m.Modules, bundle.ModuleEntry{Name: mod.Name, Path: mod.Path, Dir: mod.Dir})
	}
	for _, e := range p.Extensions {
		m.Extensions = append(m.Extensions, bundle.ExtensionEntry{Name: e.Name, Kind: e.Kind, Module: e.Module, Dir: e.Dir, Artifact: e.Artifact, SHA256: e.SHA256})
	}
	return m
}
```

```go
// internal/build/list.go
package build

import (
	"fmt"
	"io"
	"os"
)

// List imprime o plano (o que --list mostra, spec §3.1).
func (p *Plan) List(w io.Writer) error {
	runtimeSize := "?"
	if info, err := os.Stat(p.Runtime); err == nil {
		runtimeSize = HumanSize(info.Size())
	}
	fmt.Fprintf(w, "entry      %s\n", p.Entry)
	fmt.Fprintf(w, "root       %s\n", p.Root)
	fmt.Fprintf(w, "target     %s\n", p.Target)
	fmt.Fprintf(w, "runtime    %s (%s)\n", p.Runtime, runtimeSize)
	fmt.Fprintf(w, "modules    %d\n", len(p.Modules))
	for _, m := range p.Modules {
		if m.Dir {
			fmt.Fprintf(w, "  %s (dir)\n", m.Path)
			continue
		}
		fmt.Fprintf(w, "  %s\n", m.Path)
	}
	fmt.Fprintf(w, "extensions %d\n", len(p.Extensions))
	for _, e := range p.Extensions {
		fmt.Fprintf(w, "  %s  %s  %s  %s/%s  %s\n", e.Name, e.Kind, p.Target, e.Dir, e.Artifact, HumanSize(e.Size))
	}
	included := p.IncludedFiles()
	var includedBytes int64
	for _, f := range included {
		includedBytes += f.Size
	}
	fmt.Fprintf(w, "includes   %d files, %s\n", len(included), HumanSize(includedBytes))
	for _, f := range included {
		fmt.Fprintf(w, "  %s  %s\n", f.Path, HumanSize(f.Size))
	}
	var total int64
	for _, f := range p.Files {
		total += f.Size
	}
	_, err := fmt.Fprintf(w, "payload    %d files, %s uncompressed\n", len(p.Files), HumanSize(total))
	return err
}

// HumanSize: "12 B", "1.5 KB", "22.7 MB" (base 1024, uma casa decimal).
func HumanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
```

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/build/ -count=1 && go vet ./internal/build/`
Expected: PASS. (`TestPlanWalksTransitiveUsesAndDirectoryModules`: `lib.assets/` aparece como módulo de diretório vazio porque a VM também o listaria; nenhum arquivo dele entra.)

- [ ] **Step 5: commit**

```bash
git add internal/build/plan.go internal/build/list.go internal/build/plan_test.go
git commit -m "feat(build): plano do noxy build — grafo de use, compilacao no build, extensoes, includes e --list"
```

---

### Task 9: `internal/build` — escrita do executável

**Files:**
- Create: `internal/build/write.go`
- Test: `internal/build/write_test.go`

**Interfaces:**
- Consumes: `Plan`, `Options`, `bundle.Pack`, `bundle.RuntimeBytes`, `bundle.Trailer`, `bundle.Open`, `bundle.Extract`.
- Produces: `func Write(p *Plan, opts Options) (outPath string, size int64, err error)`.

- [ ] **Step 1: testes (falham)**

```go
// internal/build/write_test.go
package build

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/estevaofon/noxy/internal/bundle"
)

func fakeRuntime(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "noxy")
	if err := os.WriteFile(path, []byte("RUNTIME-BYTES"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteCreatesOutputDirectoryAndIsDeterministic(t *testing.T) {
	root := writeProject(t, map[string]string{
		"noxy.mod":       "module app\n\ninclude web\n",
		"main.nx":        "use src.a as a\nprint(a.one())\n",
		"src/a.nx":       "func one() -> int\n    return 1\nend\n",
		"web/index.html": "<html></html>\n",
	})
	rt := fakeRuntime(t)
	out := filepath.Join(t.TempDir(), "dist", "app")
	opts := Options{Entry: filepath.Join(root, "main.nx"), Output: out, Runtime: rt, Diag: &bytes.Buffer{}}
	plan, err := MakePlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	got, size, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantOut := out
	if runtime.GOOS == "windows" {
		wantOut += ".exe"
	}
	if got != wantOut {
		t.Fatalf("output %q, want %q", got, wantOut)
	}
	info, err := os.Stat(got)
	if err != nil || info.Size() != size || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		t.Fatalf("stat %v size %d/%d mode %v", err, info.Size(), size, info.Mode())
	}
	if _, err := os.Stat(got + ".tmp"); err == nil {
		t.Fatal("temporary must be renamed away")
	}
	rtBytes, err := bundle.RuntimeBytes(got)
	if err != nil || string(rtBytes) != "RUNTIME-BYTES" {
		t.Fatalf("runtime prefix %q %v", rtBytes, err)
	}
	p, err := bundle.Open(got)
	if err != nil || p == nil {
		t.Fatalf("open: %v %v", p, err)
	}
	defer p.Close()
	dir, m, err := bundle.Extract(p, t.TempDir())
	if err != nil || m.Entry != "main.nx" || m.Includes[0] != "web" {
		t.Fatalf("extract: %v %+v", err, m)
	}
	for _, rel := range []string{"main.nx", "src/a.nx", "web/index.html", "noxy.mod"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("%s: %v", rel, err)
		}
	}
	first, _ := os.ReadFile(got)
	if _, _, err := Write(plan, opts); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(got)
	if !bytes.Equal(first, second) {
		t.Fatal("two builds of the same project must be byte-identical")
	}
}

func TestWriteFromAnAppDoesNotNestPayloads(t *testing.T) {
	root := writeProject(t, map[string]string{"main.nx": "print(1)\n"})
	rt := fakeRuntime(t)
	first := filepath.Join(t.TempDir(), "app1")
	opts := Options{Entry: filepath.Join(root, "main.nx"), Output: first, Runtime: rt, Diag: &bytes.Buffer{}}
	plan, err := MakePlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	firstOut, _, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	opts.Runtime = firstOut
	opts.Output = filepath.Join(t.TempDir(), "app2")
	plan.Runtime = firstOut
	secondOut, _, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	rtBytes, err := bundle.RuntimeBytes(secondOut)
	if err != nil || string(rtBytes) != "RUNTIME-BYTES" {
		t.Fatalf("runtime of the second app %q %v — payload nested", rtBytes, err)
	}
}

func TestWriteDefaultOutputIsTheEntryName(t *testing.T) {
	root := writeProject(t, map[string]string{"hello.nx": "print(1)\n"})
	t.Chdir(t.TempDir())
	opts := Options{Entry: filepath.Join(root, "hello.nx"), Runtime: fakeRuntime(t), Diag: &bytes.Buffer{}}
	plan, err := MakePlan(opts)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := Write(plan, opts)
	if err != nil {
		t.Fatal(err)
	}
	want := "hello"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if out != want {
		t.Fatalf("output %q, want %q", out, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("default output must land in the cwd: %v", err)
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./internal/build/ -run TestWrite -count=1`
Expected: FAIL (`Write` indefinido).

- [ ] **Step 3: implementação**

```go
// internal/build/write.go
package build

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/estevaofon/noxy/internal/bundle"
)

const darwinWarning = "warning: macOS output is experimental and was not validated on this platform (see docs/BUILD.md)"

// Write gera o executavel (spec §6.6): runtime + zip + trailer em
// <saida>.tmp, chmod, rename. Reprodutivel: mesmo projeto, mesmos bytes.
func Write(p *Plan, opts Options) (string, int64, error) {
	diag := opts.Diag
	if diag == nil {
		diag = os.Stderr
	}
	out := opts.Output
	if out == "" {
		out = strings.TrimSuffix(filepath.Base(p.Entry), ".nx")
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(out), ".exe") {
		out += ".exe"
	}
	files := make([]bundle.File, 0, len(p.Files))
	for _, f := range p.Files {
		data, err := os.ReadFile(f.Abs)
		if err != nil {
			return "", 0, err
		}
		files = append(files, bundle.File{Path: f.Path, Data: data, Executable: f.Executable})
	}
	payload, err := bundle.Pack(p.Manifest(), files)
	if err != nil {
		return "", 0, err
	}
	runtimeBytes, err := bundle.RuntimeBytes(p.Runtime)
	if err != nil {
		return "", 0, fmt.Errorf("runtime %s: %w", p.Runtime, err)
	}
	trailer := bundle.Trailer{SHA256: sha256.Sum256(payload), Offset: uint64(len(runtimeBytes)), Size: uint64(len(payload))}
	if dir := filepath.Dir(out); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", 0, err
		}
	}
	tmp := out + ".tmp"
	if err := writeAll(tmp, runtimeBytes, payload, trailer.Marshal()); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(tmp, 0o755); err != nil {
			os.Remove(tmp)
			return "", 0, err
		}
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return "", 0, err
	}
	if runtime.GOOS == "darwin" {
		fmt.Fprintln(diag, darwinWarning)
	}
	return out, int64(len(runtimeBytes) + len(payload) + bundle.TrailerSize), nil
}

func writeAll(path string, parts ...[]byte) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	for _, part := range parts {
		if _, err := io.Copy(f, bytesReader(part)); err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}

func bytesReader(b []byte) io.Reader { return strings.NewReader(string(b)) }
```

(`bytesReader` pode ser trocado por `bytes.NewReader(part)` com o import `bytes` — escolha um; o teste não distingue.)

- [ ] **Step 4: rodar e ver passar**

Run: `go test ./internal/build/ -count=1 && go vet ./internal/build/`
Expected: PASS.

- [ ] **Step 5: commit**

```bash
git add internal/build/write.go internal/build/write_test.go
git commit -m "feat(build): escrita do executavel — runtime + payload + trailer, atomica e reprodutivel"
```

---

### Task 10: CLI — subcomando `noxy build`, modo aplicação, exemplo e integração

**Files:**
- Create: `cmd/noxy/build.go`, `cmd/noxy/appmode.go`, `noxy_examples/build_app.nx`, `noxy_examples/build_app_assets/greeting.txt`
- Modify: `cmd/noxy/main.go` (`main` ~linha 33; `flag.Usage` ~52; `runWithConfig` ~404)
- Test: `cmd/noxy/build_args_test.go`, `cmd/noxy/build_test.go`

**Interfaces:**
- Consumes: `build.Options/MakePlan/Write/HumanSize/Plan.List/Plan.IncludedFiles` (Tasks 8–9), `bundle.Open/Extract/CacheBase/ErrInconsistentTrailer` (Tasks 1–2), `modsrc.NewSealed` (Task 3), `vm.VMConfig.Source` (Task 5), `pkgmanager.FindRoot`.
- Produces: `func parseBuildArgs(args []string) (buildArgs, error)`; `func runBuild(args []string) int`; `func appModeExitCode() (int, bool)`; `func runWithVMConfig(filename, input string, cfg vm.VMConfig, showDisasm bool) int` (o `runWithConfig` atual delega para ele).

- [ ] **Step 1: testes de unidade do parser de flags (falham)**

```go
// cmd/noxy/build_args_test.go
package main

import (
	"strings"
	"testing"
)

func TestParseBuildArgsAcceptsFlagsAfterTheEntry(t *testing.T) {
	got, err := parseBuildArgs([]string{"editor.nx", "-o", "dist/noxy-editor", "--include", "web", "--include=docs/x.txt", "--list"})
	if err != nil {
		t.Fatal(err)
	}
	if got.entry != "editor.nx" || got.output != "dist/noxy-editor" || strings.Join(got.includes, ",") != "web,docs/x.txt" || !got.list {
		t.Fatalf("%+v", got)
	}
	if got, err := parseBuildArgs([]string{"-o=app", "main.nx"}); err != nil || got.output != "app" || got.entry != "main.nx" {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err := parseBuildArgs([]string{"--help"}); err != nil || !got.help {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestParseBuildArgsRejectsBadUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"a.nx", "b.nx"}, {"a.nx", "-o"}, {"a.nx", "--bogus"}} {
		if _, err := parseBuildArgs(args); err == nil {
			t.Errorf("%v must be rejected", args)
		}
	}
}
```

- [ ] **Step 2: rodar e ver falhar**

Run: `go test ./cmd/noxy/ -run TestParseBuildArgs -count=1`
Expected: FAIL (parseBuildArgs indefinido).

- [ ] **Step 3: `cmd/noxy/build.go`**

```go
// cmd/noxy/build.go — `noxy build <entry.nx> [-o <saida>] [--include <p>]... [--list]`
// (spec 2026-09-26 §3.1). Flags podem vir depois do entry; o pacote flag
// para no primeiro positional, por isso o parser e manual.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/estevaofon/noxy/internal/build"
)

const buildUsage = `Usage: noxy build <entry.nx> [-o <output>] [--include <path>]... [--list]

  -o <output>       executable to write (default: the entry name without .nx; .exe is added on Windows)
  --include <path>  file or directory to embed, relative to the project root (repeatable; adds to
                    the "include" lines of noxy.mod)
  --list            print what would go into the payload and write nothing
`

type buildArgs struct {
	entry    string
	output   string
	includes []string
	list     bool
	help     bool
}

func parseBuildArgs(args []string) (buildArgs, error) {
	var out buildArgs
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		takeValue := func() (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s requires a value", name)
			}
			i++
			return args[i], nil
		}
		switch name {
		case "-o", "--o", "-output", "--output":
			v, err := takeValue()
			if err != nil {
				return out, err
			}
			out.output = v
		case "-include", "--include":
			v, err := takeValue()
			if err != nil {
				return out, err
			}
			out.includes = append(out.includes, v)
		case "-list", "--list":
			out.list = true
		case "-h", "-help", "--help":
			out.help = true
		default:
			if strings.HasPrefix(arg, "-") {
				return out, fmt.Errorf("unknown flag %s", arg)
			}
			if out.entry != "" {
				return out, fmt.Errorf("unexpected argument %s", arg)
			}
			out.entry = arg
		}
	}
	if !out.help && out.entry == "" {
		return out, errors.New("missing entry file")
	}
	return out, nil
}

// runBuild devolve o exit code: 0, 1 (falha do build), 2 (uso invalido).
func runBuild(args []string) int {
	parsed, err := parseBuildArgs(args)
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: %s\n%s", err, buildUsage)
		return 2
	}
	if parsed.help {
		fmt.Fprint(diagOut, buildUsage)
		return 0
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: cannot locate the running noxy: %s\n", err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	opts := build.Options{Entry: parsed.entry, Output: parsed.output, Includes: parsed.includes, Runtime: exe, Out: os.Stdout, Diag: diagOut}
	plan, err := build.MakePlan(opts)
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: %s\n", err)
		return 1
	}
	if parsed.list {
		if err := plan.List(os.Stdout); err != nil {
			fmt.Fprintf(diagOut, "noxy build: %s\n", err)
			return 1
		}
		return 0
	}
	out, size, err := build.Write(plan, opts)
	if err != nil {
		fmt.Fprintf(diagOut, "noxy build: %s\n", err)
		return 1
	}
	names := make([]string, 0, len(plan.Extensions))
	for _, e := range plan.Extensions {
		names = append(names, e.Name)
	}
	extensions := fmt.Sprintf("%d extensions", len(names))
	if len(names) != 0 {
		extensions += " (" + strings.Join(names, ", ") + ")"
	}
	fmt.Fprintf(os.Stdout, "noxy build: %d modules, %s, %d included files\n", len(plan.Modules), extensions, len(plan.IncludedFiles()))
	fmt.Fprintf(os.Stdout, "noxy build: wrote %s (%s)\n", out, build.HumanSize(size))
	return 0
}
```

- [ ] **Step 4: `cmd/noxy/appmode.go`**

```go
// cmd/noxy/appmode.go — modo aplicacao (spec 2026-09-26 §3.3, §5): o binario
// carrega um payload NOXYAPP1 e NOXY_INTERPRETER nao esta definida.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/estevaofon/noxy/internal/bundle"
	"github.com/estevaofon/noxy/internal/modsrc"
	"github.com/estevaofon/noxy/internal/pkgmanager"
	"github.com/estevaofon/noxy/internal/vm"
)

// appModeExitCode roda o programa embutido e devolve (exit code, true).
// (0, false) = este binario e o noxy comum, ou NOXY_INTERPRETER esta
// definida: a CLI normal segue. Erro de leitura do proprio executavel conta
// como "sem payload"; trailer inconsistente e fatal.
func appModeExitCode() (int, bool) {
	if os.Getenv("NOXY_INTERPRETER") != "" {
		return 0, false
	}
	exe, err := os.Executable()
	if err != nil {
		return 0, false
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	payload, err := bundle.Open(exe)
	if err != nil {
		if errors.Is(err, bundle.ErrInconsistentTrailer) {
			fmt.Fprintf(diagOut, "Error: %s\n", err)
			return 1, true
		}
		return 0, false
	}
	if payload == nil {
		return 0, false
	}
	base, err := bundle.CacheBase()
	if err != nil {
		payload.Close()
		fmt.Fprintf(diagOut, "Error: %s\n", err)
		return 1, true
	}
	appDir, manifest, err := bundle.Extract(payload, base)
	payload.Close()
	if err != nil {
		fmt.Fprintf(diagOut, "Error: %s\n", err)
		return 1, true
	}
	entry := filepath.Join(appDir, filepath.FromSlash(manifest.Entry))
	content, ok := loadScript(entry)
	if !ok {
		return 1, true
	}
	// Exatamente como `noxy <entry> <args>`: argv[1] e o entry extraido,
	// cwd fica o do usuario.
	os.Args = append([]string{os.Args[0], entry}, os.Args[1:]...)
	rootPath := filepath.Dir(entry)
	projectRoot, _ := pkgmanager.FindRoot(rootPath)
	cfg := vm.VMConfig{RootPath: rootPath, ProjectRoot: projectRoot, Source: modsrc.NewSealed(rootPath, projectRoot)}
	return runWithVMConfig(entry, content, cfg, false), true
}
```

- [ ] **Step 5: `cmd/noxy/main.go`**

No início de `main`, logo após o `defer` do recover:

```go
	// Modo aplicacao (spec 2026-09-26 §5.1): antes de qualquer flag — todos
	// os args pertencem ao programa embutido.
	if code, handled := appModeExitCode(); handled {
		if code != 0 {
			os.Exit(code)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "build" {
		if code := runBuild(os.Args[2:]); code != 0 {
			os.Exit(code)
		}
		return
	}
```

Em `flag.Usage`, a primeira linha vira:

```go
		fmt.Fprintf(os.Stderr, "Usage: noxy [options] [file]\n       noxy build <entry.nx> [-o <output>] [--include <path>]... [--list]\n\nOptions:\n")
```

`runWithConfig` passa a delegar; o corpo atual vira `runWithVMConfig`, com duas linhas diferentes (a criação da VM e o `SetModuleSource`):

```go
// runWithConfig e o caminho da CLI comum: VM com a Source do disco.
func runWithConfig(filename string, input string, rootPath string, showDisasm bool) int {
	return runWithVMConfig(filename, input, vm.VMConfig{RootPath: rootPath}, showDisasm)
}

// runWithVMConfig devolve o codigo de saida (0 sucesso, 1 erro) em vez de
// chamar os.Exit diretamente — quem chama (runFile, modo aplicacao) precisa
// da chance de rodar seus proprios defers. O compilador do entry usa a
// MESMA Source da VM (selada em modo aplicacao).
func runWithVMConfig(filename string, input string, cfg vm.VMConfig, showDisasm bool) int {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) > 0 {
		for _, msg := range p.Errors() {
			fmt.Fprintf(diagOut, "%s\n", msg)
		}
		return 1
	}

	machine := vm.NewWithConfig(cfg)
	defer machine.CloseExtensions()
	defer machine.CloseProcesses()
	defer installExitSignalHandler(machine)()
	c := compiler.NewWithStateAndRoot(make(map[string]ast.NoxyType), make(map[string]*ast.StructStatement), filename, cfg.RootPath)
	c.SetModuleSource(machine.Config.Source)
	c.SetKnownGlobals(append(machine.GlobalNames(), compiler.PluginNativeNames(program)...))
	// ... restante identico ao runWithConfig atual (warnings, disassembly, Interpret) ...
}
```

Mantenha os comentários existentes sobre os `defer`s. `runFile` continua chamando `runWithConfig(filename, input, rootPath, showDisasm)`.

- [ ] **Step 6: exemplo e asset**

```noxy
// noxy_examples/build_app.nx — exemplo do `noxy build` (docs/BUILD.md): um
// package de noxy_libs (math_lib), um asset achado por dirname(argv[1]) e os
// argumentos da linha de comando. Roda igual como script e como executavel:
//     noxy noxy_examples/build_app.nx um dois
//     noxy build noxy_examples/build_app.nx --include noxy_examples/build_app_assets -o dist/build_app
//     dist/build_app um dois
use sys
use io
use math_lib
use strings select substring, trim

// dirname: o diretorio de p, com "/" ou "\" como separador; "." sem separador.
func dirname(p: string) -> string
    let i: int = length(p) - 1
    while i >= 0 do
        let ch: string = substring(p, i, i + 1)
        if ch == "/" || ch == "\\" then
            return substring(p, 0, i)
        end
        i = i - 1
    end
    return "."
end

let args: string[] = sys.argv()
let here: string = dirname(args[1])
let f: io.File = io.open(here + "/build_app_assets/greeting.txt", "r")
let r: io.IOResult = io.read(f)
io.close(f)
if !r.ok then
    eprint("asset missing: " + r.error)
    sys.exit(1)
end
print(trim(r.data))
print("10 + 20 = " + to_str(math_lib.add(10, 20)))
let rest: string = ""
let i: int = 2
while i < length(args) do
    if rest != "" then
        rest = rest + " "
    end
    rest = rest + args[i]
    i = i + 1
end
print("args: " + rest)
```

`noxy_examples/build_app_assets/greeting.txt`:

```
hello from the asset
```

Confira com `go run ./cmd/noxy noxy_examples/build_app.nx um dois` (a partir da raiz): três linhas, `hello from the asset`, `10 + 20 = 30`, `args: um dois`. O runner de exemplos só lista `noxy_examples/*.nx`, então o exemplo entra nele automaticamente e `build_app_assets/` fica de fora.

- [ ] **Step 7: teste de integração (falha até os passos 3–6 existirem)**

```go
// cmd/noxy/build_test.go
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/bundle"
	"github.com/estevaofon/noxy/internal/ext/exttest"
)

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// buildNoxy compila o noxy deste checkout (como sync_flags_test.go).
func buildNoxy(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "noxy"+exeSuffix())
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// appEnv: ambiente do app sem as variaveis do noxy, mais as pedidas.
func appEnv(extra ...string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "NOXY_INTERPRETER=") || strings.HasPrefix(kv, "NOXY_APP_CACHE=") || strings.HasPrefix(kv, "NOXY_PATH=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func run(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func buildExampleApp(t *testing.T, bin string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "app"+exeSuffix())
	text, err := run(t, repoRoot(t), appEnv(), bin, "build", "noxy_examples/build_app.nx", "--include", "noxy_examples/build_app_assets", "-o", out)
	if err != nil || !strings.Contains(text, "noxy build: wrote "+out) {
		t.Fatalf("build: %v\n%s", err, text)
	}
	return out
}

func TestBuildAppRunsWithoutNoxyLibs(t *testing.T) {
	bin := buildNoxy(t)
	app := buildExampleApp(t, bin)

	cache := t.TempDir()
	cwd := t.TempDir() // sem noxy_libs, sem noxy_examples
	decoy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(decoy, "math_lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(decoy, "math_lib", "math_lib.nx"), []byte("func add(a: int, b: int) -> int\n    return -1\nend\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := appEnv("NOXY_APP_CACHE="+cache, "NOXY_PATH="+decoy)

	out, err := run(t, cwd, env, app, "um", "dois")
	if err != nil {
		t.Fatalf("app: %v\n%s", err, out)
	}
	for _, want := range []string{"hello from the asset\n", "10 + 20 = 30\n", "args: um dois\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "-1") {
		t.Fatalf("the sealed app must ignore NOXY_PATH:\n%s", out)
	}
	entries, err := os.ReadDir(cache)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache must hold one app dir: %v %v", entries, err)
	}
	marker := filepath.Join(cache, entries[0].Name(), bundle.MarkerName)
	before, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("marker: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, entries[0].Name(), "noxy_examples", "build_app_assets", "greeting.txt")); err != nil {
		t.Fatalf("asset must be extracted: %v", err)
	}

	again, err := run(t, cwd, env, app, "um", "dois")
	if err != nil || again != out {
		t.Fatalf("second run: %v\n%s", err, again)
	}
	after, _ := os.Stat(marker)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("second start must not re-extract")
	}
}

func TestBuildAppInterpreterEscapeHatch(t *testing.T) {
	bin := buildNoxy(t)
	app := buildExampleApp(t, bin)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "other.nx"), []byte("print(\"interp\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := appEnv("NOXY_APP_CACHE="+t.TempDir(), "NOXY_INTERPRETER=1")
	out, err := run(t, dir, env, app, "other.nx")
	if err != nil || strings.TrimSpace(out) != "interp" {
		t.Fatalf("NOXY_INTERPRETER=1 must run the plain interpreter: %v\n%s", err, out)
	}
	if out, err := run(t, dir, env, app, "--version"); err != nil || !strings.HasPrefix(out, "Noxy v") {
		t.Fatalf("--version under the escape hatch: %v\n%s", err, out)
	}
	second := filepath.Join(t.TempDir(), "app2"+exeSuffix())
	if out, err := run(t, dir, env, app, "build", "other.nx", "-o", second); err != nil {
		t.Fatalf("build from an app: %v\n%s", err, out)
	}
	rt1, err1 := bundle.RuntimeBytes(bin)
	rt2, err2 := bundle.RuntimeBytes(second)
	if err1 != nil || err2 != nil || len(rt1) != len(rt2) {
		t.Fatalf("payload nested: runtime %d vs %d (%v %v)", len(rt1), len(rt2), err1, err2)
	}
	if out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+t.TempDir()), second); err != nil || strings.TrimSpace(out) != "interp" {
		t.Fatalf("app built from an app: %v\n%s", err, out)
	}
}

func TestBuildListAndErrorsGoToTheRightStreams(t *testing.T) {
	bin := buildNoxy(t)
	root := repoRoot(t)
	out, err := run(t, root, appEnv(), bin, "build", "--list", "noxy_examples/build_app.nx", "--include", "noxy_examples/build_app_assets")
	if err != nil {
		t.Fatalf("--list: %v\n%s", err, out)
	}
	for _, want := range []string{"entry      noxy_examples/build_app.nx", "modules    ", "  noxy_libs/math_lib/math_lib.nx", "includes   1 files", "  noxy_examples/build_app_assets/greeting.txt"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "build_app"+exeSuffix())); err == nil {
		t.Fatal("--list must not write the executable")
	}

	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(project, "main.nx"), []byte("use src.bad as bad\nprint(1)\n"), 0o644)
	os.WriteFile(filepath.Join(project, "src", "bad.nx"), []byte("let x: int = \"s\"\n"), 0o644)
	cmd := exec.Command(bin, "build", "main.nx")
	cmd.Dir = project
	cmd.Env = appEnv()
	stdout, stderr := new(strings.Builder), new(strings.Builder)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err = cmd.Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("compile error in a module must exit 1: %v", err)
	}
	if !strings.Contains(stderr.String(), "noxy build: src/bad.nx: ") || stdout.String() != "" {
		t.Fatalf("stdout %q stderr %q", stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(project, "main"+exeSuffix())); err == nil {
		t.Fatal("a failed build must not leave an executable")
	}

	cmd = exec.Command(bin, "build")
	cmd.Env = appEnv()
	if err := cmd.Run(); err == nil || err.(*exec.ExitError).ExitCode() != 2 {
		t.Fatalf("missing entry must exit 2: %v", err)
	}
}

const guestManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
%s = "%s"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"
`

func TestBuildAppWithAProcessExtension(t *testing.T) {
	bin := buildNoxy(t)
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	project := t.TempDir()
	pkg := filepath.Join(project, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"noxy.mod":     "module app\n",
		"main.nx":      "use guest as g\nprint(\"sum: \" + to_str(g.add(2, 3)))\n",
		"noxy_libs/guest/noxy_ext.toml": fmt.Sprintf(guestManifest, runtime.GOOS+"-"+runtime.GOARCH, asset),
		"noxy_libs/guest/guest.nx":      "func add(a: int, b: int) -> int\n    return guest_add(a, b)\nend\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(project, filepath.FromSlash(rel)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	guestBytes, err := os.ReadFile(guest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "bin", asset), guestBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(t.TempDir(), "app"+exeSuffix())
	if out, err := run(t, project, appEnv(), bin, "build", "main.nx", "-o", app); err != nil || !strings.Contains(out, "1 extensions (guest)") {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cache := t.TempDir()
	out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+cache), app)
	if err != nil || !strings.Contains(out, "sum: 5") {
		t.Fatalf("app with extension: %v\n%s", err, out)
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 1 {
		t.Fatalf("cache entries %v", entries)
	}
	extracted := filepath.Join(cache, entries[0].Name(), "noxy_libs", "guest", "bin", asset)
	info, err := os.Stat(extracted)
	if err != nil || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
		t.Fatalf("plugin must be extracted and executable: %v %v", info, err)
	}
}

func TestBuildAppRunsThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	bin := buildNoxy(t)
	app := buildExampleApp(t, bin)
	link := filepath.Join(t.TempDir(), "noxy-editor")
	if err := os.Symlink(app, link); err != nil {
		t.Fatal(err)
	}
	out, err := run(t, t.TempDir(), appEnv("NOXY_APP_CACHE="+t.TempDir()), link, "x")
	if err != nil || !strings.Contains(out, "args: x\n") {
		t.Fatalf("through a symlink: %v\n%s", err, out)
	}
}
```

- [ ] **Step 8: rodar e ver passar**

Run: `go build ./... && go vet ./... && go test ./cmd/... -count=1 && go test ./internal/... -count=1 && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`
Expected: PASS; o runner lista `build_app.nx` entre os exemplos e ele passa.

- [ ] **Step 9: commit**

```bash
git add cmd/noxy/build.go cmd/noxy/appmode.go cmd/noxy/main.go cmd/noxy/build_args_test.go cmd/noxy/build_test.go noxy_examples/build_app.nx noxy_examples/build_app_assets/greeting.txt
git commit -m "feat(cli): noxy build e modo aplicacao — executavel autocontido com payload NOXYAPP1"
```

---

### Task 11: documentação — `docs/BUILD.md`, README, CHANGELOG, AGENTS.md

**Files:**
- Create: `docs/BUILD.md`
- Modify: `README.md` (após "## Usage", ~linha 236), `CHANGELOG.md` (seção `[0.26.0]`), `AGENTS.md` (tabela de pacotes, regras)

- [ ] **Step 1: `docs/BUILD.md`**

```markdown
# Standalone executables (`noxy build`)

`noxy build` turns a Noxy program into a single executable that runs on a
machine without `noxy`, `noxy_libs` or the source tree. Design:
`docs/superpowers/specs/2026-09-26-noxy-build-design.md`.

```bash
noxy build editor.nx -o dist/noxy-editor
dist/noxy-editor ../some-folder          # exactly like `noxy editor.nx ../some-folder`
```

## What goes in

The executable is the `noxy` that ran the build, followed by a zip payload
and a 64-byte trailer (`NOXYAPP1`). The payload holds:

- the entry file and every module reachable through `use`, transitively,
  including packages under `noxy_libs/` (the embedded stdlib is already in
  the runtime and is not copied; a local `.nx` that shadows a stdlib module
  is);
- for each extension package, `noxy_ext.toml` and **this platform's**
  binary from `bin/` (or the `.wasm`);
- `noxy.mod` and `noxy.sum`, when the project has them;
- the assets you declare (below).

**The source is readable by anyone with `unzip`** — `unzip -l dist/noxy-editor`
lists it, `unzip` extracts it. `noxy build` is a packaging tool, not an
obfuscator, and the bytecode payload planned for a later version will not
protect code either (bytecode disassembles with `--disassembly`).

The project root is the directory of the nearest `noxy.mod` above the entry
file, or the entry's directory when there is none. Every module must live
under it: a module found through `NOXY_PATH` outside the root is a build
error.

## Assets

Files your program reads at runtime are declared either in `noxy.mod`:

```text
include web
```

or on the command line, `--include web --include docs/help.txt` (repeatable).
Paths are relative to the project root, use `/`, and cannot escape it. Both
sources are merged. A directory is embedded recursively.

## `--list`

`noxy build --list editor.nx` runs the whole plan — module graph, compile
check, platform binaries, includes — and prints what would be embedded
without writing anything. Use it to spot a missing asset or plugin binary.

## Compile errors come out of the build

Every module is compiled during the build (nothing is executed), so a type
error in `src/x.nx` fails `noxy build` with `src/x.nx: [line N] ...` instead
of failing the executable on the user's machine.

## How the executable runs

1. On start it reads its own trailer. Without one it is a plain `noxy`.
2. The payload is extracted **once** into the user cache, keyed by the
   payload's sha256: `~/.cache/noxy/apps/<hash>/` on Linux,
   `%LocalAppData%\noxy\apps\<hash>\` on Windows,
   `~/Library/Caches/noxy/apps/<hash>/` on macOS. The hash is verified at
   extraction; later starts only check the completion marker
   `.noxy-app-ok`. Extraction is atomic (temporary directory + rename), so
   two instances starting at once agree on one directory.
3. The program runs with `argv = [<the executable>, <cache>/<entry>, args...]`
   and the **current directory unchanged** — so `dirname(argv[1]) + "/web"`
   finds the embedded `web/`, and `noxy-editor .` still opens the folder
   you are in. Module resolution is sealed to the extracted tree:
   `NOXY_PATH` and the current directory are ignored.
4. Extension binaries run from the extracted `noxy_libs/.../bin/`, verified
   against the extracted `noxy.sum` like in a synced project.

A new build has a new hash and a new directory; old ones are never removed.
Deleting the `apps` directory is always safe.

| Variable | Effect |
|---|---|
| `NOXY_INTERPRETER=1` | The executable ignores its payload and behaves as the plain `noxy` (REPL, `--sync`, `noxy file.nx`, even `noxy build`). **Inherited** by child processes. |
| `NOXY_APP_CACHE=<dir>` | Use `<dir>` instead of `<user cache>/noxy/apps`. |
| `NOXY_PATH` | Ignored by a built executable. |

## Running other Noxy files from a built program

A built program has no `noxy` on the machine, but it *is* one. To run
another file with the same interpreter, spawn `sys.executable()` with
`NOXY_INTERPRETER=1` **in the child's environment only**:

```noxy
use sys
// sh:  NOXY_INTERPRETER=1 exec '<exe>' 'file.nx'
// cmd: set NOXY_INTERPRETER=1&& "<exe>" "file.nx"
let exe: string = sys.executable()
```

Do not set the variable in the program's own environment: it is inherited,
and a program started that way which calls `sys.executable()` and runs it
gets the interpreter, not the app. That is what the Noxy Editor's F5 does.

## Platforms

- **Linux and Windows** are the supported targets. The build runs on the
  target platform (no cross-compiling yet: it needs published `noxy`
  binaries, see below). On Windows the output gets `.exe`; extracted
  plugins are `.exe` files under the cache, which antivirus software may
  inspect on first run, as with `noxy.exe` itself.
- **macOS is experimental.** Go signs darwin binaries ad hoc; the appended
  payload sits outside the signed region, so local execution is expected to
  work, but `codesign --verify --strict`, Gatekeeper and notarization reject
  the file. The build prints a warning on macOS. If Apple Silicon kills the
  process, the plan B is to carry the payload in a Mach-O segment (what Node
  SEA/`postject` and Deno's `sui` do) and re-sign ad hoc.

Size: the runtime (~24 MB unstripped on Linux) plus the deflated payload —
the Noxy Editor comes out around 26 MB.

## Not yet

- **Cross-compiling** (`--target windows/amd64` from Linux): arrives when
  the noxy release publishes `noxy-<goos>-<goarch>` binaries with
  `checksums.txt`; the plugin binaries of other platforms are already
  hash-pinned in `noxy.sum`.
- **Bytecode payloads** and reading modules straight from the zip.
- `sys_load_plugin` (deprecated, removed in v0.27.0) is refused by the build.
- Cleaning old cache directories.
```

(Um `docs/*.md` passa pelo Liquid do Pages: o arquivo acima não tem `{{`.)

- [ ] **Step 2: README, CHANGELOG, AGENTS.md**

`README.md`, nova seção após "## Usage":

```markdown
## Standalone executables

`noxy build` packs a program, its modules, its `noxy_libs` packages, this
platform's plugin binaries and the assets listed by `include` in `noxy.mod`
(or `--include`) into one executable that runs without `noxy` installed:

```bash
noxy build editor.nx -o dist/noxy-editor
dist/noxy-editor some-folder
```

Linux and Windows; macOS experimental. Details, cache layout and the
`NOXY_INTERPRETER` escape hatch: [docs/BUILD.md](docs/BUILD.md).
```

`CHANGELOG.md`, dentro de `## [0.26.0] - 2026-09-26`, no `### Added` (nova
entrada) e num `### Changed` (crie a subseção se não existir):

```markdown
- **`noxy build`** (spec `docs/superpowers/specs/2026-09-26-noxy-build-design.md`,
  `docs/BUILD.md`): `noxy build entry.nx [-o saída] [--include p]... [--list]`
  gera um executável autocontido — o próprio `noxy` + payload zip (entry,
  módulos alcançáveis por `use`, packages de `noxy_libs`, `noxy_ext.toml` e
  o binário **desta** plataforma de cada extensão, `noxy.mod`/`noxy.sum`,
  assets) + trailer `NOXYAPP1`. Cada módulo é **compilado no build** (sem
  executar): erro de tipo sai como `src/x.nx: [line N] ...` no build, não
  no executável. No primeiro start o app extrai o payload para
  `<cache do usuário>/noxy/apps/<sha256>` (hash conferido só na extração;
  depois vale o marcador `.noxy-app-ok`), roda com
  `argv = [exe, <appdir>/entry, args...]`, cwd inalterado e resolução de
  módulos selada (`NOXY_PATH` e cwd ignorados). `--list` imprime módulos,
  extensões com plataforma e includes sem gerar nada. `NOXY_INTERPRETER=1`
  faz o app virar o `noxy` comum (herdada pelos filhos — é como o F5 do
  editor roda arquivos); `NOXY_APP_CACHE` troca o cache. Linux e Windows;
  macOS experimental (o build avisa). O fonte embutido é legível com
  `unzip`. Pacotes `internal/bundle` (formato), `internal/build` (plano e
  escrita).
- **`include <caminho>` no `noxy.mod`**: assets que `noxy build` embute,
  relativos ao diretório do `noxy.mod`; `--get` preserva a linha.
- **`sys.executable() -> string`**: caminho do binário que roda o programa
  (o `noxy` ou o app gerado por `noxy build`); `""` em falha.

### Changed
- **Resolução de módulos unificada** em `internal/modsrc.Source`
  (`DiskSource`): compilador e VM resolvem `use` pela mesma interface e
  pela mesma lista de candidatos (antes, duas cópias com `os.*` próprio).
  Sem mudança de comportamento para scripts.
- `noxy build` é subcomando: um arquivo chamado literalmente `build` (sem
  `.nx`) precisa de `noxy ./build`.
```

`AGENTS.md`: na tabela de pacotes, acrescente uma linha
`| internal/modsrc, internal/bundle, internal/build | Origem única de módulos (Source/DiskSource, usada por compiler e vm); formato do executável autocontido (trailer NOXYAPP1, zip, extração); pipeline do noxy build |`
e em `cmd/noxy` acrescente `noxy build (build.go) e modo aplicação (appmode.go)`. Em "Regras que não estão na spec", novo parágrafo:

```markdown
**Módulos.** `use` resolve **só** por `modsrc.Source` (`internal/modsrc`):
compilador (`SetModuleSource`) e VM (`VMConfig.Source`) recebem a mesma
instância; nunca `os.Stat`/`os.ReadFile` para achar módulo. Em modo
aplicação a Source é selada (`NewSealed`: sem `NOXY_PATH`, sem cwd).
`noxy build` compila cada módulo com `vm.CompileModule` (nada executa); o
formato do executável é `internal/bundle`, documentado em `docs/BUILD.md`.
```

- [ ] **Step 3: verificação completa**

Run: `go build ./... && go vet ./... && gofmt -l ./internal ./cmd && go test ./internal/... -count=1 && go test ./cmd/... -count=1 && go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx && git diff --numstat`
Expected: tudo verde; `gofmt -l` sem saída nos arquivos novos; `git diff --numstat` sem arquivo reescrito por completo (EOL).

- [ ] **Step 4: commit**

```bash
git add docs/BUILD.md README.md CHANGELOG.md AGENTS.md
git commit -m "docs(build): docs/BUILD.md, README, CHANGELOG 0.26.0 e AGENTS.md para o noxy build"
```

---

### Task 12: fecho no Noxy-Editor (repositório `../noxy_projects/Noxy-Editor`) e critério de aceite

**Files (outro repositório):**
- Modify: `noxy.mod`, `src/runner.nx` (`command`, ~linha 33), `README.md` (após "## Rodar")

**Interfaces:**
- Consumes: `sys.executable()`, `NOXY_INTERPRETER`, `include` do `noxy.mod` — tudo do `noxy` desta branch (instale-o com `go install ./cmd/noxy` a partir do repositório do noxy antes de começar).

- [ ] **Step 1: `noxy.mod`**

```text
module noxy_editor

noxy v0.26.0

require github.com/estevaofon/noxy_pty v0.2.0
require github.com/estevaofon/noxy_webview v0.1.0

include web
```

- [ ] **Step 2: `src/runner.nx` — o F5 usa o interpretador que roda o editor**

Substitua `command` por:

```noxy
// noxy_exe: o interpretador que roda este editor — o noxy, ou o proprio
// noxy-editor gerado por `noxy build` (docs/BUILD.md do noxy). "" (o
// sistema nao soube dizer) cai no `noxy` do PATH.
func noxy_exe() -> string
    let exe: string = sys.executable()
    if exe == "" then
        return "noxy"
    end
    return exe
end

// command e o que roda: cd na raiz e o interpretador no arquivo, na sintaxe
// do shell da plataforma, com NOXY_INTERPRETER=1 SO no ambiente do filho
// (`VAR=1 exec` no sh; `set` dentro do cmd oculto no Windows) — num
// noxy-editor autocontido a variavel faz o binario rodar como noxy comum, e
// ela e herdada, entao nunca vai no ambiente do editor. No Windows as aspas
// sao seguras aqui porque a linha inteira vai por variavel de ambiente
// (launch_windows), nao pelo comando do sys.exec.
func command(root: string, rel: string) -> string
    if platform.windows() then
        return "cd /D \"" + platform.backslashes(root) + "\" && set NOXY_INTERPRETER=1&& \"" + platform.backslashes(noxy_exe()) + "\" \"" + platform.backslashes(rel) + "\""
    end
    return "cd " + platform.shell_quote(root) + " && NOXY_INTERPRETER=1 exec " + platform.shell_quote(noxy_exe()) + " " + platform.shell_quote(rel)
end
```

Confira que `r.text = "$ noxy " + rel + "\n"` (a linha mostrada no painel) continua legível; não precisa mudar.

- [ ] **Step 3: README do editor — seção "Distribuir" após "Rodar"**

```markdown
## Distribuir

    noxy build editor.nx -o dist/noxy-editor

gera um executável único (Linux e Windows; macOS experimental) que roda sem
`noxy` nem `noxy_libs` na máquina: `dist/noxy-editor pasta`. Ele leva o
`web/` (linha `include web` do `noxy.mod`), os plugins `noxy_webview` e
`noxy_pty` desta plataforma e o fonte do editor — legível com `unzip`. O F5
roda os arquivos com o próprio executável (`sys.executable()` +
`NOXY_INTERPRETER=1` no processo filho); o terminal integrado **não** ganha
um `noxy` no PATH, porque o app não instala nada. Detalhes em
`docs/BUILD.md` do noxy.
```

- [ ] **Step 4: testes do editor e verificação manual do critério de aceite**

Run (no repositório do editor, com o `noxy` desta branch no PATH): `noxy --sync && noxy tests/run.nx && noxy tests/protocol.nx && noxy editor.nx tests/tmp/run` — o F5 num arquivo continua funcionando sob `noxy editor.nx` (mesmo comportamento, `sys.executable()` é o `noxy`).

Run: `noxy build --list editor.nx` — a lista mostra os módulos `src/*.nx`, os wrappers de `noxy_webview`/`noxy_pty`, as duas extensões com `linux/amd64` (ou a plataforma corrente) e `web/index.html`, `web/editor.css`, `web/editor.js`, `web/vendor/*`.

Run: `noxy build editor.nx -o dist/noxy-editor` e, **noutro diretório sem `noxy_libs` e com o `noxy` fora do PATH** (`env -i HOME=$HOME PATH=/usr/bin:/bin DISPLAY=$DISPLAY dist/noxy-editor ../qualquer-pasta`):
1. a janela abre pela extensão webview (stderr: `Noxy Editor em http://127.0.0.1:... (webview)`);
2. Ctrl+` abre o terminal (pty) e um comando ecoa;
3. F5 num `.nx` da pasta roda e a saída aparece no painel;
4. fechar a janela encerra o processo sem órfãos (`pgrep -f noxy-plugin` vazio).

No Windows, o mesmo com `dist\noxy-editor.exe C:\pasta` num PowerShell sem `noxy` no PATH.

- [ ] **Step 5: commit (no repositório do editor)**

```bash
git add noxy.mod src/runner.nx README.md
git commit -m "feat(build): editor distribuivel com noxy build — include web e F5 pelo proprio executavel"
```

Registre em `docs/ACHADOS.md` do editor qualquer coisa que o `noxy build` não tenha dado conta.

---

## Self-review (feito ao escrever o plano)

- **Cobertura da spec:** §3.1 (Task 10, `--list` Task 8), §3.2 (Task 6), §3.3/§5 (Tasks 2 e 10), §3.4 (Task 7), §4 (Tasks 1–2, 9), §6 (Tasks 8–9), §7 (Tasks 3–5), §8 (Task 12), §9 (testes em cada task; integração na Task 10), §10 (Task 11 e a spec §12 na Task 7), §11 (aviso darwin na Task 9; docs na Task 11).
- **Consistência de tipos:** `bundle.File{Path, Data, Executable}` (Task 2) vs `build.File{Path, Size, Executable, Abs}` (Task 8) são tipos distintos de pacotes distintos, convertidos em `Write` (Task 9). `Plan.Manifest()` produz `bundle.Manifest` com `bundle.ModuleEntry`/`bundle.ExtensionEntry`. `vm.CompileModule(modsrc.Module)` (Task 5) é o que `walker.visitModule` chama (Task 8). `runWithVMConfig` (Task 10) é o único ponto novo em `main.go` que o modo aplicação usa.
- **Regra de subdiretório quebrado:** a VM omite em silêncio um subdiretório de módulo de diretório que não carrega (`TestRuntimeDirectoryModuleGlobalsContainOnlyLoadableChildren`); o build é mais estrito — um subdiretório que resolve mas não compila é erro (§6.2 da spec, atualizado). Um subdiretório que não resolve é pulado nos dois.
