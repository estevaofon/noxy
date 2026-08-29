# Process Extensions (tier B) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the in-repository half of issue #80 in v0.23.0: the `noxy-plugin/1` protocol and `ext.Process` backend behind the existing extension seam, the `kind = "process"` manifest, `CloseExtensions` on every exit path, the Go SDK as a nested module, `noxy --get` downloading one platform asset per release and writing a portable `noxy.sum`, benchmarks, docs, and the `sys_load_plugin` deprecation warning (issue #80 checkboxes 2, 3, 4, 7 and the first half of 8).

**Architecture:** `internal/ext` gains a second `Backend` next to the wasm `*Module`: `*Process` speaks length-prefixed NXB frames over the child's stdio, with lazy start, name-bound handshake, `id` multiplexing, per-call deadlines with cooperative CANCEL, poisoning/restart and an EOF-then-kill shutdown. The VM only learns to pick a backend by `manifest.Kind` and to close backends at exit. The SDK lives in its own Go module (`sdk/noxyplugin`, no dependency on `noxy-vm`) with its own NXB codec over Go types; a shared golden-vector file keeps the two codecs honest, and a test plugin built with the SDK at test time (`internal/ext/testdata/processguest`, like `exttest.BuildGuest`) exercises host and SDK against each other. The package manager learns the release layout (`checksums.txt` + one asset), fresh-clone semantics and newest-tag resolution.

**Tech Stack:** Go 1.25 (`go test ./internal/... -count=1`, `go test -race ./internal/vm`), `github.com/BurntSushi/toml`, `golang.org/x/sys/windows` (already a dependency; job objects), `net/http` + `httptest`, `git` CLI, Noxy corpus (`noxy_examples/run_all_tests_concurrent.nx`), `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-08-29-process-extensions-design.md` — read it before any task; `§N` below refers to it. Its facts about the tree were verified at `f00eeec`; the plan branches from `develop` @ `197db4a` (spec merged, PR #112).

## Global Constraints

- Branch `feature/issue-80-process-extensions` off `develop` @ `197db4a` (create with superpowers:using-git-worktrees at execution start); PR against `develop`, title `feat(ext): extensões por processo — protocolo noxy-plugin/1, SDK Go e --get por plataforma (issue #80)`, label `not available to review`, `--assignee @me`, body Summary/Components/Related Issues/Test Plan. **The owner merges; never close issues.** Final version **`v0.23.0`**.
- Commits: Conventional Commits with scope, message in Portuguese (`feat(ext): ...`, `feat(pkgmanager): ...`, `feat(sdk): ...`, `docs(ext): ...`, `chore(version): ...`), ending with the two trailers `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01CoSYynGCX4YehTGrk286DV`.
- Protocol constants verbatim from §2: header `length u32 | kind u8 | flags u8 | reserved u16 | id u32 | fn u32`, all little-endian, header = 12 bytes, kinds `0x01 HELLO, 0x02 CALL, 0x03 RESULT, 0x04 ERROR, 0x05 LOG, 0x06 CANCEL`, version string `noxy-plugin/1`, `flags`/`reserved` must be 0, body cap = `Limits.MaxBytes` (64 MiB), no resynchronization after a bad frame. Defaults: handshake 5000 ms, call 30000 ms (`0` = no deadline), cancel grace 1 s, shutdown grace 2 s.
- Error phrasing verbatim from §6: `extension 'x' failed: <msg>`, `extension 'x' timed out: <export> exceeded <N> ms`, `extension 'x' trapped: ...` (`start: ...`, `handshake: ...`, `process exited (status N)`, `protocol violation: ...`, `<export> exceeded <N> ms and did not cancel; process killed`), `extension 'x' is poisoned by an earlier trap`. Never `null` + stderr.
- Manifest rules verbatim from §7 (`kind`, `[binaries]`, `call_timeout_ms`, `handshake_timeout_ms`, `restart`, per-export `timeout_ms`, `concurrency = "concurrent"` process-only, wasm rejects every process key). Existing wasm manifests must keep parsing unchanged; wasm loader/call semantics are **not** touched.
- Go comments: Portuguese, **no accents**, only where they state a constraint the code cannot (repo idiom — see `internal/ext/call.go`). Docs: `docs/EXTENSIONS.md`/`PACKAGE_MANAGER.md`/spec in English, `CHANGELOG.md`/`AGENTS.md` in Portuguese. `docs/**/*.md` go through the Pages Liquid renderer: never write a double opening brace or a brace-percent opener (not even inside code spans or Go composite literals — write `[]Field{Field{...}}` rather than nesting braces).
- Files are CRLF in the worktree: edit with the Edit/Write tools, never `sed -i`; check `git diff --numstat` before committing (a whole-file diff means line endings were rewritten).
- Test gate after every task: `GOFLAGS=-trimpath=false go test ./internal/... -count=1` (the owner's `go.env` sets `-trimpath`, which breaks `internal/ext` tests — not a regression). After Task 10: also `go test ./... -count=1` with `working-directory` `sdk/noxyplugin`.
- Subprocess tests build a fresh executable. On the owner's Windows machine CrowdStrike may delete a freshly built `.exe` within seconds (see memory `crowdstrike-blocks-fresh-exes`): `exttest.BuildProcessGuest` runs the binary once right after building (stdin closed → exits 0) and rebuilds once if it vanished. In CI (Linux + Windows runners) this never triggers.
- Never use `PATH` lookup for extension binaries; the verified file at `dir/bin/<asset>` is the file that runs (§2.1).
- Do not touch RC/CoW, the executor, `call_validation.go`, or `internal/plugin` (it is removed in v0.25.0, not here).
- Test helpers available: `internal/ext` — `testManifest`, `exttest.BuildGuest`; `internal/vm` — `captureVMSourceAtRoot(t, root, src)`, `compileVMSourceAtRoot`, `captureConcurrencyStderr(t, fn)`, `callBuiltin(t, machine, name, args...)`, `writeExtensionPackage` (wasm); `internal/pkgmanager` — `ParseSumFile`, `RecordExtensionSums`.
- Final validation (AGENTS.md): `go test ./... -count=1`; `go test -race ./internal/vm -count=1`; `go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx`; `go build -o noxy ./cmd/noxy`; `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/noxy`; `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build ./cmd/noxy`.

---

## Part A — Host protocol core (`internal/ext`)

### Task 1: `Backend` interface

**Files:**
- Create: `internal/ext/backend.go`
- Modify: `internal/vm/vm.go:82-85` (`Ext` map type), `internal/vm/extensions.go:31` (map allocation)

**Interfaces:**
- Produces: `type Backend interface { Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error); Close(ctx context.Context) error }` — every later task registers/closes through it. `*Module` (wasm) already has both methods.

- [ ] **Step 1: Write the interface with a compile-time assertion**

```go
// internal/ext/backend.go
package ext

import (
	"context"

	"noxy-vm/internal/value"
)

// Backend e a fronteira que o VM enxerga de uma extensao carregada, seja
// qual for o transporte (wasm em processo ou plugin por processo — spec
// 2026-08-29 §1). Call e seguro para uso concorrente; Close e chamado uma
// vez, na saida do hospedeiro.
type Backend interface {
	Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error)
	Close(ctx context.Context) error
}

var _ Backend = (*Module)(nil)
```

- [ ] **Step 2: Retype the shared map**

In `internal/vm/vm.go` replace `Ext      map[string]*ext.Module` with `Ext      map[string]ext.Backend`; in `internal/vm/extensions.go` replace `shared.Ext = make(map[string]*ext.Module)` with `shared.Ext = make(map[string]ext.Backend)`. Nothing else changes: `shared.Ext[dir] = module` still compiles because `*Module` satisfies `Backend`.

- [ ] **Step 3: Build and run the existing extension tests**

Run: `go build ./... && GOFLAGS=-trimpath=false go test ./internal/ext ./internal/vm -run 'Extension|Ext' -count=1`
Expected: PASS (behaviour unchanged).

- [ ] **Step 4: Commit**

```bash
git add internal/ext/backend.go internal/vm/vm.go internal/vm/extensions.go
git commit -m "refactor(ext): interface Backend na fronteira das extensoes (issue #80)"
```

---

### Task 2: Frame codec

**Files:**
- Create: `internal/ext/frame.go`
- Test: `internal/ext/frame_test.go`

**Interfaces:**
- Produces: `const ProtocolVersion = "noxy-plugin/1"`; `FrameHello, FrameCall, FrameResult, FrameError, FrameLog, FrameCancel byte`; `type Frame struct { Kind byte; ID, Fn uint32; Body []byte }`; `func WriteFrame(w io.Writer, f Frame) error`; `func ReadFrame(r io.Reader, maxBody int) (Frame, error)` (clean EOF → `io.EOF`; truncated → `io.ErrUnexpectedEOF`; anything else → `*ProtocolError`); `type ProtocolError struct{ Detail string }` whose `Error()` is `"protocol violation: " + Detail`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/ext/frame_test.go
package ext

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Frame{Kind: FrameCall, ID: 7, Fn: 2, Body: []byte{0x00}}
	if err := WriteFrame(&buf, in); err != nil {
		t.Fatal(err)
	}
	// length = 12 (cabecalho) + 1 (corpo); tudo LE (spec §2.2)
	want := []byte{
		0x0d, 0x00, 0x00, 0x00, // length
		0x02, 0x00, 0x00, 0x00, // kind, flags, reserved
		0x07, 0x00, 0x00, 0x00, // id
		0x02, 0x00, 0x00, 0x00, // fn
		0x00, // body
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("layout:\n got % x\nwant % x", buf.Bytes(), want)
	}
	out, err := ReadFrame(&buf, DefaultLimits().MaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	if out.Kind != FrameCall || out.ID != 7 || out.Fn != 2 || !bytes.Equal(out.Body, []byte{0x00}) {
		t.Fatalf("round trip: %#v", out)
	}
}

func TestFrameEmptyBody(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, Frame{Kind: FrameCancel, ID: 3}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 16 {
		t.Fatalf("CANCEL is header only (length 12), got %d bytes", buf.Len())
	}
	out, err := ReadFrame(&buf, 0)
	if err != nil || out.Kind != FrameCancel || out.ID != 3 || len(out.Body) != 0 {
		t.Fatalf("empty body: %#v %v", out, err)
	}
}

func readViolation(t *testing.T, raw []byte, maxBody int, wantDetail string) {
	t.Helper()
	_, err := ReadFrame(bytes.NewReader(raw), maxBody)
	var perr *ProtocolError
	if !errors.As(err, &perr) || !bytes.Contains([]byte(perr.Detail), []byte(wantDetail)) {
		t.Fatalf("want ProtocolError containing %q, got %v", wantDetail, err)
	}
}

func TestFrameViolations(t *testing.T) {
	readViolation(t, []byte{0x0b, 0, 0, 0}, 1<<20, "below header size")
	readViolation(t, []byte{0xff, 0xff, 0xff, 0x7f}, 1<<20, "exceeds")
	valid := func(kind, flags byte) []byte {
		return []byte{0x0c, 0, 0, 0, kind, flags, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0}
	}
	readViolation(t, valid(0x09, 0), 1<<20, "unknown frame kind 0x09")
	readViolation(t, valid(0x00, 0), 1<<20, "unknown frame kind 0x00")
	readViolation(t, valid(FrameCall, 0x01), 1<<20, "flags")
}

func TestFrameEOFClassification(t *testing.T) {
	if _, err := ReadFrame(bytes.NewReader(nil), 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("no bytes at all must be a clean io.EOF, got %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x0d, 0, 0, 0, FrameCall}), 1<<20); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated header must be io.ErrUnexpectedEOF, got %v", err)
	}
	truncatedBody := []byte{0x0e, 0, 0, 0, FrameResult, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0x02}
	if _, err := ReadFrame(bytes.NewReader(truncatedBody), 1<<20); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body must be io.ErrUnexpectedEOF, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run Frame -count=1`
Expected: FAIL — `undefined: Frame`, `undefined: WriteFrame`.

- [ ] **Step 3: Implement the codec**

```go
// internal/ext/frame.go
package ext

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Protocolo noxy-plugin/1 (spec 2026-08-29 §2.2, §2.3): um quadro e
// u32 length (bytes apos o campo: cabecalho + corpo) | kind u8 | flags u8 |
// reserved u16 | id u32 | fn u32 | corpo NXB. Tudo little-endian; flags e
// reserved sao zero na v1 e nao sao ponto de extensao.
const (
	ProtocolVersion = "noxy-plugin/1"

	frameHeaderSize = 12

	FrameHello  byte = 0x01
	FrameCall   byte = 0x02
	FrameResult byte = 0x03
	FrameError  byte = 0x04
	FrameLog    byte = 0x05
	FrameCancel byte = 0x06
)

type Frame struct {
	Kind byte
	ID   uint32
	Fn   uint32
	Body []byte
}

// ProtocolError marca um fluxo que perdeu o enquadramento: nao ha
// ressincronizacao (spec §2.2) — o host trata como trap e mata o processo,
// o plugin sai com status 2.
type ProtocolError struct{ Detail string }

func (e *ProtocolError) Error() string { return "protocol violation: " + e.Detail }

func WriteFrame(w io.Writer, f Frame) error {
	length := frameHeaderSize + len(f.Body)
	buf := make([]byte, 4+frameHeaderSize, 4+length)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(length))
	buf[4] = f.Kind
	binary.LittleEndian.PutUint32(buf[8:12], f.ID)
	binary.LittleEndian.PutUint32(buf[12:16], f.Fn)
	buf = append(buf, f.Body...)
	_, err := w.Write(buf)
	return err
}

// ReadFrame le um quadro inteiro. io.EOF so quando nenhum byte do quadro
// foi lido (fim limpo); um quadro cortado no meio e io.ErrUnexpectedEOF;
// qualquer inconsistencia de cabecalho e *ProtocolError.
func ReadFrame(r io.Reader, maxBody int) (Frame, error) {
	var head [4 + frameHeaderSize]byte
	if _, err := io.ReadFull(r, head[:4]); err != nil {
		return Frame{}, err
	}
	length := binary.LittleEndian.Uint32(head[:4])
	if length < frameHeaderSize {
		return Frame{}, &ProtocolError{Detail: fmt.Sprintf("frame length %d below header size %d", length, frameHeaderSize)}
	}
	if uint64(length)-frameHeaderSize > uint64(maxBody) {
		return Frame{}, &ProtocolError{Detail: fmt.Sprintf("frame body of %d bytes exceeds the %d byte limit", length-frameHeaderSize, maxBody)}
	}
	if _, err := io.ReadFull(r, head[4:]); err != nil {
		return Frame{}, unexpected(err)
	}
	kind := head[4]
	if kind < FrameHello || kind > FrameCancel {
		return Frame{}, &ProtocolError{Detail: fmt.Sprintf("unknown frame kind 0x%02x", kind)}
	}
	if head[5] != 0 || head[6] != 0 || head[7] != 0 {
		return Frame{}, &ProtocolError{Detail: "non-zero flags/reserved bits in a v1 frame"}
	}
	f := Frame{
		Kind: kind,
		ID:   binary.LittleEndian.Uint32(head[8:12]),
		Fn:   binary.LittleEndian.Uint32(head[12:16]),
		Body: make([]byte, int(length)-frameHeaderSize),
	}
	if _, err := io.ReadFull(r, f.Body); err != nil {
		return Frame{}, unexpected(err)
	}
	return f, nil
}

// unexpected: depois do campo length, qualquer EOF e um quadro truncado.
func unexpected(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run Frame -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/frame.go internal/ext/frame_test.go
git commit -m "feat(ext): codec de quadros do protocolo noxy-plugin/1 (issue #80)"
```

---

### Task 3: Manifest `kind = "process"`

**Files:**
- Modify: `internal/ext/manifest.go` (whole `ParseManifest`, new fields, new accessors)
- Test: `internal/ext/manifest_test.go` (append)

**Interfaces:**
- Produces: `const KindWasm = "wasm"`, `KindProcess = "process"`; fields `Manifest.Kind string`, `Manifest.Binaries map[string]string`, `Manifest.CallTimeoutMs *int`, `Manifest.HandshakeTimeoutMs *int`, `Manifest.Restart bool`, `ExportDecl.TimeoutMs *int`; methods `(m *Manifest) CallTimeout(export int) time.Duration` (per-export → manifest → 30 s; `0` = no deadline), `(m *Manifest) HandshakeTimeout() time.Duration` (default 5 s; `0` = no deadline), `(m *Manifest) BinaryFor(goos, goarch string) (string, bool)`, `(m *Manifest) PublishedPlatforms() []string` (sorted `goos/goarch`).
- Consumes: nothing new.

- [ ] **Step 1: Write the failing tests** (append to `manifest_test.go`)

```go
const validProcessManifest = `
name = "term"
abi = 1
kind = "process"
concurrency = "concurrent"
capabilities = ["tty"]
call_timeout_ms = 1000
handshake_timeout_ms = 250

[binaries]
linux-amd64 = "noxy-plugin-term-linux-amd64"
windows-amd64 = "noxy-plugin-term-windows-amd64.exe"

[[export]]
name = "term_read_key"
params = []
returns = "string"
timeout_ms = 0

[[export]]
name = "term_close"
params = []
returns = "void"
`

func TestProcessManifestParses(t *testing.T) {
	m, err := ParseManifest([]byte(validProcessManifest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Kind != KindProcess || m.Concurrency != "concurrent" || m.Wasm != "" || m.MemoryMaxMB != 0 {
		t.Fatalf("fields: %#v", m)
	}
	if got := m.CallTimeout(0); got != 0 {
		t.Fatalf("timeout_ms = 0 means no deadline, got %v", got)
	}
	if got := m.CallTimeout(1); got != time.Second {
		t.Fatalf("export without timeout_ms inherits call_timeout_ms, got %v", got)
	}
	if got := m.HandshakeTimeout(); got != 250*time.Millisecond {
		t.Fatalf("handshake: %v", got)
	}
	asset, ok := m.BinaryFor("windows", "amd64")
	if !ok || asset != "noxy-plugin-term-windows-amd64.exe" {
		t.Fatalf("BinaryFor: %q %v", asset, ok)
	}
	if _, ok := m.BinaryFor("freebsd", "amd64"); ok {
		t.Fatal("freebsd is not published")
	}
	if got := m.PublishedPlatforms(); len(got) != 2 || got[0] != "linux/amd64" || got[1] != "windows/amd64" {
		t.Fatalf("PublishedPlatforms: %v", got)
	}
	if got := m.Capabilities; len(got) != 1 || got[0] != "tty" {
		t.Fatalf("process capabilities are declarative and kept: %v", got)
	}
}

func TestProcessManifestDefaults(t *testing.T) {
	src := strings.NewReplacer("call_timeout_ms = 1000\n", "", "handshake_timeout_ms = 250\n", "", "timeout_ms = 0\n", "").Replace(validProcessManifest)
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if m.CallTimeout(0) != 30*time.Second || m.HandshakeTimeout() != 5*time.Second {
		t.Fatalf("defaults: call %v handshake %v", m.CallTimeout(0), m.HandshakeTimeout())
	}
	wasm, err := ParseManifest([]byte(validManifest))
	if err != nil || wasm.Kind != KindWasm {
		t.Fatalf("kind defaults to wasm: %v %#v", err, wasm)
	}
}

func TestProcessManifestRejects(t *testing.T) {
	p := validProcessManifest
	mustFail(t, strings.Replace(p, `kind = "process"`, `kind = "dylib"`, 1), "kind")
	mustFail(t, strings.Replace(p, "[binaries]\nlinux-amd64 = \"noxy-plugin-term-linux-amd64\"\nwindows-amd64 = \"noxy-plugin-term-windows-amd64.exe\"\n", "", 1), "binaries")
	mustFail(t, strings.Replace(p, `linux-amd64 =`, `Linux_AMD64 =`, 1), "binaries key")
	mustFail(t, strings.Replace(p, `"noxy-plugin-term-linux-amd64"`, `"dist/noxy-plugin-term"`, 1), "asset name")
	mustFail(t, strings.Replace(p, `"noxy-plugin-term-windows-amd64.exe"`, `"noxy-plugin-term-windows-amd64"`, 1), ".exe")
	mustFail(t, strings.Replace(p, `kind = "process"`, "kind = \"process\"\nwasm = \"ext.wasm\"", 1), "wasm")
	mustFail(t, strings.Replace(p, `kind = "process"`, "kind = \"process\"\nmemory_max_mb = 64", 1), "memory_max_mb")
	mustFail(t, strings.Replace(p, `["tty"]`, `["Net!"]`, 1), "capability")
	mustFail(t, strings.Replace(p, `call_timeout_ms = 1000`, `call_timeout_ms = -1`, 1), "call_timeout_ms")
	mustFail(t, strings.Replace(p, `handshake_timeout_ms = 250`, `handshake_timeout_ms = -5`, 1), "handshake_timeout_ms")
	mustFail(t, strings.Replace(p, `timeout_ms = 0`, `timeout_ms = -1`, 1), "timeout_ms")
	mustFail(t, strings.Replace(p, `kind = "process"`, "kind = \"process\"\nrestart = true", 1), "restart")
	// stateless continua proibindo stateful, tambem em processo
	mustFail(t, strings.Replace(strings.Replace(p, `"concurrent"`, `"stateless"`, 1), "returns = \"void\"\n", "returns = \"void\"\nstateful = true\n", 1), "stateful")
	// restart so com stateless
	ok := strings.Replace(strings.Replace(p, `"concurrent"`, `"stateless"`, 1), `kind = "process"`, "kind = \"process\"\nrestart = true", 1)
	if _, err := ParseManifest([]byte(ok)); err != nil {
		t.Fatalf("restart with stateless must parse: %v", err)
	}
}

func TestWasmManifestRejectsProcessKeys(t *testing.T) {
	w := validManifest
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\n[binaries]\nlinux-amd64 = \"x\"", 1), "binaries")
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\ncall_timeout_ms = 10", 1), "call_timeout_ms")
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\nhandshake_timeout_ms = 10", 1), "handshake_timeout_ms")
	mustFail(t, strings.Replace(w, `abi = 1`, "abi = 1\nrestart = false", 1), "restart")
	mustFail(t, strings.Replace(w, `"stateless"`, `"concurrent"`, 1), "concurrent")
	mustFail(t, w+"timeout_ms = 5\n", "timeout_ms")
}
```

Add `"time"` to the test file imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'ProcessManifest|WasmManifestRejects' -count=1`
Expected: FAIL — `undefined: KindProcess`, `m.CallTimeout undefined`.

- [ ] **Step 3: Implement**

Replace the types, constants and `ParseManifest` in `internal/ext/manifest.go` with the following (keep `validTypeName`, `parseVersion`, `CheckMinNoxy` and the regexes as they are; add `"sort"` and `"time"` to the imports):

```go
const (
	defaultMemoryMB     = 64
	hostMemoryCeilingMB = 256
	supportedABI        = 1

	KindWasm    = "wasm"
	KindProcess = "process"

	defaultCallTimeoutMs      = 30000
	defaultHandshakeTimeoutMs = 5000
)

type ExportDecl struct {
	Name     string   `toml:"name"`
	Params   []string `toml:"params"`
	Returns  string   `toml:"returns"`
	Stateful bool     `toml:"stateful"`
	// TimeoutMs so vale em kind = "process"; nil herda call_timeout_ms.
	TimeoutMs *int `toml:"timeout_ms"`
}

type Manifest struct {
	Name         string       `toml:"name"`
	ABI          int          `toml:"abi"`
	Kind         string       `toml:"kind"`
	MinNoxy      string       `toml:"min_noxy"`
	Concurrency  string       `toml:"concurrency"`
	Capabilities []string     `toml:"capabilities"`
	MemoryMaxMB  int          `toml:"memory_max_mb"`
	Wasm         string       `toml:"wasm"`
	Exports      []ExportDecl `toml:"export"`

	// Chaves de kind = "process" (spec 2026-08-29 §7). Ponteiros distinguem
	// "ausente" de "0" — 0 significa sem prazo.
	Binaries           map[string]string `toml:"binaries"`
	CallTimeoutMs      *int              `toml:"call_timeout_ms"`
	HandshakeTimeoutMs *int              `toml:"handshake_timeout_ms"`
	Restart            bool              `toml:"restart"`
}

var (
	binaryKeyRE  = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+$`)
	assetNameRE  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	capabilityRE = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

func ParseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	meta, err := toml.Decode(string(data), &m)
	if err != nil {
		return nil, fmt.Errorf("noxy_ext.toml: %w", err)
	}
	// Chaves desconhecidas sao erro, nao warning (spec §10): typo falha na
	// publicacao, nao silenciosamente em runtime.
	if undecoded := meta.Undecoded(); len(undecoded) != 0 {
		return nil, fmt.Errorf("noxy_ext.toml: unknown key %q", undecoded[0].String())
	}
	if !manifestNameRE.MatchString(m.Name) {
		return nil, fmt.Errorf("noxy_ext.toml: invalid extension name %q", m.Name)
	}
	if m.ABI != supportedABI {
		return nil, fmt.Errorf("noxy_ext.toml: unsupported abi %d (host supports %d)", m.ABI, supportedABI)
	}
	switch m.Kind {
	case "":
		m.Kind = KindWasm
	case KindWasm, KindProcess:
	default:
		return nil, fmt.Errorf("noxy_ext.toml: invalid kind %q (wasm | process)", m.Kind)
	}
	switch m.Concurrency {
	case "":
		m.Concurrency = "single"
	case "single", "stateless":
	case "concurrent":
		if m.Kind != KindProcess {
			return nil, fmt.Errorf("noxy_ext.toml: concurrency \"concurrent\" is only valid for kind = \"process\"")
		}
	default:
		return nil, fmt.Errorf("noxy_ext.toml: invalid concurrency %q", m.Concurrency)
	}
	if len(m.Exports) == 0 {
		return nil, fmt.Errorf("noxy_ext.toml: at least one [[export]] is required")
	}
	prefix := m.Name + "_"
	seen := map[string]bool{}
	for _, exp := range m.Exports {
		if !strings.HasPrefix(exp.Name, prefix) {
			return nil, fmt.Errorf("noxy_ext.toml: export %q must start with %q", exp.Name, prefix)
		}
		if !manifestNameRE.MatchString(exp.Name) {
			return nil, fmt.Errorf("noxy_ext.toml: invalid export name %q", exp.Name)
		}
		if seen[exp.Name] {
			return nil, fmt.Errorf("noxy_ext.toml: duplicate export %q", exp.Name)
		}
		seen[exp.Name] = true
		for _, p := range exp.Params {
			if p == "void" || !validTypeName(p) {
				return nil, fmt.Errorf("noxy_ext.toml: export %q: invalid param type %q", exp.Name, p)
			}
		}
		if exp.Returns == "" || !validTypeName(exp.Returns) {
			return nil, fmt.Errorf("noxy_ext.toml: export %q: invalid return type %q", exp.Name, exp.Returns)
		}
		if m.Concurrency == "stateless" && exp.Stateful {
			return nil, fmt.Errorf("noxy_ext.toml: stateless extension cannot declare stateful export %q", exp.Name)
		}
	}
	if m.Kind == KindProcess {
		if err := m.validateProcessKeys(meta); err != nil {
			return nil, err
		}
		return &m, nil
	}
	if err := m.validateWasmKeys(meta); err != nil {
		return nil, err
	}
	return &m, nil
}

// validateProcessKeys aplica as regras de kind = "process" (spec §7): as
// chaves do wasm nao existem aqui, [binaries] e obrigatoria e cada asset e
// um nome de arquivo (vai parar em bin/), .exe no Windows.
func (m *Manifest) validateProcessKeys(meta toml.MetaData) error {
	if m.Wasm != "" {
		return fmt.Errorf("noxy_ext.toml: key \"wasm\" is not valid for kind = \"process\"")
	}
	if meta.IsDefined("memory_max_mb") {
		return fmt.Errorf("noxy_ext.toml: key \"memory_max_mb\" is not valid for kind = \"process\" (no sandbox, no cap)")
	}
	if len(m.Binaries) == 0 {
		return fmt.Errorf("noxy_ext.toml: kind = \"process\" requires a [binaries] table with at least one entry")
	}
	for key, asset := range m.Binaries {
		if !binaryKeyRE.MatchString(key) {
			return fmt.Errorf("noxy_ext.toml: invalid binaries key %q (want \"<goos>-<goarch>\")", key)
		}
		if !assetNameRE.MatchString(asset) {
			return fmt.Errorf("noxy_ext.toml: invalid asset name %q for %s (a file name, no path)", asset, key)
		}
		if strings.HasPrefix(key, "windows-") && !strings.HasSuffix(asset, ".exe") {
			return fmt.Errorf("noxy_ext.toml: windows asset %q must end in .exe", asset)
		}
	}
	for _, c := range m.Capabilities {
		if !capabilityRE.MatchString(c) {
			return fmt.Errorf("noxy_ext.toml: invalid capability name %q", c)
		}
	}
	if m.CallTimeoutMs != nil && *m.CallTimeoutMs < 0 {
		return fmt.Errorf("noxy_ext.toml: call_timeout_ms must not be negative")
	}
	if m.HandshakeTimeoutMs != nil && *m.HandshakeTimeoutMs < 0 {
		return fmt.Errorf("noxy_ext.toml: handshake_timeout_ms must not be negative")
	}
	if m.Restart && m.Concurrency != "stateless" {
		return fmt.Errorf("noxy_ext.toml: restart = true requires concurrency = \"stateless\" (handles would dangle)")
	}
	for _, exp := range m.Exports {
		if exp.TimeoutMs != nil && *exp.TimeoutMs < 0 {
			return fmt.Errorf("noxy_ext.toml: export %q: timeout_ms must not be negative", exp.Name)
		}
	}
	return nil
}

// validateWasmKeys mantem as regras do M1 e rejeita as chaves de processo.
func (m *Manifest) validateWasmKeys(meta toml.MetaData) error {
	if m.Binaries != nil {
		return fmt.Errorf("noxy_ext.toml: key \"binaries\" is only valid for kind = \"process\"")
	}
	if m.CallTimeoutMs != nil {
		return fmt.Errorf("noxy_ext.toml: key \"call_timeout_ms\" is only valid for kind = \"process\"")
	}
	if m.HandshakeTimeoutMs != nil {
		return fmt.Errorf("noxy_ext.toml: key \"handshake_timeout_ms\" is only valid for kind = \"process\"")
	}
	if meta.IsDefined("restart") {
		return fmt.Errorf("noxy_ext.toml: key \"restart\" is only valid for kind = \"process\"")
	}
	for _, exp := range m.Exports {
		if exp.TimeoutMs != nil {
			return fmt.Errorf("noxy_ext.toml: export %q: timeout_ms is only valid for kind = \"process\"", exp.Name)
		}
	}
	if m.Wasm == "" {
		m.Wasm = "ext.wasm"
	}
	if m.MemoryMaxMB == 0 {
		m.MemoryMaxMB = defaultMemoryMB
	}
	if m.MemoryMaxMB < 0 {
		return fmt.Errorf("noxy_ext.toml: memory_max_mb %d must not be negative", m.MemoryMaxMB)
	}
	if m.MemoryMaxMB > hostMemoryCeilingMB {
		return fmt.Errorf("noxy_ext.toml: memory_max_mb %d exceeds host ceiling %d", m.MemoryMaxMB, hostMemoryCeilingMB)
	}
	// M1 nao implementa capability nenhuma: aceitar a declaracao seria
	// prometer o que o host ignora (revisao do plano, item 6).
	if len(m.Capabilities) != 0 {
		return fmt.Errorf("noxy_ext.toml: capabilities are not supported in this phase (M1)")
	}
	return nil
}

// CallTimeout e o prazo do export (spec §4.3): timeout_ms do export, senao
// call_timeout_ms, senao 30 s. Zero = sem prazo.
func (m *Manifest) CallTimeout(export int) time.Duration {
	if export >= 0 && export < len(m.Exports) && m.Exports[export].TimeoutMs != nil {
		return time.Duration(*m.Exports[export].TimeoutMs) * time.Millisecond
	}
	if m.CallTimeoutMs != nil {
		return time.Duration(*m.CallTimeoutMs) * time.Millisecond
	}
	return defaultCallTimeoutMs * time.Millisecond
}

func (m *Manifest) HandshakeTimeout() time.Duration {
	if m.HandshakeTimeoutMs != nil {
		return time.Duration(*m.HandshakeTimeoutMs) * time.Millisecond
	}
	return defaultHandshakeTimeoutMs * time.Millisecond
}

func (m *Manifest) BinaryFor(goos, goarch string) (string, bool) {
	asset, ok := m.Binaries[goos+"-"+goarch]
	return asset, ok
}

// PublishedPlatforms lista "goos/goarch" em ordem, para mensagens de erro.
func (m *Manifest) PublishedPlatforms() []string {
	out := make([]string, 0, len(m.Binaries))
	for key := range m.Binaries {
		out = append(out, strings.Replace(key, "-", "/", 1))
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run the whole ext package**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -count=1`
Expected: PASS, including the pre-existing manifest tests (wasm defaults unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/ext/manifest.go internal/ext/manifest_test.go
git commit -m "feat(ext): manifesto kind = \"process\" — [binaries], prazos, restart, concurrent (issue #80)"
```

---

### Task 4: Body helpers for HELLO / ERROR / LOG

**Files:**
- Create: `internal/ext/process_proto.go`
- Test: `internal/ext/process_proto_test.go`

**Interfaces:**
- Produces (package-private, used by Task 5): `func encodeStringMap(fields map[string]value.Value, limits Limits) ([]byte, error)`; `func helloBody(noxyVersion, extName string, exports []string, limits Limits) ([]byte, error)`; `func decodeBodyMap(body []byte, limits Limits) (*value.ObjMap, error)`; `func mapString(m *value.ObjMap, key string) (string, bool)`; `func mapInt(m *value.ObjMap, key string) (int64, bool)`.
- Consumes: `EncodeValue`, `DecodeValue`, `value.NewMap/NewString/NewArray`, `ObjMap.Set/Get`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/ext/process_proto_test.go
package ext

import (
	"testing"

	"noxy-vm/internal/value"
)

func TestHelloBodyCarriesExportsInOrder(t *testing.T) {
	body, err := helloBody("v0.23.0", "term", []string{"term_a", "term_b"}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	m, err := decodeBodyMap(body, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if proto, _ := mapString(m, "protocol"); proto != ProtocolVersion {
		t.Fatalf("protocol: %q", proto)
	}
	if ext, _ := mapString(m, "extension"); ext != "term" {
		t.Fatalf("extension: %q", ext)
	}
	if noxy, _ := mapString(m, "noxy"); noxy != "v0.23.0" {
		t.Fatalf("noxy: %q", noxy)
	}
	exports, ok := m.Get("exports")
	if !ok {
		t.Fatal("exports missing")
	}
	elems := exports.Obj.(*value.ObjArray).Elements
	if len(elems) != 2 || elems[0].Obj.(string) != "term_a" || elems[1].Obj.(string) != "term_b" {
		t.Fatalf("exports order: %#v", elems)
	}
}

func TestMapAccessorsTolerateMissingAndWrongTypes(t *testing.T) {
	body, _ := encodeStringMap(map[string]value.Value{
		"message": value.NewString("boom"),
		"level":   value.NewInt(2),
	}, DefaultLimits())
	m, err := decodeBodyMap(body, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := mapString(m, "message"); !ok || s != "boom" {
		t.Fatalf("message: %q %v", s, ok)
	}
	if n, ok := mapInt(m, "level"); !ok || n != 2 {
		t.Fatalf("level: %d %v", n, ok)
	}
	if _, ok := mapString(m, "level"); ok {
		t.Fatal("level is an int, not a string")
	}
	if _, ok := mapString(m, "absent"); ok {
		t.Fatal("absent key must report !ok")
	}
}

func TestDecodeBodyMapRejectsNonMap(t *testing.T) {
	body, _ := EncodeValue(value.NewInt(1), DefaultLimits())
	if _, err := decodeBodyMap(body, DefaultLimits()); err == nil {
		t.Fatal("an int body is not a map")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'HelloBody|MapAccessors|DecodeBodyMap' -count=1`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement**

```go
// internal/ext/process_proto.go
package ext

import (
	"fmt"

	"noxy-vm/internal/value"
)

// Corpos dos quadros fora de chamada (HELLO, ERROR, LOG — spec §2.4, §2.6)
// sao mapas NXB com chaves string. Chaves desconhecidas sao ignoradas: e o
// unico canal aditivo da v1 (spec §2.8).

func encodeStringMap(fields map[string]value.Value, limits Limits) ([]byte, error) {
	m := value.NewMap()
	obj := m.Obj.(*value.ObjMap)
	for key, item := range fields {
		obj.Set(key, item)
	}
	return EncodeValue(m, limits)
}

// helloBody e o HELLO do host: versao do protocolo, versao do noxy, nome da
// extensao e os exports na ordem do manifesto (= fn index).
func helloBody(noxyVersion, extName string, exports []string, limits Limits) ([]byte, error) {
	names := make([]value.Value, len(exports))
	for i, name := range exports {
		names[i] = value.NewString(name)
	}
	return encodeStringMap(map[string]value.Value{
		"protocol":  value.NewString(ProtocolVersion),
		"noxy":      value.NewString(noxyVersion),
		"extension": value.NewString(extName),
		"exports":   value.NewArray(names),
	}, limits)
}

func decodeBodyMap(body []byte, limits Limits) (*value.ObjMap, error) {
	v, err := DecodeValue(body, limits)
	if err != nil {
		return nil, err
	}
	m, ok := v.Obj.(*value.ObjMap)
	if v.Type != value.VAL_OBJ || !ok {
		return nil, fmt.Errorf("body is not a map")
	}
	return m, nil
}

func mapString(m *value.ObjMap, key string) (string, bool) {
	v, ok := m.Get(key)
	if !ok || v.Type != value.VAL_OBJ {
		return "", false
	}
	s, ok := v.Obj.(string)
	return s, ok
}

func mapInt(m *value.ObjMap, key string) (int64, bool) {
	v, ok := m.Get(key)
	if !ok || v.Type != value.VAL_INT {
		return 0, false
	}
	return v.Int(), true
}
```

- [ ] **Step 4: Run to verify pass**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'HelloBody|MapAccessors|DecodeBodyMap' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/process_proto.go internal/ext/process_proto_test.go
git commit -m "feat(ext): corpos NXB de HELLO/ERROR/LOG do protocolo de processo (issue #80)"
```

---

### Task 5: `Process` — start, handshake, reader, calls, logs

**Files:**
- Create: `internal/ext/process.go`
- Test: `internal/ext/process_test.go` (fake plugin over `io.Pipe`; no subprocess)

**Interfaces:**
- Produces: `type ProcessConfig struct { Path, NoxyVersion string; Log io.Writer; Limits Limits }`; `func NewProcess(manifest *Manifest, cfg ProcessConfig) *Process` (real spawner, Task 8); `func newProcess(manifest *Manifest, cfg ProcessConfig, spawn spawnFunc) *Process` (tests); `type procConn interface { Stdin() io.WriteCloser; Stdout() io.Reader; Wait() error; Kill() error }`; `type spawnFunc func(ctx context.Context) (procConn, error)`; `(*Process).Call`, `(*Process).Close` (Backend). Package vars `cancelGrace = time.Second`, `shutdownGrace = 2 * time.Second` (tests shorten them).
- Consumes: Tasks 2–4, `checkDeclaredReturn` (`call.go`), `EncodeArgs`/`DecodeValue`.

Semantics implemented here (§4.1, §4.2, §2.4–§2.6, §5): lazy start on the first `Call`; concurrent first calls share one start; the handshake tolerates LOG frames before the plugin's HELLO; ERROR at handshake → `trapped: handshake: plugin refused: <msg>`; version mismatch → `trapped: handshake: plugin speaks "x", host speaks "noxy-plugin/1"`; no reply within `HandshakeTimeout()` → `trapped: handshake: no reply within <d>`; RESULT checked with `checkDeclaredReturn`; ERROR → `failed: <msg>`; LOG → `[ext <name>] <msg>` on `cfg.Log`; `single` serializes the CALL/reply exchange with a mutex. Timeouts, death and Close are Tasks 6–7 but the struct fields for them are declared now.

- [ ] **Step 1: Write the fake plugin harness and the first tests**

```go
// internal/ext/process_test.go
package ext

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

// fakeConn e o par de pipes que o host enxerga como o processo do plugin.
type fakeConn struct {
	stdinR  *io.PipeReader // o plugin le daqui
	stdinW  *io.PipeWriter // o host escreve aqui (Stdin)
	stdoutR *io.PipeReader // o host le daqui (Stdout)
	stdoutW *io.PipeWriter // o plugin escreve aqui
	exited  chan struct{}
	once    sync.Once
	waitErr error
	killed  atomic.Bool
}

func newFakeConn() *fakeConn {
	c := &fakeConn{exited: make(chan struct{})}
	c.stdinR, c.stdinW = io.Pipe()
	c.stdoutR, c.stdoutW = io.Pipe()
	return c
}

func (c *fakeConn) Stdin() io.WriteCloser { return c.stdinW }
func (c *fakeConn) Stdout() io.Reader     { return c.stdoutR }
func (c *fakeConn) Wait() error           { <-c.exited; return c.waitErr }

// Kill num processo que ja saiu e um no-op (como os.Process.Kill): so
// conta como "morto pelo host" quando o plugin ainda estava vivo.
func (c *fakeConn) Kill() error {
	select {
	case <-c.exited:
		return nil
	default:
	}
	c.killed.Store(true)
	c.exit(errors.New("killed"))
	return nil
}

// syncBuffer e um bytes.Buffer seguro para o leitor do host escrever
// enquanto o teste le (-race).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// exit encerra o "processo": fecha o lado do plugin dos dois pipes (o host
// ve EOF em Stdout) e libera Wait com err.
func (c *fakeConn) exit(err error) {
	c.once.Do(func() {
		c.waitErr = err
		_ = c.stdoutW.Close()
		_ = c.stdinR.Close()
		close(c.exited)
	})
}

// fakePlugin fala noxy-plugin/1 de dentro do teste.
type fakePlugin struct {
	conn     *fakeConn
	protocol string                       // "" → ProtocolVersion
	refuse   string                       // != "" → ERROR id 0 no handshake
	silent   bool                         // nao responde ao HELLO
	handle   func(p *fakePlugin, f Frame) // por CALL; nil → echo do 1o argumento
	onCancel func(p *fakePlugin, id uint32)
	ignoreEOF bool                        // nao sai quando o host fecha stdin
	preHelloLog string                    // != "" → LOG antes de responder o HELLO
	hello    chan Frame
	writeMu  sync.Mutex
}

func (p *fakePlugin) send(t testing.TB, f Frame) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if err := WriteFrame(p.conn.stdoutW, f); err != nil && t != nil {
		t.Logf("fake plugin write: %v", err)
	}
}

func (p *fakePlugin) result(id uint32, v value.Value) {
	body, _ := EncodeValue(v, DefaultLimits())
	p.send(nil, Frame{Kind: FrameResult, ID: id, Body: body})
}

func (p *fakePlugin) fail(id uint32, msg string) {
	body, _ := encodeStringMap(map[string]value.Value{"message": value.NewString(msg)}, DefaultLimits())
	p.send(nil, Frame{Kind: FrameError, ID: id, Body: body})
}

func (p *fakePlugin) log(level int64, msg string) {
	body, _ := encodeStringMap(map[string]value.Value{"level": value.NewInt(level), "message": value.NewString(msg)}, DefaultLimits())
	p.send(nil, Frame{Kind: FrameLog, Body: body})
}

func (p *fakePlugin) serve() {
	for {
		f, err := ReadFrame(p.conn.stdinR, DefaultLimits().MaxBytes)
		if err != nil {
			if !p.ignoreEOF {
				p.conn.exit(nil) // EOF do host = shutdown, status 0 (spec §2.7)
			}
			return
		}
		switch f.Kind {
		case FrameHello:
			p.hello <- f
			if p.silent {
				continue
			}
			if p.preHelloLog != "" {
				p.log(0, p.preHelloLog)
			}
			if p.refuse != "" {
				p.fail(0, p.refuse)
				continue
			}
			proto := p.protocol
			if proto == "" {
				proto = ProtocolVersion
			}
			body, _ := encodeStringMap(map[string]value.Value{"protocol": value.NewString(proto), "sdk": value.NewString("fake/0")}, DefaultLimits())
			p.send(nil, Frame{Kind: FrameHello, Body: body})
		case FrameCall:
			if p.handle != nil {
				go p.handle(p, f)
				continue
			}
			args, _ := DecodeArgs(f.Body, DefaultLimits())
			v := value.NewNull()
			if len(args) != 0 {
				v = args[0]
			}
			p.result(f.ID, v)
		case FrameCancel:
			if p.onCancel != nil {
				p.onCancel(p, f.ID)
			}
		}
	}
}

// fakeHarness cria um fakePlugin novo por spawn (necessario para restart).
type fakeHarness struct {
	mu      sync.Mutex
	spawns  int
	current *fakePlugin
	setup   func(p *fakePlugin)
	logs    syncBuffer
}

func (h *fakeHarness) spawn(ctx context.Context) (procConn, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.spawns++
	p := &fakePlugin{conn: newFakeConn(), hello: make(chan Frame, 4)}
	if h.setup != nil {
		h.setup(p)
	}
	h.current = p
	go p.serve()
	return p.conn, nil
}

func (h *fakeHarness) plugin() *fakePlugin {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.current
}

const processTestManifest = `
name = "guest"
abi = 1
kind = "process"
concurrency = "%s"
call_timeout_ms = 200
handshake_timeout_ms = 300
%s

[binaries]
linux-amd64 = "guest-linux-amd64"

[[export]]
name = "guest_echo"
params = ["any"]
returns = "any"

[[export]]
name = "guest_fail"
params = []
returns = "any"

[[export]]
name = "guest_slow"
params = []
returns = "any"
timeout_ms = 50

[[export]]
name = "guest_int"
params = []
returns = "int"
`

func newFakeProcess(t *testing.T, concurrency, extraKeys string, setup func(p *fakePlugin)) (*Process, *fakeHarness) {
	t.Helper()
	m, err := ParseManifest([]byte(fmt.Sprintf(processTestManifest, concurrency, extraKeys)))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	h := &fakeHarness{setup: setup}
	p := newProcess(m, ProcessConfig{NoxyVersion: "v0.23.0", Log: &h.logs}, h.spawn)
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p, h
}

func callEcho(t *testing.T, p *Process, v value.Value) (value.Value, error) {
	t.Helper()
	return p.Call(context.Background(), 0, []value.Value{v})
}

func TestProcessLazyStartAndEcho(t *testing.T) {
	p, h := newFakeProcess(t, "single", "", nil)
	if h.spawns != 0 {
		t.Fatal("NewProcess must not start the process (spec §4.1)")
	}
	got, err := callEcho(t, p, value.NewInt(42))
	if err != nil || got.Int() != 42 {
		t.Fatalf("echo: %#v %v", got, err)
	}
	hello := <-h.plugin().hello
	m, err := decodeBodyMap(hello.Body, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if proto, _ := mapString(m, "protocol"); proto != ProtocolVersion {
		t.Fatalf("host HELLO protocol: %q", proto)
	}
	exports, _ := m.Get("exports")
	names := exports.Obj.(*value.ObjArray).Elements
	if len(names) != 4 || names[2].Obj.(string) != "guest_slow" {
		t.Fatalf("exports in manifest order: %#v", names)
	}
	if _, err := callEcho(t, p, value.NewString("again")); err != nil {
		t.Fatal(err)
	}
	if h.spawns != 1 {
		t.Fatalf("one process per extension, got %d spawns", h.spawns)
	}
}

func TestProcessHandshakeRefusedIsTrapAndPoisons(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.refuse = `no handler for export "guest_fail"` })
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), `extension 'guest' trapped: handshake: plugin refused: no handler for export "guest_fail"`) {
		t.Fatalf("got %v", err)
	}
	_, err = callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' is poisoned by an earlier trap") {
		t.Fatalf("second call must see the poison, got %v", err)
	}
}

func TestProcessHandshakeVersionMismatch(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.protocol = "noxy-plugin/2" })
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), `trapped: handshake: plugin speaks "noxy-plugin/2", host speaks "noxy-plugin/1"`) {
		t.Fatalf("got %v", err)
	}
}

func TestProcessHandshakeTimeoutKills(t *testing.T) {
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.silent = true })
	start := time.Now()
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: handshake: no reply within 300ms") {
		t.Fatalf("got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("handshake timeout must be honoured, took %v", elapsed)
	}
	if !h.plugin().conn.killed.Load() {
		t.Fatal("a plugin that never replies must be killed")
	}
}

func TestProcessErrorFrameIsFailedNotPoison(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			if f.Fn == 1 {
				fp.fail(f.ID, "boom")
				return
			}
			fp.result(f.ID, value.NewInt(1))
		}
	})
	_, err := p.Call(context.Background(), 1, nil)
	if err == nil || err.Error() != "extension 'guest' failed: boom" {
		t.Fatalf("got %v", err)
	}
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatalf("declared failure must not poison: %v", err)
	}
}

func TestProcessDeclaredReturnIsChecked(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.result(f.ID, value.NewString("not an int")) }
	})
	_, err := p.Call(context.Background(), 3, nil)
	if err == nil || !strings.Contains(err.Error(), `result does not match declared return type "int"`) {
		t.Fatalf("got %v", err)
	}
	if _, err := p.Call(context.Background(), 3, nil); err == nil || strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("a lying result is an error, not a trap: %v", err)
	}
}

func TestProcessLogFramesGoToLog(t *testing.T) {
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.preHelloLog = "hello" // LOG antes do HELLO do plugin e permitido (spec §2.4)
		fp.handle = func(fp *fakePlugin, f Frame) {
			fp.log(1, "working")
			fp.result(f.ID, value.NewNull())
		}
	})
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatal(err)
	}
	h.plugin().log(0, "late")
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(h.logs.String(), "[ext guest] late") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	got := h.logs.String()
	for _, want := range []string{"[ext guest] hello\n", "[ext guest] working\n", "[ext guest] late\n"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output missing %q: %q", want, got)
		}
	}
}

func TestProcessOutOfOrderRepliesConcurrent(t *testing.T) {
	release := make(chan struct{})
	p, _ := newFakeProcess(t, "concurrent", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			args, _ := DecodeArgs(f.Body, DefaultLimits())
			if args[0].Int() == 1 {
				<-release // a primeira chamada so responde depois da segunda
			}
			fp.result(f.ID, args[0])
		}
	})
	var wg sync.WaitGroup
	results := make([]int64, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := callEcho(t, p, value.NewInt(int64(i+1)))
			if err != nil {
				t.Errorf("call %d: %v", i+1, err)
				return
			}
			results[i] = got.Int()
			if i == 1 {
				close(release)
			}
		}(i)
	}
	wg.Wait()
	if results[0] != 1 || results[1] != 2 {
		t.Fatalf("replies must route by id: %v", results)
	}
}

func TestProcessSingleSerializesCalls(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			n := inFlight.Add(1)
			for {
				old := maxInFlight.Load()
				if n <= old || maxInFlight.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			inFlight.Add(-1)
			fp.result(f.ID, value.NewNull())
		}
	})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := callEcho(t, p, value.NewNull()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if maxInFlight.Load() != 1 {
		t.Fatalf("single mode allows one call in flight, saw %d", maxInFlight.Load())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run Process -count=1`
Expected: FAIL — `undefined: newProcess`, `undefined: procConn`.

- [ ] **Step 3: Implement `process.go`** (the timeout/death/close paths referenced here are completed in Tasks 6–7; write the whole file now so the package compiles, including `expire`, `die` and `Close`)

```go
// internal/ext/process.go
package ext

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"noxy-vm/internal/value"
)

// Carencias do host (spec §2.7, §4.3). Variaveis para os testes encurtarem.
var (
	cancelGrace   = time.Second
	shutdownGrace = 2 * time.Second
)

// procConn e o que um plugin em execucao parece ao host: os dois pipes e
// wait/kill. execConn (process_spawn.go) e o real; os testes usam io.Pipe.
type procConn interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Wait() error
	Kill() error
}

type spawnFunc func(ctx context.Context) (procConn, error)

type ProcessConfig struct {
	// Path e o binario a executar, absoluto (spec §2.1).
	Path        string
	NoxyVersion string
	Log         io.Writer // destino dos LOG; nil → os.Stderr
	Limits      Limits    // zero → DefaultLimits()
}

// reply e o que uma chamada em voo recebe: RESULT (body), ERROR (failed +
// msg) ou a morte do processo (err).
type reply struct {
	body   []byte
	failed bool
	msg    string
	err    error
}

// hostKill e a causa de uma morte decidida pelo host (timeout sem cancel).
type hostKill struct{ reason string }

func (e *hostKill) Error() string { return e.reason }

// Process e o backend de kind = "process" (spec 2026-08-29): um processo por
// extensao, multiplexado por id, subido na primeira chamada.
type Process struct {
	Manifest *Manifest

	cfg     ProcessConfig
	logOut  io.Writer
	limits  Limits
	exports []string
	spawn   spawnFunc

	// callMu serializa a troca CALL/resposta em concurrency = "single"
	// (spec §5) — nao o start.
	callMu sync.Mutex

	// mu guarda o estado abaixo; writeMu serializa escritas no stdin do
	// plugin (um quadro inteiro por vez).
	mu       sync.Mutex
	writeMu  sync.Mutex
	conn     procConn
	alive    bool
	poisoned bool
	closed   bool
	pending  map[uint32]chan reply
	nextID   uint32
	deathErr error
	// dying e a primeira causa registrada para a conexao atual: o kill do
	// host (timeout sem cancel, violacao) precede o EOF que ele mesmo
	// provoca no leitor, e e a causa que deve aparecer no erro.
	dying error
}

var _ Backend = (*Process)(nil)

func NewProcess(manifest *Manifest, cfg ProcessConfig) *Process {
	return newProcess(manifest, cfg, execSpawner(cfg.Path))
}

func newProcess(manifest *Manifest, cfg ProcessConfig, spawn spawnFunc) *Process {
	logOut := cfg.Log
	if logOut == nil {
		logOut = os.Stderr
	}
	limits := cfg.Limits
	if limits.MaxBytes == 0 {
		limits = DefaultLimits()
	}
	exports := make([]string, len(manifest.Exports))
	for i, exp := range manifest.Exports {
		exports[i] = exp.Name
	}
	return &Process{
		Manifest: manifest,
		cfg:      cfg,
		logOut:   logOut,
		limits:   limits,
		exports:  exports,
		spawn:    spawn,
		pending:  map[uint32]chan reply{},
	}
}

// ensureStarted sobe o processo e faz o handshake na primeira chamada
// (spec §4.1, §4.2). Chamadas concorrentes esperam o mesmo start sob mu.
func (p *Process) ensureStarted(ctx context.Context) error {
	name := p.Manifest.Name
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return fmt.Errorf("extension '%s' was closed at exit", name)
	}
	if p.alive {
		return nil
	}
	if p.poisoned {
		return fmt.Errorf("extension '%s' is poisoned by an earlier trap", name)
	}
	conn, err := p.spawn(ctx)
	if err != nil {
		p.poisoned = !p.Manifest.Restart
		return fmt.Errorf("extension '%s' trapped: start: %v", name, err)
	}
	reader := bufio.NewReaderSize(conn.Stdout(), 64<<10)
	if err := p.handshake(conn, reader); err != nil {
		_ = conn.Kill()
		_ = conn.Wait()
		p.poisoned = !p.Manifest.Restart
		return fmt.Errorf("extension '%s' trapped: handshake: %v", name, err)
	}
	p.conn = conn
	p.alive = true
	p.deathErr = nil
	go p.readLoop(conn, reader)
	return nil
}

func (p *Process) handshake(conn procConn, reader *bufio.Reader) error {
	body, err := helloBody(p.cfg.NoxyVersion, p.Manifest.Name, p.exports, p.limits)
	if err != nil {
		return err
	}
	if err := WriteFrame(conn.Stdin(), Frame{Kind: FrameHello, Body: body}); err != nil {
		return fmt.Errorf("write HELLO: %v", err)
	}
	type outcome struct {
		frame Frame
		err   error
	}
	replies := make(chan outcome, 1)
	go func() {
		for {
			f, err := ReadFrame(reader, p.limits.MaxBytes)
			if err != nil {
				replies <- outcome{err: err}
				return
			}
			// LOG antes do HELLO do plugin e permitido (spec §2.4).
			if f.Kind == FrameLog {
				if err := p.printLog(f); err != nil {
					replies <- outcome{err: err}
					return
				}
				continue
			}
			replies <- outcome{frame: f}
			return
		}
	}()
	var deadline <-chan time.Time
	if d := p.Manifest.HandshakeTimeout(); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case out := <-replies:
		if out.err != nil {
			if errors.Is(out.err, io.EOF) || errors.Is(out.err, io.ErrUnexpectedEOF) {
				return fmt.Errorf("process exited before replying")
			}
			return out.err
		}
		return p.checkHello(out.frame)
	case <-deadline:
		// ensureStarted mata o processo; o leitor acima ve EOF e termina.
		return fmt.Errorf("no reply within %v", p.Manifest.HandshakeTimeout())
	}
}

func (p *Process) checkHello(f Frame) error {
	switch f.Kind {
	case FrameError:
		msg, err := p.errorMessage(f)
		if err != nil {
			return fmt.Errorf("plugin refused with a malformed ERROR: %v", err)
		}
		return fmt.Errorf("plugin refused: %s", msg)
	case FrameHello:
	default:
		return fmt.Errorf("unexpected frame kind 0x%02x before HELLO", f.Kind)
	}
	if f.ID != 0 {
		return fmt.Errorf("HELLO carries call id %d", f.ID)
	}
	m, err := decodeBodyMap(f.Body, p.limits)
	if err != nil {
		return fmt.Errorf("HELLO body: %v", err)
	}
	proto, ok := mapString(m, "protocol")
	if !ok {
		return fmt.Errorf("HELLO without a protocol field")
	}
	if proto != ProtocolVersion {
		return fmt.Errorf("plugin speaks %q, host speaks %q", proto, ProtocolVersion)
	}
	return nil
}

func (p *Process) errorMessage(f Frame) (string, error) {
	m, err := decodeBodyMap(f.Body, p.limits)
	if err != nil {
		return "", err
	}
	msg, ok := mapString(m, "message")
	if !ok {
		return "", fmt.Errorf("missing message")
	}
	return msg, nil
}

func (p *Process) printLog(f Frame) error {
	m, err := decodeBodyMap(f.Body, p.limits)
	if err != nil {
		return &ProtocolError{Detail: "LOG body: " + err.Error()}
	}
	msg, ok := mapString(m, "message")
	if !ok {
		return &ProtocolError{Detail: "LOG without a message field"}
	}
	fmt.Fprintf(p.logOut, "[ext %s] %s\n", p.Manifest.Name, msg)
	return nil
}

// readLoop demultiplexa as respostas por id (spec §5). Qualquer erro de
// leitura ou quadro fora de lugar encerra o processo (§4.4, §6).
func (p *Process) readLoop(conn procConn, reader *bufio.Reader) {
	for {
		f, err := ReadFrame(reader, p.limits.MaxBytes)
		if err != nil {
			p.die(conn, err)
			return
		}
		switch f.Kind {
		case FrameLog:
			if err := p.printLog(f); err != nil {
				p.die(conn, err)
				return
			}
		case FrameResult, FrameError:
			if err := p.deliver(f); err != nil {
				p.die(conn, err)
				return
			}
		default:
			p.die(conn, &ProtocolError{Detail: fmt.Sprintf("unexpected frame kind 0x%02x from the plugin", f.Kind)})
			return
		}
	}
}

func (p *Process) deliver(f Frame) error {
	r := reply{}
	switch f.Kind {
	case FrameResult:
		r.body = f.Body
	case FrameError:
		msg, err := p.errorMessage(f)
		if err != nil {
			return &ProtocolError{Detail: fmt.Sprintf("ERROR frame for call %d: %v", f.ID, err)}
		}
		r.failed, r.msg = true, msg
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ch, ok := p.pending[f.ID]
	if !ok {
		return &ProtocolError{Detail: fmt.Sprintf("reply for unknown call id %d", f.ID)}
	}
	delete(p.pending, f.ID)
	ch <- r
	return nil
}

// register reserva um id em voo. Devolve a conexao para que o chamador
// escreva na mesma conexao que registrou — apos a morte, conn e nil.
func (p *Process) register() (uint32, chan reply, procConn, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.alive {
		if p.deathErr != nil {
			return 0, nil, nil, p.deathErr
		}
		return 0, nil, nil, fmt.Errorf("extension '%s' is not running", p.Manifest.Name)
	}
	for {
		p.nextID++
		if p.nextID == 0 {
			continue
		}
		if _, busy := p.pending[p.nextID]; !busy {
			break
		}
	}
	ch := make(chan reply, 1)
	p.pending[p.nextID] = ch
	return p.nextID, ch, p.conn, nil
}

func (p *Process) writeFrame(conn procConn, f Frame) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return WriteFrame(conn.Stdin(), f)
}

func (p *Process) Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error) {
	name := p.Manifest.Name
	if fnIndex < 0 || fnIndex >= len(p.exports) {
		return value.NewNull(), fmt.Errorf("extension '%s': export index %d out of range", name, fnIndex)
	}
	encoded, err := EncodeArgs(args, p.limits)
	if err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	if p.Manifest.Concurrency == "single" {
		p.callMu.Lock()
		defer p.callMu.Unlock()
	}
	if err := p.ensureStarted(ctx); err != nil {
		return value.NewNull(), err
	}
	id, ch, conn, err := p.register()
	if err != nil {
		return value.NewNull(), err
	}
	if err := p.writeFrame(conn, Frame{Kind: FrameCall, ID: id, Fn: uint32(fnIndex), Body: encoded}); err != nil {
		p.die(conn, err)
		return value.NewNull(), (<-ch).err
	}
	var deadline <-chan time.Time
	if d := p.Manifest.CallTimeout(fnIndex); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		deadline = timer.C
	}
	select {
	case r := <-ch:
		return p.finish(conn, r, fnIndex)
	case <-deadline:
		return p.expire(conn, id, ch, fnIndex)
	}
}

func (p *Process) finish(conn procConn, r reply, fnIndex int) (value.Value, error) {
	name := p.Manifest.Name
	if r.err != nil {
		return value.NewNull(), r.err
	}
	if r.failed {
		return value.NewNull(), fmt.Errorf("extension '%s' failed: %s", name, r.msg)
	}
	result, err := DecodeValue(r.body, p.limits)
	if err != nil {
		// NXB invalido no RESULT e violacao de protocolo (spec §6): o fluxo
		// nao e mais confiavel.
		p.die(conn, &ProtocolError{Detail: "RESULT body: " + err.Error()})
		return value.NewNull(), p.currentDeathErr()
	}
	if err := checkDeclaredReturn(result, p.Manifest.Exports[fnIndex].Returns); err != nil {
		return value.NewNull(), fmt.Errorf("extension '%s': %w", name, err)
	}
	return result, nil
}

func (p *Process) currentDeathErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.deathErr != nil {
		return p.deathErr
	}
	return fmt.Errorf("extension '%s' trapped: process exited", p.Manifest.Name)
}

// expire trata o prazo vencido (spec §4.3): pede CANCEL, devolve "timed out"
// e, se o plugin nao responder na carencia, mata o processo. Em "single" o
// chamador espera a resposta ao CANCEL (o mutex fica com ele ate la); nos
// outros modos a espera vai para uma goroutine.
func (p *Process) expire(conn procConn, id uint32, ch chan reply, fnIndex int) (value.Value, error) {
	name := p.Manifest.Name
	export := p.exports[fnIndex]
	limit := p.Manifest.CallTimeout(fnIndex).Milliseconds()
	timedOut := fmt.Errorf("extension '%s' timed out: %s exceeded %d ms", name, export, limit)
	if err := p.writeFrame(conn, Frame{Kind: FrameCancel, ID: id}); err != nil {
		p.die(conn, err)
		return value.NewNull(), (<-ch).err
	}
	await := func() error {
		select {
		case r := <-ch:
			return r.err // nil: cancel honrado, resposta descartada
		case <-time.After(cancelGrace):
			p.die(conn, &hostKill{reason: fmt.Sprintf("%s exceeded %d ms and did not cancel; process killed", export, limit)})
			return (<-ch).err
		}
	}
	if p.Manifest.Concurrency == "single" {
		if err := await(); err != nil {
			return value.NewNull(), err
		}
		return value.NewNull(), timedOut
	}
	go await()
	return value.NewNull(), timedOut
}

// die encerra a conexao (mata, espera) e entrega a causa a toda chamada em
// voo (spec §4.4). A PRIMEIRA causa registrada vence: expire chama die com
// hostKill e so depois o leitor ve o EOF do kill — as duas chamadas
// convergem, e a segunda a chegar ao fim ve p.conn != conn e volta.
func (p *Process) die(conn procConn, cause error) {
	p.mu.Lock()
	if p.conn != conn {
		p.mu.Unlock()
		return
	}
	if p.dying == nil {
		p.dying = cause
	}
	p.mu.Unlock()

	_ = conn.Kill()
	waitErr := conn.Wait()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.conn != conn {
		return
	}
	p.alive = false
	p.conn = nil
	p.poisoned = !p.Manifest.Restart
	p.deathErr = p.deathError(p.dying, waitErr)
	p.dying = nil
	for id, ch := range p.pending {
		delete(p.pending, id)
		ch <- reply{err: p.deathErr}
	}
}

func (p *Process) deathError(cause, waitErr error) error {
	name := p.Manifest.Name
	var violation *ProtocolError
	var kill *hostKill
	switch {
	case errors.As(cause, &violation):
		return fmt.Errorf("extension '%s' trapped: %v", name, violation)
	case errors.As(cause, &kill):
		return fmt.Errorf("extension '%s' trapped: %s", name, kill.reason)
	default:
		return fmt.Errorf("extension '%s' trapped: process exited (%s)", name, exitStatus(waitErr))
	}
}

func exitStatus(waitErr error) string {
	var exitErr *exec.ExitError
	switch {
	case errors.As(waitErr, &exitErr):
		return fmt.Sprintf("status %d", exitErr.ExitCode())
	case waitErr != nil:
		return waitErr.Error()
	default:
		return "status 0"
	}
}

// Close fecha o stdin do plugin (EOF = shutdown, spec §2.7), espera a
// carencia e mata o que sobrar. Chamadas depois de Close nao sobem processo.
func (p *Process) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	conn, alive := p.conn, p.alive
	p.mu.Unlock()
	if !alive || conn == nil {
		return nil
	}
	_ = conn.Stdin().Close()
	done := make(chan error, 1)
	go func() { done <- conn.Wait() }()
	select {
	case <-done:
	case <-time.After(shutdownGrace):
		_ = conn.Kill()
		<-done
	}
	return nil
}
```

- [ ] **Step 4: Run the Task 5 tests**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run Process -count=1 -race`
Expected: PASS (all nine tests, race-clean).

- [ ] **Step 5: Commit**

```bash
git add internal/ext/process.go internal/ext/process_test.go
git commit -m "feat(ext): backend Process — start lazy, handshake, leitor multiplexado, chamadas e LOG (issue #80)"
```

---

### Task 6: Timeouts and cooperative CANCEL

**Files:**
- Modify: `internal/ext/process.go` (already contains `expire`; this task proves it)
- Test: `internal/ext/process_test.go` (append)

**Interfaces:**
- Consumes: `expire`, `cancelGrace`, `hostKill` from Task 5.
- Produces: verified behaviours — `timed out` without poison when CANCEL is honoured; `trapped: <export> exceeded N ms and did not cancel; process killed` + poison otherwise; other in-flight calls unaffected in `concurrent`.

- [ ] **Step 1: Write the failing tests**

```go
func shortGraces(t *testing.T) {
	t.Helper()
	prevCancel, prevShutdown := cancelGrace, shutdownGrace
	cancelGrace, shutdownGrace = 100*time.Millisecond, 100*time.Millisecond
	t.Cleanup(func() { cancelGrace, shutdownGrace = prevCancel, prevShutdown })
}

func TestProcessTimeoutCancelHonoured(t *testing.T) {
	shortGraces(t)
	cancelled := make(chan uint32, 1)
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		stop := make(map[uint32]chan struct{})
		var mu sync.Mutex
		fp.handle = func(fp *fakePlugin, f Frame) {
			if f.Fn != 2 {
				fp.result(f.ID, value.NewNull())
				return
			}
			ch := make(chan struct{})
			mu.Lock()
			stop[f.ID] = ch
			mu.Unlock()
			<-ch // espera o CANCEL
			fp.fail(f.ID, "cancelled")
		}
		fp.onCancel = func(fp *fakePlugin, id uint32) {
			mu.Lock()
			ch := stop[id]
			mu.Unlock()
			close(ch)
			cancelled <- id
		}
	})
	_, err := p.Call(context.Background(), 2, nil) // guest_slow: timeout_ms = 50
	if err == nil || err.Error() != "extension 'guest' timed out: guest_slow exceeded 50 ms" {
		t.Fatalf("got %v", err)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("host must send CANCEL on expiry")
	}
	if _, err := callEcho(t, p, value.NewInt(1)); err != nil {
		t.Fatalf("a cancelled call does not poison: %v", err)
	}
}

func TestProcessTimeoutCancelIgnoredKillsAndPoisons(t *testing.T) {
	shortGraces(t)
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { /* nunca responde */ }
	})
	_, err := p.Call(context.Background(), 2, nil)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' trapped: guest_slow exceeded 50 ms and did not cancel; process killed") {
		t.Fatalf("got %v", err)
	}
	if !h.plugin().conn.killed.Load() {
		t.Fatal("plugin must be killed after the cancel grace")
	}
	_, err = callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "is poisoned by an earlier trap") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessTimeoutConcurrentDoesNotBlockOthers(t *testing.T) {
	shortGraces(t)
	p, _ := newFakeProcess(t, "concurrent", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			if f.Fn == 2 {
				return // guest_slow nunca responde; o cancel tambem nao
			}
			fp.result(f.ID, value.NewInt(7))
		}
	})
	done := make(chan error, 1)
	go func() {
		_, err := p.Call(context.Background(), 2, nil)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	got, err := callEcho(t, p, value.NewNull())
	if err != nil || got.Int() != 7 {
		t.Fatalf("other call must complete while guest_slow hangs: %#v %v", got, err)
	}
	err = <-done
	if err == nil || !strings.Contains(err.Error(), "timed out: guest_slow exceeded 50 ms") {
		t.Fatalf("caller gets timed out immediately in concurrent mode, got %v", err)
	}
	// depois da carencia sem resposta ao CANCEL, o processo cai e a proxima
	// chamada ve o poison
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, err := callEcho(t, p, value.NewNull()); err != nil && strings.Contains(err.Error(), "poisoned") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("process must be killed and poisoned after the cancel grace")
}
```

- [ ] **Step 2: Run to verify** (these may already pass — the point of the task is the proof)

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'ProcessTimeout' -count=1 -race`
Expected: PASS. If `TestProcessTimeoutCancelIgnoredKillsAndPoisons` ever reports "process exited (killed)" instead of the `did not cancel` text, the first-cause rule of `die` (`p.dying`) is broken: `expire` records the `hostKill` cause under `mu` *before* killing, so the reader's later EOF cause is ignored. Fix `die`, never the expected text.

- [ ] **Step 3: Commit**

```bash
git add internal/ext/process_test.go
git commit -m "test(ext): prazos por chamada e CANCEL cooperativo do backend Process (issue #80)"
```

---

### Task 7: Death, poisoning, restart, Close, protocol violations

**Files:**
- Test: `internal/ext/process_test.go` (append)
- Modify: `internal/ext/process.go` only if a test below exposes a gap

- [ ] **Step 1: Write the failing tests**

```go
func TestProcessExitFailsInFlightAndPoisons(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.conn.exit(errors.New("status 3")) }
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || err.Error() != "extension 'guest' trapped: process exited (status 3)" {
		t.Fatalf("got %v", err)
	}
	_, err = callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' is poisoned by an earlier trap") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessRestartOnlyWithStateless(t *testing.T) {
	p, h := newFakeProcess(t, "stateless", "restart = true", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			args, _ := DecodeArgs(f.Body, DefaultLimits())
			if args[0].Int() == 0 {
				fp.conn.exit(nil)
				return
			}
			fp.result(f.ID, args[0])
		}
	})
	if _, err := callEcho(t, p, value.NewInt(0)); err == nil || !strings.Contains(err.Error(), "process exited (status 0)") {
		t.Fatalf("got %v", err)
	}
	got, err := callEcho(t, p, value.NewInt(5))
	if err != nil || got.Int() != 5 {
		t.Fatalf("restart must respawn on the next call: %#v %v", got, err)
	}
	if h.spawns != 2 {
		t.Fatalf("expected a second spawn, got %d", h.spawns)
	}
}

func TestProcessStartFailureIsTrap(t *testing.T) {
	m, _ := ParseManifest([]byte(fmt.Sprintf(processTestManifest, "single", "")))
	p := newProcess(m, ProcessConfig{}, func(context.Context) (procConn, error) {
		return nil, errors.New("exec format error")
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || err.Error() != "extension 'guest' trapped: start: exec format error" {
		t.Fatalf("got %v", err)
	}
	if _, err := callEcho(t, p, value.NewNull()); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("start failure poisons without restart: %v", err)
	}
}

func TestProcessUnknownReplyIdIsViolation(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.result(99, value.NewNull()) }
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: protocol violation: reply for unknown call id 99") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessMalformedFrameIsViolation(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) {
			fp.writeMu.Lock()
			_, _ = fp.conn.stdoutW.Write([]byte{0x02, 0, 0, 0, 0xff, 0xff})
			fp.writeMu.Unlock()
		}
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: protocol violation: frame length 2 below header size") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessMalformedResultIsViolation(t *testing.T) {
	p, _ := newFakeProcess(t, "single", "", func(fp *fakePlugin) {
		fp.handle = func(fp *fakePlugin, f Frame) { fp.send(nil, Frame{Kind: FrameResult, ID: f.ID, Body: []byte{0x77}}) }
	})
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "trapped: protocol violation: RESULT body") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessCloseSendsEOFAndRefusesLaterCalls(t *testing.T) {
	shortGraces(t)
	p, h := newFakeProcess(t, "single", "", nil)
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	conn := h.plugin().conn
	select {
	case <-conn.exited:
	default:
		t.Fatal("plugin must have exited on EOF before Close returns")
	}
	if conn.killed.Load() {
		t.Fatal("a plugin that honours EOF must not be killed")
	}
	_, err := callEcho(t, p, value.NewNull())
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' was closed at exit") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessCloseKillsAfterGrace(t *testing.T) {
	shortGraces(t)
	p, h := newFakeProcess(t, "single", "", func(fp *fakePlugin) { fp.ignoreEOF = true })
	if _, err := callEcho(t, p, value.NewNull()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_ = p.Close(context.Background())
	if !h.plugin().conn.killed.Load() {
		t.Fatal("a plugin that ignores EOF must be killed after the shutdown grace")
	}
	if time.Since(start) > time.Second {
		t.Fatal("Close must not wait beyond the grace")
	}
}
```

- [ ] **Step 2: Run to verify**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'ProcessExit|ProcessRestart|ProcessStart|ProcessUnknown|ProcessMalformed|ProcessClose' -count=1 -race`
Expected: PASS. Fix `process.go` inline if any assertion fails; the expected texts are the spec's (§6).

- [ ] **Step 3: Commit**

```bash
git add internal/ext/process.go internal/ext/process_test.go
git commit -m "test(ext): morte, poison, restart, Close e violacoes de protocolo do backend Process (issue #80)"
```

---

### Task 8: Real spawner and platform death guards

**Files:**
- Create: `internal/ext/process_spawn.go`, `internal/ext/process_spawn_linux.go`, `internal/ext/process_spawn_windows.go`, `internal/ext/process_spawn_other.go`
- Test: `internal/ext/process_exec_test.go` (a `TestMain` helper mode turns the test binary itself into a minimal plugin, so this task needs no SDK)

**Interfaces:**
- Produces: `func execSpawner(path string) spawnFunc` — `exec.Command(path)` with no arguments, inherited env and cwd, `Stderr = os.Stderr`, stdin/stdout pipes, platform guard; `type execConn struct` with memoized `Wait`; `func applyDeathGuard(cmd *exec.Cmd)` (Linux: `Pdeathsig = SIGKILL`); `func attachJobObject(pid int) func()` (Windows: job object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`; returns the release func or nil).
- Consumes: `procConn`, `spawnFunc` (Task 5).

- [ ] **Step 1: Write the helper-mode `TestMain` and the failing tests**

```go
// internal/ext/process_exec_test.go
package ext

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"noxy-vm/internal/value"
)

// TestMain: com NOXY_EXT_HELPER=plugin o binario de teste vira um plugin
// minimo (spec §2) — assim o spawner real e testado sem toolchain extra.
// A Task 13 acrescenta o modo "host".
func TestMain(m *testing.M) {
	switch os.Getenv("NOXY_EXT_HELPER") {
	case "plugin":
		os.Exit(helperPlugin())
	case "host":
		os.Exit(helperHost())
	}
	os.Exit(m.Run())
}

func helperHost() int { return 0 } // substituido na Task 13

// helperPlugin: HELLO → HELLO; CALL fn 0 → echo; fn 1 → ERROR; EOF → sai 0.
func helperPlugin() int {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout
	limits := DefaultLimits()
	f, err := ReadFrame(in, limits.MaxBytes)
	if err != nil || f.Kind != FrameHello {
		return 2
	}
	body, _ := encodeStringMap(map[string]value.Value{"protocol": value.NewString(ProtocolVersion), "sdk": value.NewString("helper/0")}, limits)
	if err := WriteFrame(out, Frame{Kind: FrameHello, Body: body}); err != nil {
		return 2
	}
	for {
		f, err := ReadFrame(in, limits.MaxBytes)
		if err != nil {
			return 0
		}
		if f.Kind != FrameCall {
			continue
		}
		switch f.Fn {
		case 1:
			msg, _ := encodeStringMap(map[string]value.Value{"message": value.NewString("helper says no")}, limits)
			_ = WriteFrame(out, Frame{Kind: FrameError, ID: f.ID, Body: msg})
		default:
			args, _ := DecodeArgs(f.Body, limits)
			v := value.NewNull()
			if len(args) != 0 {
				v = args[0]
			}
			res, _ := EncodeValue(v, limits)
			_ = WriteFrame(out, Frame{Kind: FrameResult, ID: f.ID, Body: res})
		}
	}
}

// helperProcess sobe o proprio binario de teste como plugin via execSpawner.
func helperProcess(t *testing.T, concurrency string) *Process {
	t.Helper()
	m, err := ParseManifest([]byte(fmt.Sprintf(processTestManifest, concurrency, "")))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOXY_EXT_HELPER", "plugin")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := NewProcess(m, ProcessConfig{Path: self, NoxyVersion: "v0.23.0"})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p
}

func TestExecSpawnerEchoAndError(t *testing.T) {
	p := helperProcess(t, "single")
	got, err := p.Call(context.Background(), 0, []value.Value{value.NewString("hi")})
	if err != nil || got.Obj.(string) != "hi" {
		t.Fatalf("echo through a real process: %#v %v", got, err)
	}
	_, err = p.Call(context.Background(), 1, nil)
	if err == nil || err.Error() != "extension 'guest' failed: helper says no" {
		t.Fatalf("got %v", err)
	}
}

func TestExecSpawnerCloseExitsOnEOF(t *testing.T) {
	p := helperProcess(t, "single")
	if _, err := p.Call(context.Background(), 0, []value.Value{value.NewInt(1)}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > shutdownGrace {
		t.Fatal("a plugin that honours EOF exits before the grace")
	}
	_, err := p.Call(context.Background(), 0, nil)
	if err == nil || !strings.Contains(err.Error(), "was closed at exit") {
		t.Fatalf("got %v", err)
	}
}

func TestExecSpawnerMissingBinaryIsStartTrap(t *testing.T) {
	m, _ := ParseManifest([]byte(fmt.Sprintf(processTestManifest, "single", "")))
	p := NewProcess(m, ProcessConfig{Path: "/definitely/not/here/noxy-plugin-guest"})
	_, err := p.Call(context.Background(), 0, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "extension 'guest' trapped: start: ") {
		t.Fatalf("got %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run ExecSpawner -count=1`
Expected: FAIL — `undefined: execSpawner`.

- [ ] **Step 3: Implement the spawner and platform files**

```go
// internal/ext/process_spawn.go
package ext

import (
	"context"
	"io"
	"os"
	"os/exec"
	"sync"
)

// execConn e o processo real do plugin. Wait e memoizado: die (leitor),
// expire (timeout) e Close podem todos esperar a mesma saida.
type execConn struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	waitOnce sync.Once
	waitErr  error
	release  func()
}

// execSpawner executa o binario pelo caminho absoluto, sem argumentos, com
// o ambiente e o diretorio do host; stderr passa direto (spec §2.1).
func execSpawner(path string) spawnFunc {
	return func(ctx context.Context) (procConn, error) {
		cmd := exec.Command(path)
		cmd.Env = os.Environ()
		cmd.Stderr = os.Stderr
		applyDeathGuard(cmd)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		return &execConn{cmd: cmd, stdin: stdin, stdout: stdout, release: attachJobObject(cmd.Process.Pid)}, nil
	}
}

func (c *execConn) Stdin() io.WriteCloser { return c.stdin }
func (c *execConn) Stdout() io.Reader     { return c.stdout }

func (c *execConn) Wait() error {
	c.waitOnce.Do(func() {
		c.waitErr = c.cmd.Wait()
		if c.release != nil {
			c.release()
		}
	})
	return c.waitErr
}

func (c *execConn) Kill() error { return c.cmd.Process.Kill() }
```

```go
// internal/ext/process_spawn_linux.go
//go:build linux

package ext

import (
	"os/exec"
	"syscall"
)

// Pdeathsig: se o host morrer sem passar por Close, o kernel mata o filho
// (spec §4.5). A regra de EOF continua sendo a guarda principal.
func applyDeathGuard(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

func attachJobObject(int) func() { return nil }
```

```go
// internal/ext/process_spawn_windows.go
//go:build windows

package ext

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func applyDeathGuard(*exec.Cmd) {}

// attachJobObject poe o filho num job object com KILL_ON_JOB_CLOSE: o
// handle do job morre com o host e o kernel mata o filho (spec §4.5). Best
// effort — qualquer falha devolve nil e fica a regra de EOF.
func attachJobObject(pid int) func() {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	return func() { _ = windows.CloseHandle(job) }
}
```

```go
// internal/ext/process_spawn_other.go
//go:build !linux && !windows

package ext

import "os/exec"

// macOS e os demais nao tem sinal de morte do pai: vale a regra de EOF
// (spec §2.7, §4.5).
func applyDeathGuard(*exec.Cmd) {}

func attachJobObject(int) func() { return nil }
```

- [ ] **Step 4: Run the exec tests and cross-compile the three targets**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run ExecSpawner -count=1 -race`
Expected: PASS.
Run: `GOOS=linux GOARCH=amd64 go build ./internal/ext && GOOS=darwin GOARCH=arm64 go build ./internal/ext && GOOS=windows GOARCH=amd64 go build ./internal/ext`
Expected: all three build (each platform file compiles).

- [ ] **Step 5: Commit**

```bash
git add internal/ext/process_spawn.go internal/ext/process_spawn_linux.go internal/ext/process_spawn_windows.go internal/ext/process_spawn_other.go internal/ext/process_exec_test.go
git commit -m "feat(ext): spawner real do plugin com pdeathsig (linux) e job object (windows) (issue #80)"
```

---

## Part B — SDK (`sdk/noxyplugin`) and the test guest

### Task 9: Shared NXB golden vectors (host side)

**Files:**
- Create: `internal/ext/testdata/nxb/vectors.txt`
- Test: `internal/ext/nxb_golden_test.go`

**Interfaces:**
- Produces: the vector file, read by both codecs (§9.1). Format: one `name<TAB>hex` per line, `#` comments. Names: `null`, `bool_true`, `int_minus_two`, `float_one_point_five`, `string_ola`, `bytes_two`, `array_int_string`, `map_string`, `map_int`, `struct_point`.

- [ ] **Step 1: Write the vector file** (bytes derived by hand from §2 of the wasm spec / `nxb.go`: tag, LE scalars, u32 lengths; maps sorted ints-before-strings; struct = raw name blob + tagged field names)

```
# NXB v1 golden vectors — shared by internal/ext (host) and sdk/noxyplugin.
# name<TAB>hex. Values: null; true; -2; 1.5; "olá"; bytes 00 ff; [7, "a"];
# {"a": true, "b": 1}; {1: "y", 2: "x"}; Point{x: 1, y: 2.0}.
null	00
bool_true	0101
int_minus_two	02feffffffffffffff
float_one_point_five	03000000000000f83f
string_ola	04040000006f6cc3a1
bytes_two	050200000000ff
array_int_string	0602000000020700000000000000040100000061
map_string	07020000000401000000610101040100000062020100000000000000
map_int	0702000000020100000000000000040100000079020200000000000000040100000078
struct_point	0805000000506f696e7402000000040100000078020100000000000000040100000079030000000000000040
```

- [ ] **Step 2: Write the host-side golden test**

```go
// internal/ext/nxb_golden_test.go
package ext

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func loadNXBVectors(t testing.TB) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "nxb", "vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("malformed vector line %q", line)
		}
		raw, err := hex.DecodeString(fields[1])
		if err != nil {
			t.Fatalf("vector %s: %v", fields[0], err)
		}
		out[fields[0]] = raw
	}
	return out
}

func TestNXBGoldenVectors(t *testing.T) {
	vectors := loadNXBVectors(t)
	strMap := value.NewMap()
	strMap.Obj.(*value.ObjMap).Set("b", value.NewInt(1))
	strMap.Obj.(*value.ObjMap).Set("a", value.NewBool(true))
	intMap := value.NewMap()
	intMap.Obj.(*value.ObjMap).Set(int64(2), value.NewString("x"))
	intMap.Obj.(*value.ObjMap).Set(int64(1), value.NewString("y"))
	point := value.NewInstanceWith(&value.ObjStruct{Name: "Point", Fields: []string{"x", "y"}},
		map[string]value.Value{"x": value.NewInt(1), "y": value.NewFloat(2)})
	cases := []struct {
		name string
		v    value.Value
	}{
		{"null", value.NewNull()},
		{"bool_true", value.NewBool(true)},
		{"int_minus_two", value.NewInt(-2)},
		{"float_one_point_five", value.NewFloat(1.5)},
		{"string_ola", value.NewString("olá")},
		{"bytes_two", value.NewBytes("\x00\xff")},
		{"array_int_string", value.NewArray([]value.Value{value.NewInt(7), value.NewString("a")})},
		{"map_string", strMap},
		{"map_int", intMap},
		{"struct_point", point},
	}
	if len(cases) != len(vectors) {
		t.Fatalf("every vector needs a case: %d cases, %d vectors", len(cases), len(vectors))
	}
	for _, c := range cases {
		want, ok := vectors[c.name]
		if !ok {
			t.Fatalf("vector %s missing from vectors.txt", c.name)
		}
		got, err := EncodeValue(c.v, DefaultLimits())
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: encode\n got %x\nwant %x", c.name, got, want)
		}
		decoded, err := DecodeValue(want, DefaultLimits())
		if err != nil {
			t.Fatalf("%s: decode: %v", c.name, err)
		}
		if c.name == "struct_point" {
			// Struct volta como map (spec wasm §3): confere os campos.
			m := decoded.Obj.(*value.ObjMap)
			if x, _ := m.Get("x"); x.Int() != 1 {
				t.Fatalf("struct_point x: %#v", x)
			}
			if y, _ := m.Get("y"); y.Float() != 2 {
				t.Fatalf("struct_point y: %#v", y)
			}
			continue
		}
		again, err := EncodeValue(decoded, DefaultLimits())
		if err != nil || !bytes.Equal(again, want) {
			t.Fatalf("%s: decode/encode round trip\n got %x\nwant %x (%v)", c.name, again, want, err)
		}
	}
}
```

- [ ] **Step 3: Run**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run NXBGolden -count=1`
Expected: PASS. If a vector disagrees with the encoder, the **encoder is right** (it is the shipped M1 format) — fix the hex in `vectors.txt`, then re-check the byte-by-byte derivation in the comment of the file.

- [ ] **Step 4: Commit**

```bash
git add internal/ext/testdata/nxb/vectors.txt internal/ext/nxb_golden_test.go
git commit -m "test(ext): vetores dourados NXB v1 compartilhados com o SDK (issue #80)"
```

---

### Task 10: SDK module — frame and NXB codecs over Go types

**Files:**
- Create: `sdk/noxyplugin/go.mod`, `sdk/noxyplugin/frame.go`, `sdk/noxyplugin/nxb.go`
- Test: `sdk/noxyplugin/frame_test.go`, `sdk/noxyplugin/nxb_test.go`

**Interfaces:**
- Produces (package `noxyplugin`, module `github.com/estevaofon/noxy/sdk/noxyplugin`): `type Struct struct { Name string; Fields []Field }`, `type Field struct { Name string; Value any }`; unexported `frame`, `writeFrame`, `readFrame`, `kindHello…kindCancel`, `protocolVersion`; `encodeValue(buf []byte, v any, depth int) ([]byte, error)`, `encodeArgs(args []any) ([]byte, error)`, `decodeValue(data []byte) (any, error)`, `decodeArgs(data []byte) ([]any, error)`, `decodeStringMap(data []byte) (map[string]any, error)`.
- Go type mapping (§9.3): null↔`nil`, bool↔`bool`, int↔`int64` (any Go int kind encodes), float↔`float64`, string↔`string`, bytes↔`[]byte`, array↔`[]any` (any slice encodes), map↔`map[string]any` / `map[int64]any` / `map[any]any` (mixed), struct↔`Struct`.

- [ ] **Step 1: Create the module and write the failing tests**

`sdk/noxyplugin/go.mod`:

```
module github.com/estevaofon/noxy/sdk/noxyplugin

go 1.25
```

```go
// sdk/noxyplugin/frame_test.go
package noxyplugin

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFrameLayoutMatchesHost(t *testing.T) {
	var buf bytes.Buffer
	if err := writeFrame(&buf, frame{Kind: kindCall, ID: 7, Fn: 2, Body: []byte{0x00}}); err != nil {
		t.Fatal(err)
	}
	want := []byte{0x0d, 0, 0, 0, 0x02, 0, 0, 0, 7, 0, 0, 0, 2, 0, 0, 0, 0}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("layout:\n got % x\nwant % x", buf.Bytes(), want)
	}
	f, err := readFrame(&buf, 1<<20)
	if err != nil || f.Kind != kindCall || f.ID != 7 || f.Fn != 2 || len(f.Body) != 1 {
		t.Fatalf("round trip: %#v %v", f, err)
	}
}

func TestFrameReadErrors(t *testing.T) {
	if _, err := readFrame(bytes.NewReader(nil), 1<<20); !errors.Is(err, io.EOF) {
		t.Fatalf("clean EOF: %v", err)
	}
	if _, err := readFrame(bytes.NewReader([]byte{0x0b, 0, 0, 0}), 1<<20); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("length below header must be an error, got %v", err)
	}
	if _, err := readFrame(bytes.NewReader([]byte{0x0c, 0, 0, 0, 0x09, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}), 1<<20); err == nil {
		t.Fatal("unknown kind must be an error")
	}
}
```

```go
// sdk/noxyplugin/nxb_test.go
package noxyplugin

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadVectors(t *testing.T) map[string][]byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "ext", "testdata", "nxb", "vectors.txt"))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		raw, err := hex.DecodeString(fields[1])
		if err != nil {
			t.Fatal(err)
		}
		out[fields[0]] = raw
	}
	return out
}

func TestNXBGoldenVectorsSDK(t *testing.T) {
	vectors := loadVectors(t)
	cases := []struct {
		name string
		v    any
	}{
		{"null", nil},
		{"bool_true", true},
		{"int_minus_two", int64(-2)},
		{"float_one_point_five", 1.5},
		{"string_ola", "olá"},
		{"bytes_two", []byte{0x00, 0xff}},
		{"array_int_string", []any{int64(7), "a"}},
		{"map_string", map[string]any{"b": int64(1), "a": true}},
		{"map_int", map[int64]any{2: "x", 1: "y"}},
		{"struct_point", Struct{Name: "Point", Fields: []Field{Field{"x", int64(1)}, Field{"y", 2.0}}}},
	}
	if len(cases) != len(vectors) {
		t.Fatalf("%d cases for %d vectors", len(cases), len(vectors))
	}
	for _, c := range cases {
		want := vectors[c.name]
		got, err := encodeValue(nil, c.v, 0)
		if err != nil {
			t.Fatalf("%s: encode: %v", c.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: encode\n got %x\nwant %x", c.name, got, want)
		}
		decoded, err := decodeValue(want)
		if err != nil {
			t.Fatalf("%s: decode: %v", c.name, err)
		}
		if !reflect.DeepEqual(decoded, c.v) {
			t.Fatalf("%s: decode\n got %#v\nwant %#v", c.name, decoded, c.v)
		}
	}
}

func TestNXBGoTypesEncode(t *testing.T) {
	vectors := loadVectors(t)
	for _, v := range []any{int(-2), int32(-2), int16(-2), int8(-2)} {
		got, err := encodeValue(nil, v, 0)
		if err != nil || !bytes.Equal(got, vectors["int_minus_two"]) {
			t.Fatalf("%T must encode as int: %x %v", v, got, err)
		}
	}
	got, _ := encodeValue(nil, []string{"a"}, 0)
	want, _ := encodeValue(nil, []any{"a"}, 0)
	if !bytes.Equal(got, want) {
		t.Fatal("typed slices encode as arrays")
	}
	got, _ = encodeValue(nil, map[string]int{"b": 1}, 0)
	want, _ = encodeValue(nil, map[string]any{"b": int64(1)}, 0)
	if !bytes.Equal(got, want) {
		t.Fatal("typed maps encode as maps")
	}
	if _, err := encodeValue(nil, uint64(1<<63), 0); err == nil {
		t.Fatal("uint64 above MaxInt64 cannot cross")
	}
	if _, err := encodeValue(nil, make(chan int), 0); err == nil {
		t.Fatal("a channel cannot cross")
	}
	if _, err := encodeValue(nil, map[bool]any{true: 1}, 0); err == nil {
		t.Fatal("map keys must be strings or ints")
	}
}

func TestNXBArgsAndStringMap(t *testing.T) {
	data, err := encodeArgs([]any{int64(1), "x"})
	if err != nil {
		t.Fatal(err)
	}
	args, err := decodeArgs(data)
	if err != nil || len(args) != 2 || args[0] != int64(1) || args[1] != "x" {
		t.Fatalf("args: %#v %v", args, err)
	}
	if _, err := decodeArgs(append(data, 0x00)); err == nil {
		t.Fatal("trailing bytes after the arguments are an error")
	}
	body, _ := encodeValue(nil, map[string]any{"protocol": "noxy-plugin/1", "exports": []any{"a"}}, 0)
	m, err := decodeStringMap(body)
	if err != nil || m["protocol"] != "noxy-plugin/1" {
		t.Fatalf("string map: %#v %v", m, err)
	}
	intBody, _ := encodeValue(nil, int64(3), 0)
	if _, err := decodeStringMap(intBody); err == nil {
		t.Fatal("an int is not a string map")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run (from `sdk/noxyplugin`): `go test ./... -count=1`
Expected: FAIL — build errors (undefined `frame`, `encodeValue`).

- [ ] **Step 3: Implement `frame.go`**

```go
// sdk/noxyplugin/frame.go
package noxyplugin

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Espelho do codec de quadros do host (noxy-plugin/1, spec §2.2): u32
// length | kind | flags | reserved u16 | id u32 | fn u32 | corpo NXB.
const (
	protocolVersion = "noxy-plugin/1"
	headerSize      = 12

	kindHello  byte = 0x01
	kindCall   byte = 0x02
	kindResult byte = 0x03
	kindError  byte = 0x04
	kindLog    byte = 0x05
	kindCancel byte = 0x06
)

type frame struct {
	Kind byte
	ID   uint32
	Fn   uint32
	Body []byte
}

func writeFrame(w io.Writer, f frame) error {
	length := headerSize + len(f.Body)
	buf := make([]byte, 4+headerSize, 4+length)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(length))
	buf[4] = f.Kind
	binary.LittleEndian.PutUint32(buf[8:12], f.ID)
	binary.LittleEndian.PutUint32(buf[12:16], f.Fn)
	buf = append(buf, f.Body...)
	_, err := w.Write(buf)
	return err
}

func readFrame(r io.Reader, maxBody int) (frame, error) {
	var head [4 + headerSize]byte
	if _, err := io.ReadFull(r, head[:4]); err != nil {
		return frame{}, err
	}
	length := binary.LittleEndian.Uint32(head[:4])
	if length < headerSize {
		return frame{}, fmt.Errorf("protocol violation: frame length %d below header size", length)
	}
	if uint64(length)-headerSize > uint64(maxBody) {
		return frame{}, fmt.Errorf("protocol violation: frame body exceeds %d bytes", maxBody)
	}
	if _, err := io.ReadFull(r, head[4:]); err != nil {
		return frame{}, truncated(err)
	}
	kind := head[4]
	if kind < kindHello || kind > kindCancel {
		return frame{}, fmt.Errorf("protocol violation: unknown frame kind 0x%02x", kind)
	}
	if head[5] != 0 || head[6] != 0 || head[7] != 0 {
		return frame{}, errors.New("protocol violation: non-zero flags/reserved bits")
	}
	f := frame{
		Kind: kind,
		ID:   binary.LittleEndian.Uint32(head[8:12]),
		Fn:   binary.LittleEndian.Uint32(head[12:16]),
		Body: make([]byte, int(length)-headerSize),
	}
	if _, err := io.ReadFull(r, f.Body); err != nil {
		return frame{}, truncated(err)
	}
	return f, nil
}

func truncated(err error) error {
	if errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
```

- [ ] **Step 4: Implement `nxb.go`**

```go
// sdk/noxyplugin/nxb.go
package noxyplugin

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
	"sort"
)

// NXB v1 sobre tipos Go (spec wasm §2; vetores dourados em
// internal/ext/testdata/nxb). Mapeamento na spec 2026-08-29 §9.3.
const (
	tagNull   = 0x00
	tagBool   = 0x01
	tagInt    = 0x02
	tagFloat  = 0x03
	tagString = 0x04
	tagBytes  = 0x05
	tagArray  = 0x06
	tagMap    = 0x07
	tagStruct = 0x08

	maxDepth = 64
)

// Struct e um struct Noxy cruzando a fronteira: nome e campos na ordem de
// declaracao. Na volta ao host vira um map com forma de struct.
type Struct struct {
	Name   string
	Fields []Field
}

type Field struct {
	Name  string
	Value any
}

func appendU32(buf []byte, v uint32) []byte { return binary.LittleEndian.AppendUint32(buf, v) }

func appendInt(buf []byte, v int64) []byte {
	return binary.LittleEndian.AppendUint64(append(buf, tagInt), uint64(v))
}

func appendFloat(buf []byte, v float64) []byte {
	return binary.LittleEndian.AppendUint64(append(buf, tagFloat), math.Float64bits(v))
}

func appendBlob(buf []byte, tag byte, data []byte) []byte {
	buf = appendU32(append(buf, tag), uint32(len(data)))
	return append(buf, data...)
}

func encodeValue(buf []byte, v any, depth int) ([]byte, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("nxb: value nesting exceeds depth %d", maxDepth)
	}
	switch x := v.(type) {
	case nil:
		return append(buf, tagNull), nil
	case bool:
		b := byte(0)
		if x {
			b = 1
		}
		return append(buf, tagBool, b), nil
	case int:
		return appendInt(buf, int64(x)), nil
	case int8:
		return appendInt(buf, int64(x)), nil
	case int16:
		return appendInt(buf, int64(x)), nil
	case int32:
		return appendInt(buf, int64(x)), nil
	case int64:
		return appendInt(buf, x), nil
	case uint8:
		return appendInt(buf, int64(x)), nil
	case uint16:
		return appendInt(buf, int64(x)), nil
	case uint32:
		return appendInt(buf, int64(x)), nil
	case uint:
		if uint64(x) > math.MaxInt64 {
			return nil, fmt.Errorf("nxb: %d does not fit in an int", x)
		}
		return appendInt(buf, int64(x)), nil
	case uint64:
		if x > math.MaxInt64 {
			return nil, fmt.Errorf("nxb: %d does not fit in an int", x)
		}
		return appendInt(buf, int64(x)), nil
	case float32:
		return appendFloat(buf, float64(x)), nil
	case float64:
		return appendFloat(buf, x), nil
	case string:
		return appendBlob(buf, tagString, []byte(x)), nil
	case []byte:
		return appendBlob(buf, tagBytes, x), nil
	case Struct:
		return encodeStruct(buf, x, depth)
	case *Struct:
		if x == nil {
			return append(buf, tagNull), nil
		}
		return encodeStruct(buf, *x, depth)
	case []any:
		return encodeArray(buf, x, depth)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf = appendU32(append(buf, tagMap), uint32(len(x)))
		for _, k := range keys {
			buf = appendBlob(buf, tagString, []byte(k))
			var err error
			if buf, err = encodeValue(buf, x[k], depth+1); err != nil {
				return nil, err
			}
		}
		return buf, nil
	case map[int64]any:
		keys := make([]int64, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		buf = appendU32(append(buf, tagMap), uint32(len(x)))
		for _, k := range keys {
			buf = appendInt(buf, k)
			var err error
			if buf, err = encodeValue(buf, x[k], depth+1); err != nil {
				return nil, err
			}
		}
		return buf, nil
	case map[any]any:
		return encodeMixedMap(buf, x, depth)
	}
	return encodeReflect(buf, reflect.ValueOf(v), depth)
}

func encodeArray(buf []byte, items []any, depth int) ([]byte, error) {
	buf = appendU32(append(buf, tagArray), uint32(len(items)))
	for _, item := range items {
		var err error
		if buf, err = encodeValue(buf, item, depth+1); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

func encodeStruct(buf []byte, s Struct, depth int) ([]byte, error) {
	buf = appendU32(append(buf, tagStruct), uint32(len(s.Name)))
	buf = append(buf, s.Name...)
	buf = appendU32(buf, uint32(len(s.Fields)))
	for _, f := range s.Fields {
		buf = appendBlob(buf, tagString, []byte(f.Name))
		var err error
		if buf, err = encodeValue(buf, f.Value, depth+1); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// encodeMixedMap: ints antes de strings, cada grupo ordenado — a mesma
// ordem deterministica do host.
func encodeMixedMap(buf []byte, m map[any]any, depth int) ([]byte, error) {
	var ints []int64
	var strs []string
	for k := range m {
		switch key := k.(type) {
		case int64:
			ints = append(ints, key)
		case int:
			ints = append(ints, int64(key))
		case string:
			strs = append(strs, key)
		default:
			return nil, fmt.Errorf("nxb: map key of type %T cannot cross the boundary", k)
		}
	}
	sort.Slice(ints, func(i, j int) bool { return ints[i] < ints[j] })
	sort.Strings(strs)
	buf = appendU32(append(buf, tagMap), uint32(len(m)))
	var err error
	for _, k := range ints {
		buf = appendInt(buf, k)
		item, ok := m[k]
		if !ok {
			item = m[int(k)]
		}
		if buf, err = encodeValue(buf, item, depth+1); err != nil {
			return nil, err
		}
	}
	for _, k := range strs {
		buf = appendBlob(buf, tagString, []byte(k))
		if buf, err = encodeValue(buf, m[k], depth+1); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

// encodeReflect cobre slices e maps tipados ([]string, map[string]int, ...)
// e ponteiros; qualquer outro tipo Go nao cruza a fronteira.
func encodeReflect(buf []byte, rv reflect.Value, depth int) ([]byte, error) {
	switch rv.Kind() {
	case reflect.Ptr:
		if rv.IsNil() {
			return append(buf, tagNull), nil
		}
		return encodeValue(buf, rv.Elem().Interface(), depth)
	case reflect.Slice, reflect.Array:
		items := make([]any, rv.Len())
		for i := range items {
			items[i] = rv.Index(i).Interface()
		}
		return encodeArray(buf, items, depth)
	case reflect.Map:
		mixed := make(map[any]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key()
			switch key.Kind() {
			case reflect.String:
				mixed[key.String()] = iter.Value().Interface()
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				mixed[key.Int()] = iter.Value().Interface()
			default:
				return nil, fmt.Errorf("nxb: map key of type %s cannot cross the boundary", key.Type())
			}
		}
		return encodeMixedMap(buf, mixed, depth)
	}
	if !rv.IsValid() {
		return append(buf, tagNull), nil
	}
	return nil, fmt.Errorf("nxb: Go value of type %s cannot cross the boundary", rv.Type())
}

func encodeArgs(args []any) ([]byte, error) {
	buf := appendU32(nil, uint32(len(args)))
	for _, arg := range args {
		var err error
		if buf, err = encodeValue(buf, arg, 0); err != nil {
			return nil, err
		}
	}
	return buf, nil
}

type decoder struct {
	data []byte
	pos  int
}

func (d *decoder) u8() (byte, error) {
	if d.pos >= len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	b := d.data[d.pos]
	d.pos++
	return b, nil
}

func (d *decoder) u32() (uint32, error) {
	if d.pos+4 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint32(d.data[d.pos:])
	d.pos += 4
	return v, nil
}

func (d *decoder) u64() (uint64, error) {
	if d.pos+8 > len(d.data) {
		return 0, fmt.Errorf("nxb: truncated input at offset %d", d.pos)
	}
	v := binary.LittleEndian.Uint64(d.data[d.pos:])
	d.pos += 8
	return v, nil
}

func (d *decoder) blob() ([]byte, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	if d.pos+int(n) > len(d.data) {
		return nil, fmt.Errorf("nxb: truncated blob at offset %d", d.pos)
	}
	out := make([]byte, n)
	copy(out, d.data[d.pos:d.pos+int(n)])
	d.pos += int(n)
	return out, nil
}

func (d *decoder) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("nxb: value nesting exceeds depth %d", maxDepth)
	}
	tag, err := d.u8()
	if err != nil {
		return nil, err
	}
	switch tag {
	case tagNull:
		return nil, nil
	case tagBool:
		b, err := d.u8()
		return b != 0, err
	case tagInt:
		v, err := d.u64()
		return int64(v), err
	case tagFloat:
		v, err := d.u64()
		return math.Float64frombits(v), err
	case tagString:
		b, err := d.blob()
		return string(b), err
	case tagBytes:
		return d.blob()
	case tagArray:
		count, err := d.u32()
		if err != nil {
			return nil, err
		}
		items := make([]any, 0, min(int(count), len(d.data)-d.pos))
		for i := uint32(0); i < count; i++ {
			item, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case tagMap:
		return d.decodeMap(depth)
	case tagStruct:
		name, err := d.blob()
		if err != nil {
			return nil, err
		}
		count, err := d.u32()
		if err != nil {
			return nil, err
		}
		s := Struct{Name: string(name)}
		for i := uint32(0); i < count; i++ {
			nameTag, err := d.u8()
			if err != nil || nameTag != tagString {
				return nil, fmt.Errorf("nxb: struct field name must be a string")
			}
			field, err := d.blob()
			if err != nil {
				return nil, err
			}
			item, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			s.Fields = append(s.Fields, Field{Name: string(field), Value: item})
		}
		return s, nil
	}
	return nil, fmt.Errorf("nxb: unknown tag 0x%02x at offset %d", tag, d.pos-1)
}

// decodeMap devolve map[string]any quando toda chave e string (inclusive o
// mapa vazio), map[int64]any quando toda chave e int, map[any]any se
// misturado.
func (d *decoder) decodeMap(depth int) (any, error) {
	count, err := d.u32()
	if err != nil {
		return nil, err
	}
	keys := make([]any, 0, min(int(count), len(d.data)-d.pos))
	values := make([]any, 0, cap(keys))
	strKeys, intKeys := 0, 0
	for i := uint32(0); i < count; i++ {
		key, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		switch key.(type) {
		case string:
			strKeys++
		case int64:
			intKeys++
		default:
			return nil, fmt.Errorf("nxb: invalid map key")
		}
		item, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
		values = append(values, item)
	}
	switch {
	case intKeys == 0:
		out := make(map[string]any, len(keys))
		for i, k := range keys {
			out[k.(string)] = values[i]
		}
		return out, nil
	case strKeys == 0:
		out := make(map[int64]any, len(keys))
		for i, k := range keys {
			out[k.(int64)] = values[i]
		}
		return out, nil
	default:
		out := make(map[any]any, len(keys))
		for i, k := range keys {
			out[k] = values[i]
		}
		return out, nil
	}
}

func decodeValue(data []byte) (any, error) {
	d := &decoder{data: data}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("nxb: %d trailing bytes after value", len(data)-d.pos)
	}
	return v, nil
}

func decodeArgs(data []byte) ([]any, error) {
	d := &decoder{data: data}
	count, err := d.u32()
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, min(int(count), len(data)-d.pos))
	for i := uint32(0); i < count; i++ {
		arg, err := d.value(0)
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("nxb: %d trailing bytes after arguments", len(data)-d.pos)
	}
	return args, nil
}

// decodeStringMap le os corpos de HELLO/ERROR/LOG (mapas de chave string).
func decodeStringMap(data []byte) (map[string]any, error) {
	v, err := decodeValue(data)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("body is not a string-keyed map")
	}
	return m, nil
}
```

- [ ] **Step 5: Run**

Run (from `sdk/noxyplugin`): `go test ./... -count=1 && go vet ./...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add sdk/noxyplugin/go.mod sdk/noxyplugin/frame.go sdk/noxyplugin/nxb.go sdk/noxyplugin/frame_test.go sdk/noxyplugin/nxb_test.go
git commit -m "feat(sdk): modulo Go noxyplugin — codecs de quadro e NXB sobre tipos Go (issue #80)"
```

---

### Task 11: SDK `Plugin` — handshake, dispatch, CANCEL, EOF, `Args`, `FuncN`, `Logf`, `Main`

**Files:**
- Create: `sdk/noxyplugin/plugin.go`, `sdk/noxyplugin/args.go`, `sdk/noxyplugin/funcs.go`
- Test: `sdk/noxyplugin/plugin_test.go`

**Interfaces:**
- Produces (§9.2, §9.4): `const Version = "0.1.0"`; `type Handler func(ctx context.Context, args Args) (any, error)`; `func New() *Plugin`; `(*Plugin).Handle(name string, h Handler)`; `(*Plugin).Serve(r io.Reader, w io.Writer) error` (returns `nil` on EOF, `*ExitError` on protocol trouble); `(*Plugin).Main()`; `type ExitError struct { Code int; Msg string }`; `type Level int` with `LevelDebug/LevelInfo/LevelWarn/LevelError`; `func Logf(level Level, format string, args ...any)`; `type Args []any` with `Int/Float/Bool/String/Bytes/Array/Map/IntMap/Struct(i int)`; `Func0…Func5` generic adapters.
- Consumes: Task 10.

- [ ] **Step 1: Write the failing tests**

```go
// sdk/noxyplugin/plugin_test.go
package noxyplugin

import (
	"bufio"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

// fakeHost fala noxy-plugin/1 do lado do host, em memoria.
type fakeHost struct {
	toPlugin   *io.PipeWriter
	fromPlugin *bufio.Reader
	served     chan error
}

func startPlugin(t *testing.T, p *Plugin) *fakeHost {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	h := &fakeHost{toPlugin: inW, fromPlugin: bufio.NewReader(outR), served: make(chan error, 1)}
	go func() { h.served <- p.Serve(inR, outW); _ = outW.Close() }()
	t.Cleanup(func() { _ = inW.Close() })
	return h
}

func (h *fakeHost) write(t *testing.T, f frame) {
	t.Helper()
	if err := writeFrame(h.toPlugin, f); err != nil {
		t.Fatalf("host write: %v", err)
	}
}

func (h *fakeHost) read(t *testing.T) frame {
	t.Helper()
	f, err := readFrame(h.fromPlugin, 64<<20)
	if err != nil {
		t.Fatalf("host read: %v", err)
	}
	return f
}

func (h *fakeHost) hello(t *testing.T, exports ...string) frame {
	t.Helper()
	names := make([]any, len(exports))
	for i, e := range exports {
		names[i] = e
	}
	body, _ := encodeValue(nil, map[string]any{"protocol": protocolVersion, "noxy": "test", "extension": "t", "exports": names}, 0)
	h.write(t, frame{Kind: kindHello, Body: body})
	return h.read(t)
}

func (h *fakeHost) call(t *testing.T, id, fn uint32, args ...any) {
	t.Helper()
	body, err := encodeArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	h.write(t, frame{Kind: kindCall, ID: id, Fn: fn, Body: body})
}

func message(t *testing.T, f frame) string {
	t.Helper()
	m, err := decodeStringMap(f.Body)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := m["message"].(string)
	return s
}

func newTestPlugin() *Plugin {
	p := New()
	p.Handle("t_add", Func2(func(_ context.Context, a, b int64) (int64, error) { return a + b, nil }))
	p.Handle("t_fail", Func0(func(context.Context) (any, error) { return nil, errors.New("nope") }))
	p.Handle("t_panic", Func0(func(context.Context) (any, error) { panic("kaboom") }))
	p.Handle("t_wait", Func0(func(ctx context.Context) (any, error) { <-ctx.Done(); return nil, ctx.Err() }))
	p.Handle("t_log", Func0(func(context.Context) (any, error) { Logf(LevelWarn, "careful %d", 1); return "ok", nil }))
	return p
}

func TestServeHandshakeAndCall(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	reply := h.hello(t, "t_add", "t_fail")
	if reply.Kind != kindHello {
		t.Fatalf("expected HELLO back, got 0x%02x %s", reply.Kind, message(t, reply))
	}
	m, _ := decodeStringMap(reply.Body)
	if m["protocol"] != protocolVersion || !strings.HasPrefix(m["sdk"].(string), "noxyplugin-go/") {
		t.Fatalf("plugin HELLO: %#v", m)
	}
	h.call(t, 1, 0, int64(2), int64(3))
	res := h.read(t)
	v, _ := decodeValue(res.Body)
	if res.Kind != kindResult || res.ID != 1 || v != int64(5) {
		t.Fatalf("RESULT: %#v %#v", res, v)
	}
}

func TestServeErrorsAndPanics(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_fail", "t_panic", "t_add")
	h.call(t, 1, 0)
	if f := h.read(t); f.Kind != kindError || message(t, f) != "nope" {
		t.Fatalf("handler error: %#v", f)
	}
	h.call(t, 2, 1)
	if f := h.read(t); f.Kind != kindError || message(t, f) != "panic: kaboom" {
		t.Fatalf("panic: %#v %s", f, message(t, f))
	}
	h.call(t, 3, 2, "x", int64(1))
	if f := h.read(t); f.Kind != kindError || message(t, f) != "argument 1: expected int, got string" {
		t.Fatalf("typed adapter: %s", message(t, f))
	}
	h.call(t, 4, 2, int64(1))
	if f := h.read(t); f.Kind != kindError || message(t, f) != "expected 2 arguments, got 1" {
		t.Fatalf("arity: %s", message(t, f))
	}
}

func TestServeRefusesMissingHandlerAndBadProtocol(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	reply := h.hello(t, "t_add", "t_zzz")
	if reply.Kind != kindError || reply.ID != 0 || message(t, reply) != `no handler for export "t_zzz"` {
		t.Fatalf("got %#v %s", reply, message(t, reply))
	}
	var exit *ExitError
	if err := <-h.served; !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("Serve must return ExitError code 2, got %v", err)
	}

	h2 := startPlugin(t, newTestPlugin())
	body, _ := encodeValue(nil, map[string]any{"protocol": "noxy-plugin/9", "exports": []any{}}, 0)
	h2.write(t, frame{Kind: kindHello, Body: body})
	reply = h2.read(t)
	if reply.Kind != kindError || !strings.Contains(message(t, reply), "unsupported protocol") {
		t.Fatalf("got %#v %s", reply, message(t, reply))
	}
}

func TestServeCancelAndEOF(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_wait")
	h.call(t, 1, 0)
	h.write(t, frame{Kind: kindCancel, ID: 1})
	if f := h.read(t); f.Kind != kindError || message(t, f) != "context canceled" {
		t.Fatalf("CANCEL must cancel the handler context: %#v %s", f, message(t, f))
	}
	h.call(t, 2, 0)
	_ = h.toPlugin.Close() // EOF: shutdown
	select {
	case err := <-h.served:
		if err != nil {
			t.Fatalf("EOF is a clean exit, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve must return after EOF (handlers cancelled, bounded wait)")
	}
}

func TestServeLogFrames(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_log")
	h.call(t, 1, 0)
	logf := h.read(t)
	if logf.Kind != kindLog {
		t.Fatalf("LOG must precede the RESULT, got 0x%02x", logf.Kind)
	}
	m, _ := decodeStringMap(logf.Body)
	if m["level"] != int64(LevelWarn) || m["message"] != "careful 1" {
		t.Fatalf("LOG body: %#v", m)
	}
	if f := h.read(t); f.Kind != kindResult {
		t.Fatalf("RESULT after LOG, got 0x%02x", f.Kind)
	}
}

func TestServeUnexpectedFrameExits(t *testing.T) {
	h := startPlugin(t, newTestPlugin())
	h.hello(t, "t_add")
	h.write(t, frame{Kind: kindResult, ID: 9})
	var exit *ExitError
	if err := <-h.served; !errors.As(err, &exit) || exit.Code != 2 {
		t.Fatalf("got %v", err)
	}
}

func TestFuncAdaptersConvert(t *testing.T) {
	join := Func1(func(_ context.Context, parts []string) (string, error) { return strings.Join(parts, "+"), nil })
	got, err := join(context.Background(), Args{[]any{"a", "b"}})
	if err != nil || got != "a+b" {
		t.Fatalf("[]any → []string: %v %v", got, err)
	}
	sum := Func1(func(_ context.Context, m map[string]int) (int, error) { return m["x"] + m["y"], nil })
	got, err = sum(context.Background(), Args{map[string]any{"x": int64(1), "y": int64(2)}})
	if err != nil || got != 3 {
		t.Fatalf("map[string]any → map[string]int: %v %v", got, err)
	}
	name := Func1(func(_ context.Context, s Struct) (string, error) { return s.Name, nil })
	got, err = name(context.Background(), Args{Struct{Name: "Point"}})
	if err != nil || got != "Point" {
		t.Fatalf("Struct passthrough: %v %v", got, err)
	}
	small := Func1(func(_ context.Context, n int8) (int8, error) { return n, nil })
	if _, err := small(context.Background(), Args{int64(300)}); err == nil || !strings.Contains(err.Error(), "overflows") {
		t.Fatalf("overflow must be reported: %v", err)
	}
	optional := Func1(func(_ context.Context, xs []int64) (int, error) { return len(xs), nil })
	got, err = optional(context.Background(), Args{nil})
	if err != nil || got != 0 {
		t.Fatalf("null → nil slice: %v %v", got, err)
	}
}

func TestArgsAccessors(t *testing.T) {
	args := Args{int64(1), "s", []byte{1}, []any{int64(2)}, map[string]any{"k": true}, 2.5, true, Struct{Name: "S"}}
	if v, err := args.Int(0); err != nil || v != 1 {
		t.Fatal(v, err)
	}
	if v, err := args.String(1); err != nil || v != "s" {
		t.Fatal(v, err)
	}
	if v, err := args.Bytes(2); err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}
	if v, err := args.Array(3); err != nil || len(v) != 1 {
		t.Fatal(v, err)
	}
	if v, err := args.Map(4); err != nil || v["k"] != true {
		t.Fatal(v, err)
	}
	if v, err := args.Float(5); err != nil || v != 2.5 {
		t.Fatal(v, err)
	}
	if v, err := args.Bool(6); err != nil || !v {
		t.Fatal(v, err)
	}
	if v, err := args.Struct(7); err != nil || v.Name != "S" {
		t.Fatal(v, err)
	}
	if _, err := args.Int(1); err == nil || err.Error() != "argument 2: expected int, got string" {
		t.Fatalf("type error text: %v", err)
	}
	if _, err := args.Int(99); err == nil || err.Error() != "argument 100: missing" {
		t.Fatalf("missing text: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run (from `sdk/noxyplugin`): `go test ./... -count=1`
Expected: FAIL — undefined `New`, `Func2`, `Args`.

- [ ] **Step 3: Implement `plugin.go`**

```go
// sdk/noxyplugin/plugin.go
package noxyplugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"time"
)

// Version e a versao do SDK anunciada no HELLO ("noxyplugin-go/<Version>").
const Version = "0.1.0"

const (
	maxBody      = 64 << 20
	shutdownWait = time.Second
)

// Level e o nivel de um LOG (spec §2.6).
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

// Handler atende um export: args ja decodificados, resultado codificado
// pelo SDK. Um erro vira ERROR (`extension 'x' failed: <msg>` no Noxy); um
// panic e recuperado como ERROR "panic: <v>" e o processo continua.
type Handler func(ctx context.Context, args Args) (any, error)

// ExitError e devolvido por Serve quando o host violou o protocolo ou a
// escrita falhou; Main traduz em os.Exit(Code).
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string { return e.Msg }

type Plugin struct {
	handlers map[string]Handler
	table    []Handler

	out      io.Writer
	writeMu  sync.Mutex
	writeErr error

	calls   map[uint32]context.CancelFunc
	callsMu sync.Mutex
	wg      sync.WaitGroup
	base    context.Context
	stop    context.CancelFunc
}

// current e o plugin em Serve, para Logf.
var current atomic.Pointer[Plugin]

func New() *Plugin {
	return &Plugin{handlers: map[string]Handler{}, calls: map[uint32]context.CancelFunc{}}
}

// Handle registra o handler do export `name` (o nome do manifesto, com o
// prefixo da extensao). Handlers extras sao permitidos; um export do
// manifesto sem handler recusa o handshake.
func (p *Plugin) Handle(name string, h Handler) { p.handlers[name] = h }

// Serve fala noxy-plugin/1 em r/w ate o EOF de r (nil) ou uma violacao do
// host (*ExitError). Uma goroutine por CALL; o host serializa em "single".
func (p *Plugin) Serve(r io.Reader, w io.Writer) error {
	p.out = w
	p.base, p.stop = context.WithCancel(context.Background())
	defer p.stop()
	current.Store(p)
	defer current.CompareAndSwap(p, nil)

	in := bufio.NewReaderSize(r, 64<<10)
	if err := p.handshake(in); err != nil {
		return err
	}
	for {
		f, err := readFrame(in, maxBody)
		if err != nil {
			p.shutdown()
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return &ExitError{Code: 2, Msg: "noxyplugin: " + err.Error()}
		}
		switch f.Kind {
		case kindCall:
			p.dispatch(f)
		case kindCancel:
			p.cancel(f.ID)
		default:
			p.shutdown()
			return &ExitError{Code: 2, Msg: fmt.Sprintf("noxyplugin: unexpected frame kind 0x%02x from the host", f.Kind)}
		}
		if err := p.lastWriteError(); err != nil {
			p.shutdown()
			return &ExitError{Code: 2, Msg: "noxyplugin: write: " + err.Error()}
		}
	}
}

func (p *Plugin) handshake(in *bufio.Reader) error {
	f, err := readFrame(in, maxBody)
	if err != nil {
		return &ExitError{Code: 2, Msg: "noxyplugin: read HELLO: " + err.Error()}
	}
	if f.Kind != kindHello {
		return &ExitError{Code: 2, Msg: fmt.Sprintf("noxyplugin: first frame is 0x%02x, not HELLO", f.Kind)}
	}
	hello, err := decodeStringMap(f.Body)
	if err != nil {
		return &ExitError{Code: 2, Msg: "noxyplugin: HELLO body: " + err.Error()}
	}
	proto, _ := hello["protocol"].(string)
	if proto != protocolVersion {
		p.sendError(0, fmt.Sprintf("unsupported protocol %q (plugin speaks %s)", proto, protocolVersion))
		return &ExitError{Code: 2, Msg: "noxyplugin: unsupported protocol " + proto}
	}
	// Binding por nome (spec §2.4): a tabela por indice nasce da lista do
	// host, e um export sem handler recusa o handshake com o nome.
	rawExports, _ := hello["exports"].([]any)
	table := make([]Handler, len(rawExports))
	for i, raw := range rawExports {
		name, _ := raw.(string)
		h, ok := p.handlers[name]
		if !ok {
			p.sendError(0, fmt.Sprintf("no handler for export %q", name))
			return &ExitError{Code: 2, Msg: "noxyplugin: no handler for export " + name}
		}
		table[i] = h
	}
	p.table = table
	body, err := encodeValue(nil, map[string]any{"protocol": protocolVersion, "sdk": "noxyplugin-go/" + Version}, 0)
	if err != nil {
		return err
	}
	p.send(frame{Kind: kindHello, Body: body})
	if err := p.lastWriteError(); err != nil {
		return &ExitError{Code: 2, Msg: "noxyplugin: write HELLO: " + err.Error()}
	}
	return nil
}

func (p *Plugin) dispatch(f frame) {
	args, err := decodeArgs(f.Body)
	if err != nil {
		p.sendError(f.ID, "invalid arguments: "+err.Error())
		return
	}
	if int(f.Fn) >= len(p.table) {
		p.sendError(f.ID, fmt.Sprintf("unknown export index %d", f.Fn))
		return
	}
	handler := p.table[f.Fn]
	ctx, cancel := context.WithCancel(p.base)
	p.callsMu.Lock()
	p.calls[f.ID] = cancel
	p.callsMu.Unlock()
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() {
			p.callsMu.Lock()
			delete(p.calls, f.ID)
			p.callsMu.Unlock()
			cancel()
		}()
		result, err := invoke(ctx, handler, args)
		if err != nil {
			p.sendError(f.ID, err.Error())
			return
		}
		body, err := encodeValue(nil, result, 0)
		if err != nil {
			p.sendError(f.ID, "result cannot cross the boundary: "+err.Error())
			return
		}
		p.send(frame{Kind: kindResult, ID: f.ID, Body: body})
	}()
}

func invoke(ctx context.Context, h Handler, args Args) (result any, err error) {
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return h(ctx, args)
}

func (p *Plugin) cancel(id uint32) {
	p.callsMu.Lock()
	cancel := p.calls[id]
	p.callsMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// shutdown cancela todo handler em voo e espera ate shutdownWait: quem nao
// devolveu ate la e abandonado (spec §2.7, §9.4).
func (p *Plugin) shutdown() {
	p.stop()
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(shutdownWait):
	}
}

func (p *Plugin) send(f frame) {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if p.writeErr != nil {
		return
	}
	p.writeErr = writeFrame(p.out, f)
}

func (p *Plugin) lastWriteError() error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.writeErr
}

func (p *Plugin) sendError(id uint32, msg string) {
	body, err := encodeValue(nil, map[string]any{"message": msg}, 0)
	if err != nil {
		return
	}
	p.send(frame{Kind: kindError, ID: id, Body: body})
}

// Logf envia um LOG ao host (`[ext <name>] <msg>` no stderr do Noxy). Fora
// de Serve escreve em stderr.
func Logf(level Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	p := current.Load()
	if p == nil {
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	body, err := encodeValue(nil, map[string]any{"level": int64(level), "message": msg}, 0)
	if err != nil {
		return
	}
	p.send(frame{Kind: kindLog, Body: body})
}

// Main serve stdin/stdout e sai com o status do protocolo (spec §9.4):
// protege o canal (os.Stdout passa a apontar para stderr), recusa rodar num
// terminal, ignora SIGINT (Ctrl-C e do host; o filho sai no EOF).
func (p *Plugin) Main() {
	stdout := os.Stdout
	os.Stdout = os.Stderr
	if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "this program is a Noxy extension; install it with 'noxy --get'")
		os.Exit(2)
	}
	signal.Ignore(os.Interrupt)
	err := p.Serve(os.Stdin, stdout)
	var exit *ExitError
	switch {
	case errors.As(err, &exit):
		fmt.Fprintln(os.Stderr, exit.Msg)
		os.Exit(exit.Code)
	case err != nil:
		fmt.Fprintln(os.Stderr, "noxyplugin:", err)
		os.Exit(2)
	}
	os.Exit(0)
}
```

- [ ] **Step 4: Implement `args.go`**

```go
// sdk/noxyplugin/args.go
package noxyplugin

import "fmt"

// Args sao os argumentos de uma chamada, ja decodificados (§9.3): int64,
// float64, bool, string, []byte, []any, map[string]any / map[int64]any /
// map[any]any, Struct, nil.
type Args []any

func (a Args) count(want int) error {
	if len(a) != want {
		return fmt.Errorf("expected %d arguments, got %d", want, len(a))
	}
	return nil
}

func (a Args) at(i int) (any, error) {
	if i < 0 || i >= len(a) {
		return nil, fmt.Errorf("argument %d: missing", i+1)
	}
	return a[i], nil
}

func argTypeError(i int, want string, got any) error {
	return fmt.Errorf("argument %d: expected %s, got %s", i+1, want, typeName(got))
}

// typeName nomeia o valor no vocabulario da Noxy, para mensagens.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "bool"
	case int64:
		return "int"
	case float64:
		return "float"
	case string:
		return "string"
	case []byte:
		return "bytes"
	case []any:
		return "array"
	case map[string]any, map[int64]any, map[any]any:
		return "map"
	case Struct:
		return "struct"
	}
	return fmt.Sprintf("%T", v)
}

func (a Args) Int(i int) (int64, error) {
	v, err := a.at(i)
	if err != nil {
		return 0, err
	}
	n, ok := v.(int64)
	if !ok {
		return 0, argTypeError(i, "int", v)
	}
	return n, nil
}

func (a Args) Float(i int) (float64, error) {
	v, err := a.at(i)
	if err != nil {
		return 0, err
	}
	f, ok := v.(float64)
	if !ok {
		return 0, argTypeError(i, "float", v)
	}
	return f, nil
}

func (a Args) Bool(i int) (bool, error) {
	v, err := a.at(i)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, argTypeError(i, "bool", v)
	}
	return b, nil
}

func (a Args) String(i int) (string, error) {
	v, err := a.at(i)
	if err != nil {
		return "", err
	}
	s, ok := v.(string)
	if !ok {
		return "", argTypeError(i, "string", v)
	}
	return s, nil
}

func (a Args) Bytes(i int) ([]byte, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	b, ok := v.([]byte)
	if !ok {
		return nil, argTypeError(i, "bytes", v)
	}
	return b, nil
}

func (a Args) Array(i int) ([]any, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	items, ok := v.([]any)
	if !ok {
		return nil, argTypeError(i, "array", v)
	}
	return items, nil
}

func (a Args) Map(i int) (map[string]any, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, argTypeError(i, "map", v)
	}
	return m, nil
}

func (a Args) IntMap(i int) (map[int64]any, error) {
	v, err := a.at(i)
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[int64]any)
	if !ok {
		return nil, argTypeError(i, "map", v)
	}
	return m, nil
}

func (a Args) Struct(i int) (Struct, error) {
	v, err := a.at(i)
	if err != nil {
		return Struct{}, err
	}
	s, ok := v.(Struct)
	if !ok {
		return Struct{}, argTypeError(i, "struct", v)
	}
	return s, nil
}
```

- [ ] **Step 5: Implement `funcs.go`**

```go
// sdk/noxyplugin/funcs.go
package noxyplugin

import (
	"context"
	"fmt"
	"reflect"
)

// Func0..Func5 adaptam funcoes Go tipadas a Handler: conferem a aridade e
// convertem cada argumento (§9.2) — a checagem do lado do plugin, gemea do
// checkDeclaredReturn do host.

func Func0[R any](f func(context.Context) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(0); err != nil {
			return nil, err
		}
		return f(ctx)
	}
}

func Func1[A, R any](f func(context.Context, A) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(1); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		return f(ctx, a)
	}
}

func Func2[A, B, R any](f func(context.Context, A, B) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(2); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b)
	}
}

func Func3[A, B, C, R any](f func(context.Context, A, B, C) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(3); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		c, err := arg[C](args, 2)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b, c)
	}
}

func Func4[A, B, C, D, R any](f func(context.Context, A, B, C, D) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(4); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		c, err := arg[C](args, 2)
		if err != nil {
			return nil, err
		}
		d, err := arg[D](args, 3)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b, c, d)
	}
}

func Func5[A, B, C, D, E, R any](f func(context.Context, A, B, C, D, E) (R, error)) Handler {
	return func(ctx context.Context, args Args) (any, error) {
		if err := args.count(5); err != nil {
			return nil, err
		}
		a, err := arg[A](args, 0)
		if err != nil {
			return nil, err
		}
		b, err := arg[B](args, 1)
		if err != nil {
			return nil, err
		}
		c, err := arg[C](args, 2)
		if err != nil {
			return nil, err
		}
		d, err := arg[D](args, 3)
		if err != nil {
			return nil, err
		}
		e, err := arg[E](args, 4)
		if err != nil {
			return nil, err
		}
		return f(ctx, a, b, c, d, e)
	}
}

func arg[T any](args Args, i int) (T, error) {
	var zero T
	converted, err := coerce(args[i], reflect.TypeOf(&zero).Elem())
	if err != nil {
		return zero, fmt.Errorf("argument %d: %w", i+1, err)
	}
	if converted == nil {
		return zero, nil
	}
	return converted.(T), nil
}

var structType = reflect.TypeOf(Struct{})

// noxyName nomeia um tipo Go de parametro no vocabulario da Noxy.
func noxyName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.String:
		return "string"
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			return "bytes"
		}
		return "array"
	case reflect.Map:
		return "map"
	case reflect.Struct:
		if t == structType {
			return "struct"
		}
	}
	return t.String()
}

// coerce converte um valor decodificado para o tipo Go do parametro.
func coerce(v any, t reflect.Type) (any, error) {
	if t.Kind() == reflect.Interface {
		return v, nil
	}
	if v == nil {
		switch t.Kind() {
		case reflect.Slice, reflect.Map, reflect.Ptr:
			return nil, nil
		}
		return nil, fmt.Errorf("expected %s, got null", noxyName(t))
	}
	mismatch := func() error { return fmt.Errorf("expected %s, got %s", noxyName(t), typeName(v)) }
	rv := reflect.New(t).Elem()
	switch t.Kind() {
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return nil, mismatch()
		}
		rv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := v.(int64)
		if !ok {
			return nil, mismatch()
		}
		if rv.OverflowInt(n) {
			return nil, fmt.Errorf("int %d overflows %s", n, t)
		}
		rv.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, ok := v.(int64)
		if !ok {
			return nil, mismatch()
		}
		if n < 0 || rv.OverflowUint(uint64(n)) {
			return nil, fmt.Errorf("int %d overflows %s", n, t)
		}
		rv.SetUint(uint64(n))
	case reflect.Float32, reflect.Float64:
		f, ok := v.(float64)
		if !ok {
			return nil, mismatch()
		}
		rv.SetFloat(f)
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return nil, mismatch()
		}
		rv.SetString(s)
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			b, ok := v.([]byte)
			if !ok {
				return nil, mismatch()
			}
			rv.SetBytes(b)
			break
		}
		items, ok := v.([]any)
		if !ok {
			return nil, mismatch()
		}
		out := reflect.MakeSlice(t, len(items), len(items))
		for i, item := range items {
			c, err := coerce(item, t.Elem())
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			if c != nil {
				out.Index(i).Set(reflect.ValueOf(c))
			}
		}
		rv.Set(out)
	case reflect.Map:
		out := reflect.MakeMap(t)
		set := func(key any, item any) error {
			k, err := coerce(key, t.Key())
			if err != nil {
				return fmt.Errorf("key %v: %w", key, err)
			}
			c, err := coerce(item, t.Elem())
			if err != nil {
				return fmt.Errorf("key %v: %w", key, err)
			}
			if c == nil {
				out.SetMapIndex(reflect.ValueOf(k), reflect.Zero(t.Elem()))
				return nil
			}
			out.SetMapIndex(reflect.ValueOf(k), reflect.ValueOf(c))
			return nil
		}
		switch src := v.(type) {
		case map[string]any:
			for k, item := range src {
				if err := set(k, item); err != nil {
					return nil, err
				}
			}
		case map[int64]any:
			for k, item := range src {
				if err := set(k, item); err != nil {
					return nil, err
				}
			}
		case map[any]any:
			for k, item := range src {
				if err := set(k, item); err != nil {
					return nil, err
				}
			}
		default:
			return nil, mismatch()
		}
		rv.Set(out)
	case reflect.Struct:
		if t != structType {
			return nil, fmt.Errorf("unsupported parameter type %s", t)
		}
		s, ok := v.(Struct)
		if !ok {
			return nil, mismatch()
		}
		rv.Set(reflect.ValueOf(s))
	default:
		return nil, fmt.Errorf("unsupported parameter type %s", t)
	}
	return rv.Interface(), nil
}
```

- [ ] **Step 6: Run**

Run (from `sdk/noxyplugin`): `go test ./... -count=1 -race && go vet ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add sdk/noxyplugin/plugin.go sdk/noxyplugin/args.go sdk/noxyplugin/funcs.go sdk/noxyplugin/plugin_test.go
git commit -m "feat(sdk): Plugin — handshake por nome, despacho, CANCEL, EOF, Args, FuncN, Logf, Main (issue #80)"
```

---

### Task 12: Test guest built with the SDK, `exttest.BuildProcessGuest`, subprocess tests

**Files:**
- Create: `internal/ext/testdata/processguest/go.mod`, `internal/ext/testdata/processguest/main.go`, `internal/ext/exttest/processguest.go`
- Test: `internal/ext/process_guest_test.go`

**Interfaces:**
- Produces: `func exttest.BuildProcessGuest(tb testing.TB) string` (path of the built executable, cached per test process); `const processGuestManifest` (package `ext` tests) with the guest's exports in this order: 0 `guest_echo(any)->any`, 1 `guest_add(int,int)->int`, 2 `guest_fail(string)->void`, 3 `guest_sleep_ms(int)->void` (`timeout_ms = 150`), 4 `guest_block()->void` (`timeout_ms = 100`), 5 `guest_exit(int)->void`, 6 `guest_log(string)->void`, 7 `guest_panic()->void`, 8 `guest_bytes(bytes)->bytes`, 9 `guest_pid()->int`, 10 `guest_print(string)->void`, 11 `guest_badtype()->int`, 12 `guest_noop()->void`; `func guestProcess(t, concurrency, extraKeys string) (*Process, *syncBuffer)`.
- Consumes: Tasks 5–8, 10–11.

- [ ] **Step 1: Write the guest module**

`internal/ext/testdata/processguest/go.mod`:

```
module processguest

go 1.25

require github.com/estevaofon/noxy/sdk/noxyplugin v0.0.0

replace github.com/estevaofon/noxy/sdk/noxyplugin => ../../../../sdk/noxyplugin
```

```go
// internal/ext/testdata/processguest/main.go
// Guest de teste do backend de processo, escrito com o SDK: compilado em
// tempo de teste por exttest.BuildProcessGuest. A ordem dos exports esta
// no manifesto de process_guest_test.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/estevaofon/noxy/sdk/noxyplugin"
)

func main() {
	p := noxyplugin.New()
	p.Handle("guest_echo", func(_ context.Context, args noxyplugin.Args) (any, error) {
		if len(args) == 0 {
			return nil, nil
		}
		return args[0], nil
	})
	p.Handle("guest_add", noxyplugin.Func2(func(_ context.Context, a, b int64) (int64, error) { return a + b, nil }))
	p.Handle("guest_fail", noxyplugin.Func1(func(_ context.Context, msg string) (any, error) { return nil, errors.New(msg) }))
	p.Handle("guest_sleep_ms", noxyplugin.Func1(func(ctx context.Context, ms int64) (any, error) {
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
			return nil, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}))
	p.Handle("guest_block", noxyplugin.Func0(func(context.Context) (any, error) {
		time.Sleep(10 * time.Second) // ignora o cancel de proposito
		return nil, nil
	}))
	p.Handle("guest_exit", noxyplugin.Func1(func(_ context.Context, code int64) (any, error) {
		os.Exit(int(code))
		return nil, nil
	}))
	p.Handle("guest_log", noxyplugin.Func1(func(_ context.Context, msg string) (any, error) {
		noxyplugin.Logf(noxyplugin.LevelInfo, "%s", msg)
		return nil, nil
	}))
	p.Handle("guest_panic", noxyplugin.Func0(func(context.Context) (any, error) { panic("kaboom") }))
	p.Handle("guest_bytes", noxyplugin.Func1(func(_ context.Context, b []byte) ([]byte, error) { return b, nil }))
	p.Handle("guest_pid", noxyplugin.Func0(func(context.Context) (int64, error) { return int64(os.Getpid()), nil }))
	p.Handle("guest_print", noxyplugin.Func1(func(_ context.Context, s string) (any, error) {
		fmt.Println(s) // vai para stderr: Main protege o stdout do protocolo
		return nil, nil
	}))
	p.Handle("guest_badtype", noxyplugin.Func0(func(context.Context) (string, error) { return "not an int", nil }))
	p.Handle("guest_noop", noxyplugin.Func0(func(context.Context) (any, error) { return nil, nil }))
	p.Main()
}
```

- [ ] **Step 2: Write the builder**

```go
// internal/ext/exttest/processguest.go
package exttest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	procMu   sync.Mutex
	procPath string
)

// BuildProcessGuest compila testdata/processguest (modulo aninhado que usa
// o SDK via replace) uma vez por processo de teste e devolve o caminho do
// executavel. Roda o binario uma vez logo apos o build (stdin vazio → o
// SDK ve EOF e sai 0): na maquina do dono um .exe recem-compilado pode ser
// apagado pelo antivirus nos primeiros segundos — se sumir, reconstroi uma
// vez antes de falhar.
func BuildProcessGuest(tb testing.TB) string {
	tb.Helper()
	procMu.Lock()
	defer procMu.Unlock()
	if procPath != "" {
		if _, err := os.Stat(procPath); err == nil {
			return procPath
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		path := buildProcessGuest(tb)
		if warm(path) == nil {
			procPath = path
			return path
		}
	}
	tb.Fatal("exttest: processguest binary vanished right after build, twice")
	return ""
}

func buildProcessGuest(tb testing.TB) string {
	tb.Helper()
	dir, err := os.MkdirTemp("", "noxy-processguest-")
	if err != nil {
		tb.Fatal(err)
	}
	name := "processguest"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, name)
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Dir = filepath.Join(repoRoot(tb), "internal", "ext", "testdata", "processguest")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("exttest: go build processguest: %v\n%s", err, output)
	}
	return out
}

func warm(path string) error {
	cmd := exec.Command(path)
	cmd.Stdin = strings.NewReader("")
	return cmd.Run()
}
```

- [ ] **Step 3: Write the subprocess tests**

```go
// internal/ext/process_guest_test.go
package ext

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

const processGuestManifest = `
name = "guest"
abi = 1
kind = "process"
concurrency = "%s"
call_timeout_ms = 2000
%s

[binaries]
%s = "%s"

[[export]]
name = "guest_echo"
params = ["any"]
returns = "any"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"

[[export]]
name = "guest_fail"
params = ["string"]
returns = "void"

[[export]]
name = "guest_sleep_ms"
params = ["int"]
returns = "void"
timeout_ms = 150

[[export]]
name = "guest_block"
params = []
returns = "void"
timeout_ms = 100

[[export]]
name = "guest_exit"
params = ["int"]
returns = "void"

[[export]]
name = "guest_log"
params = ["string"]
returns = "void"

[[export]]
name = "guest_panic"
params = []
returns = "void"

[[export]]
name = "guest_bytes"
params = ["bytes"]
returns = "bytes"

[[export]]
name = "guest_pid"
params = []
returns = "int"

[[export]]
name = "guest_print"
params = ["string"]
returns = "void"

[[export]]
name = "guest_badtype"
params = []
returns = "int"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`

const (
	fnEcho = iota
	fnAdd
	fnFail
	fnSleep
	fnBlock
	fnExit
	fnLog
	fnPanic
	fnBytes
	fnPid
	fnPrint
	fnBadType
	fnNoop
)

// syncBuffer vem de process_test.go (Task 5).

func guestManifest(t testing.TB, guestPath, concurrency, extra string) *Manifest {
	t.Helper()
	src := fmt.Sprintf(processGuestManifest, concurrency, extra, runtime.GOOS+"-"+runtime.GOARCH, filepath.Base(guestPath))
	m, err := ParseManifest([]byte(src))
	if err != nil {
		t.Fatalf("guest manifest: %v", err)
	}
	return m
}

func guestProcess(t *testing.T, concurrency, extra string) (*Process, *syncBuffer) {
	t.Helper()
	path := exttest.BuildProcessGuest(t)
	logs := &syncBuffer{}
	p := NewProcess(guestManifest(t, path, concurrency, extra), ProcessConfig{Path: path, NoxyVersion: "v0.23.0", Log: logs})
	t.Cleanup(func() { _ = p.Close(context.Background()) })
	return p, logs
}

func call(t *testing.T, p *Process, fn int, args ...value.Value) (value.Value, error) {
	t.Helper()
	return p.Call(context.Background(), fn, args)
}

func TestGuestEchoAddBytes(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	if got, err := call(t, p, fnAdd, value.NewInt(2), value.NewInt(3)); err != nil || got.Int() != 5 {
		t.Fatalf("add: %#v %v", got, err)
	}
	m := value.NewMap()
	m.Obj.(*value.ObjMap).Set("k", value.NewString("v"))
	got, err := call(t, p, fnEcho, m)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := got.Obj.(*value.ObjMap).Get("k"); !ok || v.Obj.(string) != "v" {
		t.Fatalf("echo map: %#v", got)
	}
	payload := strings.Repeat("x", 1<<20)
	if got, err := call(t, p, fnBytes, value.NewBytes(payload)); err != nil || got.Obj.(string) != payload {
		t.Fatalf("1 MB bytes round trip: %v", err)
	}
}

func TestGuestFailedPanicBadType(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	if _, err := call(t, p, fnFail, value.NewString("boom")); err == nil || err.Error() != "extension 'guest' failed: boom" {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnPanic); err == nil || err.Error() != "extension 'guest' failed: panic: kaboom" {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnBadType); err == nil || !strings.Contains(err.Error(), `declared return type "int"`) {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnAdd, value.NewString("x"), value.NewInt(1)); err == nil || err.Error() != "extension 'guest' failed: argument 1: expected int, got string" {
		t.Fatalf("got %v", err)
	}
	if got, err := call(t, p, fnAdd, value.NewInt(1), value.NewInt(1)); err != nil || got.Int() != 2 {
		t.Fatalf("none of the above poisons: %v", err)
	}
}

func TestGuestCancelHonoured(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	start := time.Now()
	_, err := call(t, p, fnSleep, value.NewInt(5000))
	if err == nil || err.Error() != "extension 'guest' timed out: guest_sleep_ms exceeded 150 ms" {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("a cancelled call returns promptly")
	}
	if got, err := call(t, p, fnAdd, value.NewInt(1), value.NewInt(1)); err != nil || got.Int() != 2 {
		t.Fatalf("process survives a cancelled call: %v", err)
	}
}

func TestGuestBlockIgnoresCancelIsKilled(t *testing.T) {
	shortGraces(t)
	p, _ := guestProcess(t, "single", "")
	_, err := call(t, p, fnBlock)
	if err == nil || !strings.Contains(err.Error(), "trapped: guest_block exceeded 100 ms and did not cancel; process killed") {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnNoop); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("got %v", err)
	}
}

func TestGuestExitTrapsAndRestart(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	if _, err := call(t, p, fnExit, value.NewInt(3)); err == nil || err.Error() != "extension 'guest' trapped: process exited (status 3)" {
		t.Fatalf("got %v", err)
	}
	if _, err := call(t, p, fnNoop); err == nil || !strings.Contains(err.Error(), "poisoned") {
		t.Fatalf("got %v", err)
	}

	r, _ := guestProcess(t, "stateless", "restart = true")
	first, err := call(t, r, fnPid)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = call(t, r, fnExit, value.NewInt(0))
	second, err := call(t, r, fnPid)
	if err != nil || second.Int() == first.Int() {
		t.Fatalf("restart must spawn a new process: %v %v %v", first, second, err)
	}
}

func TestGuestLogAndStdoutProtection(t *testing.T) {
	p, logs := guestProcess(t, "single", "")
	if _, err := call(t, p, fnLog, value.NewString("hello from guest")); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, p, fnPrint, value.NewString("stray print")); err != nil {
		t.Fatal(err)
	}
	if got, err := call(t, p, fnAdd, value.NewInt(4), value.NewInt(4)); err != nil || got.Int() != 8 {
		t.Fatalf("a stray print must not corrupt the stream: %v", err)
	}
	if !strings.Contains(logs.String(), "[ext guest] hello from guest\n") {
		t.Fatalf("log: %q", logs.String())
	}
}

func TestGuestConcurrentInterleaves(t *testing.T) {
	p, _ := guestProcess(t, "concurrent", "")
	if _, err := call(t, p, fnNoop); err != nil {
		t.Fatal(err)
	}
	sleepDone := make(chan error, 1)
	go func() {
		_, err := call(t, p, fnSleep, value.NewInt(100))
		sleepDone <- err
	}()
	time.Sleep(10 * time.Millisecond)
	start := time.Now()
	if _, err := call(t, p, fnAdd, value.NewInt(1), value.NewInt(2)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 80*time.Millisecond {
		t.Fatal("add must not wait for the sleeping call in concurrent mode")
	}
	if err := <-sleepDone; err != nil {
		t.Fatalf("sleep(100) is under its 150 ms deadline: %v", err)
	}
}

func TestGuestCloseExitsOnEOF(t *testing.T) {
	p, _ := guestProcess(t, "single", "")
	pid, err := call(t, p, fnPid)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := p.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) >= shutdownGrace {
		t.Fatal("the SDK exits on EOF before the grace")
	}
	deadline := time.Now().Add(2 * time.Second)
	for exttest.ProcessAlive(int(pid.Int())) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if exttest.ProcessAlive(int(pid.Int())) {
		t.Fatal("guest still alive after Close")
	}
}
```

`exttest.ProcessAlive` is written in Task 13; to keep this task green on its own, add it now (Task 13 only adds the orphan test around it):

```go
// internal/ext/exttest/alive_unix.go
//go:build !windows

package exttest

import "syscall"

// ProcessAlive diz se o pid existe (kill 0). EPERM tambem significa vivo.
func ProcessAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
```

```go
// internal/ext/exttest/alive_windows.go
//go:build windows

package exttest

import "golang.org/x/sys/windows"

const stillActive = 259

func ProcessAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}
```

- [ ] **Step 4: Run**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'Guest' -count=1 -race -v`
Expected: PASS on the dev machine; the first test builds the guest (a few seconds). If `TestGuestCloseExitsOnEOF` flakes because `ProcessAlive` sees a zombie on Linux, `execConn.Wait` has already reaped it — re-check that `Close` waits `done` before returning.

- [ ] **Step 5: Commit**

```bash
git add internal/ext/testdata/processguest internal/ext/exttest/processguest.go internal/ext/exttest/alive_unix.go internal/ext/exttest/alive_windows.go internal/ext/process_guest_test.go
git commit -m "test(ext): guest de processo escrito com o SDK e testes host x SDK por subprocesso (issue #80)"
```

---

### Task 13: Orphan guard test (host killed → guest exits)

**Files:**
- Modify: `internal/ext/process_exec_test.go` (`helperHost`, new test)

**Interfaces:**
- Consumes: `exttest.BuildProcessGuest`, `exttest.ProcessAlive`, `processGuestManifest`, `fnPid` (Task 12); `TestMain` helper switch (Task 8).

- [ ] **Step 1: Replace the `helperHost` stub and add the test**

```go
// helperHost (NOXY_EXT_HELPER=host): sobe o guest, imprime o pid dele em
// stdout e fica parado ate ser morto pelo teste — que entao confere que o
// guest morreu junto (spec §4.5: pdeathsig no Linux, job object no
// Windows, EOF no macOS).
func helperHost() int {
	guest := os.Getenv("NOXY_EXT_GUEST")
	src := fmt.Sprintf(processGuestManifest, "single", "", runtime.GOOS+"-"+runtime.GOARCH, filepath.Base(guest))
	m, err := ParseManifest([]byte(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	p := NewProcess(m, ProcessConfig{Path: guest, NoxyVersion: "helper"})
	pid, err := p.Call(context.Background(), fnPid, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println(pid.Int())
	time.Sleep(30 * time.Second)
	return 0
}

func TestOrphanGuestDiesWithHost(t *testing.T) {
	guest := exttest.BuildProcessGuest(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), "NOXY_EXT_HELPER=host", "NOXY_EXT_GUEST="+guest)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("helper host did not report the guest pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatalf("pid line %q: %v", line, err)
	}
	if !exttest.ProcessAlive(pid) {
		t.Fatal("guest must be alive while the host lives")
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for exttest.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if exttest.ProcessAlive(pid) {
		t.Fatalf("guest %d outlived its killed host", pid)
	}
}
```

Add `"os/exec"`, `"path/filepath"`, `"runtime"`, `"strconv"` and `"noxy-vm/internal/ext/exttest"` to the file's imports.

- [ ] **Step 2: Run**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run 'Orphan|ExecSpawner' -count=1 -v`
Expected: PASS on Windows and Linux (the CI matrix). On macOS the EOF rule carries it: the helper's pipe closes when it dies.

- [ ] **Step 3: Commit**

```bash
git add internal/ext/process_exec_test.go
git commit -m "test(ext): guest morre junto com o host morto — guarda contra orfaos (issue #80)"
```

---

## Part C — VM integration

### Task 14: Backend selection at `use`, binary resolution, hash verification

**Files:**
- Modify: `internal/vm/extensions.go` (`ensureExtensionLoaded` tail, `verifyExtensionSum` signature)
- Test: `internal/vm/process_extensions_e2e_test.go`

**Interfaces:**
- Produces: `(vm *VM) loadWasmBackend(dir string, manifest *ext.Manifest, manifestData []byte) (ext.Backend, error)`; `(vm *VM) loadProcessBackend(dir string, manifest *ext.Manifest, manifestData []byte) (ext.Backend, error)`; `verifyExtensionSum(dir string, manifest *ext.Manifest, manifestData []byte, artifactName string, artifact []byte) error` (artifact = `manifest.Wasm` or `"bin/"+asset`); test helper `writeProcessExtensionPackage(t, root string) (asset string)`.
- Consumes: `ext.NewProcess`, `ext.ProcessConfig`, `Manifest.BinaryFor/PublishedPlatforms/Kind`, `exttest.BuildProcessGuest`.
- Error texts (§1, §8.4): `extension "guest" has no binary for <goos>/<goarch> (published: <list>)`; `extension "guest": binary bin/<asset> not found — run 'noxy --get' to download it`; `extension artifact mismatch for <pkg>/bin/<asset>: noxy.sum has sha256:…, disk has sha256:…`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/vm/process_extensions_e2e_test.go
package vm

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

const testProcessExtManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
%s = "%s"

[[export]]
name = "guest_add"
params = ["int", "int"]
returns = "int"

[[export]]
name = "guest_fail"
params = ["string"]
returns = "void"

[[export]]
name = "guest_pid"
params = []
returns = "int"

[[export]]
name = "guest_exit"
params = ["int"]
returns = "void"
`

const testProcessExtWrapper = `
func add(a: int, b: int) -> int
    return guest_add(a, b)
end

func pid() -> int
    return guest_pid()
end
`

// writeProcessExtensionPackage instala noxy_libs/guest com o guest do SDK
// copiado para bin/<asset>; devolve o nome do asset.
func writeProcessExtensionPackage(t *testing.T, root string) string {
	t.Helper()
	guest := exttest.BuildProcessGuest(t)
	asset := filepath.Base(guest)
	pkg := filepath.Join(root, "noxy_libs", "guest")
	if err := os.MkdirAll(filepath.Join(pkg, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf(testProcessExtManifest, runtime.GOOS+"-"+runtime.GOARCH, asset)
	if err := os.WriteFile(filepath.Join(pkg, "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "guest.nx"), []byte(testProcessExtWrapper), 0o644); err != nil {
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

func sha256File(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestProcessExtensionEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	got := captureVMSourceAtRoot(t, root, `
use guest as g
test_report(g.add(2, 3))
`)
	if got.Type != value.VAL_INT || got.Int() != 5 {
		t.Fatalf("expected 5 through the process extension, got %#v", got)
	}
}

func TestProcessExtensionFailureIsRuntimeError(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	machine := NewWithConfig(VMConfig{RootPath: root})
	t.Cleanup(machine.CloseExtensions)
	code := compileVMSourceAtRoot(t, root, `
use guest as g
guest_fail("boom")
`)
	err := machine.Interpret(code)
	if err == nil || !strings.Contains(err.Error(), "extension 'guest' failed: boom") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessExtensionMissingBinaryErrorsAtUse(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	if err := os.RemoveAll(filepath.Join(root, "noxy_libs", "guest", "bin")); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "run 'noxy --get'") {
		t.Fatalf("missing binary must fail at use with a --get hint, got %v", err)
	}
}

func TestProcessExtensionUnpublishedPlatformErrorsAtUse(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	manifest := fmt.Sprintf(testProcessExtManifest, "plan9-mips", "guest-plan9-mips")
	if err := os.WriteFile(filepath.Join(root, "noxy_libs", "guest", "noxy_ext.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "has no binary for "+runtime.GOOS+"/"+runtime.GOARCH) || !strings.Contains(err.Error(), "plan9/mips") {
		t.Fatalf("got %v", err)
	}
}

func TestProcessExtensionSumMismatchRefusesLoad(t *testing.T) {
	root := t.TempDir()
	asset := writeProcessExtensionPackage(t, root)
	sum := "guest bin/" + asset + " sha256:0000000000000000000000000000000000000000000000000000000000000000\n"
	if err := os.WriteFile(filepath.Join(root, "noxy.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	machine := NewWithConfig(VMConfig{RootPath: root})
	err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\n"))
	if err == nil || !strings.Contains(err.Error(), "mismatch") || !strings.Contains(err.Error(), "bin/"+asset) {
		t.Fatalf("got %v", err)
	}
}

func TestProcessExtensionSumMatchLoads(t *testing.T) {
	root := t.TempDir()
	asset := writeProcessExtensionPackage(t, root)
	pkg := filepath.Join(root, "noxy_libs", "guest")
	sum := "guest noxy_ext.toml sha256:" + sha256File(t, filepath.Join(pkg, "noxy_ext.toml")) + "\n" +
		"guest bin/" + asset + " sha256:" + sha256File(t, filepath.Join(pkg, "bin", asset)) + "\n"
	if err := os.WriteFile(filepath.Join(root, "noxy.sum"), []byte(sum), 0o644); err != nil {
		t.Fatal(err)
	}
	got := captureVMSourceAtRoot(t, root, "use guest as g\ntest_report(g.add(1, 1))\n")
	if got.Int() != 2 {
		t.Fatalf("verified load must work: %#v", got)
	}
}
```

`machine.CloseExtensions` does not exist until Task 15 — for this task use `t.Cleanup(func() { machine.shared.CloseExtensions() })` only if you implement Task 15 first; otherwise leave the process to the test binary's exit (the guest exits on EOF when the test process dies). Recommended: implement Task 15's `CloseExtensions` methods **in this task** (they are ten lines) and keep Task 15 for the exit-path wiring.

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-trimpath=false go test ./internal/vm -run ProcessExtension -count=1`
Expected: FAIL — `ensureExtensionLoaded` reads `manifest.Wasm` (empty) and fails with a read error, not the expected messages.

- [ ] **Step 3: Implement**

In `internal/vm/extensions.go`, add `"runtime"` to the imports and replace everything from `wasmPath := filepath.Join(dir, manifest.Wasm)` down to `shared.Ext[dir] = module` with:

```go
	var backend ext.Backend
	switch manifest.Kind {
	case ext.KindProcess:
		backend, err = vm.loadProcessBackend(dir, manifest, manifestData)
	default:
		backend, err = vm.loadWasmBackend(dir, manifest, manifestData)
	}
	if err != nil {
		return err
	}
	for i, exp := range manifest.Exports {
		index := i
		sig := value.NativeSignature{
			Arity:      len(exp.Params),
			Params:     make([]value.ParamInfo, len(exp.Params)),
			ReturnType: signatureTypeName(exp.Returns),
		}
		for j, p := range exp.Params {
			sig.Params[j] = value.ParamInfo{TypeName: signatureTypeName(p)}
		}
		vm.DefineContextualNativeWithSignature(exp.Name, sig,
			func(_ value.NativeContext, args []value.Value) (value.Value, error) {
				return backend.Call(context.Background(), index, args)
			})
	}
	shared.Ext[dir] = backend
```

Add the two loaders after `ensureExtensionLoaded`:

```go
// loadWasmBackend e o caminho do M1: le o .wasm, verifica o hash, carrega
// no wazero.
func (vm *VM) loadWasmBackend(dir string, manifest *ext.Manifest, manifestData []byte) (ext.Backend, error) {
	wasmBytes, err := os.ReadFile(filepath.Join(dir, manifest.Wasm))
	if err != nil {
		return nil, fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	if err := vm.verifyExtensionSum(dir, manifest, manifestData, manifest.Wasm, wasmBytes); err != nil {
		return nil, err
	}
	// Sem campo diagOut no VM ainda: nx_log vai para stderr explicitamente
	// (achado de revisao) ate a spec de diagOut chegar.
	return ext.LoadModule(context.Background(), wasmBytes, manifest,
		ext.LoaderConfig{PermittedImports: extensionLoaderPermits, Log: os.Stderr})
}

// loadProcessBackend resolve o binario da plataforma em bin/, verifica o
// hash e constroi o backend SEM subir o processo (spec §4.1) — o start e da
// primeira chamada.
func (vm *VM) loadProcessBackend(dir string, manifest *ext.Manifest, manifestData []byte) (ext.Backend, error) {
	asset, ok := manifest.BinaryFor(runtime.GOOS, runtime.GOARCH)
	if !ok {
		return nil, fmt.Errorf("extension %q has no binary for %s/%s (published: %s)",
			manifest.Name, runtime.GOOS, runtime.GOARCH, strings.Join(manifest.PublishedPlatforms(), ", "))
	}
	binPath := filepath.Join(dir, "bin", asset)
	binBytes, err := os.ReadFile(binPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("extension %q: binary bin/%s not found — run 'noxy --get' to download it", manifest.Name, asset)
		}
		return nil, fmt.Errorf("extension %q: %w", manifest.Name, err)
	}
	if err := vm.verifyExtensionSum(dir, manifest, manifestData, "bin/"+asset, binBytes); err != nil {
		return nil, err
	}
	absPath, err := filepath.Abs(binPath)
	if err != nil {
		return nil, err
	}
	return ext.NewProcess(manifest, ext.ProcessConfig{Path: absPath, NoxyVersion: version.Version, Log: os.Stderr}), nil
}
```

Generalize `verifyExtensionSum`: signature `func (vm *VM) verifyExtensionSum(dir string, manifest *ext.Manifest, manifestData []byte, artifactName string, artifact []byte) error`; inside, replace `sums.Lookup(pkg, manifest.Wasm)` with `sums.Lookup(pkg, artifactName)`, `wantWasm/hasWasm` with `wantArtifact/hasArtifact`, `sha256.Sum256(wasmBytes)` with `sha256.Sum256(artifact)`, and the mismatch message's `manifest.Wasm` with `artifactName`. Update its doc comment: "confere o hash do artefato que o backend vai executar (`.wasm` ou `bin/<asset>`)".

Add to the same file (used by the tests now, wired to exit paths in Task 15):

```go
// CloseExtensions encerra todo backend carregado (spec §4.5): plugins por
// processo recebem EOF e sao mortos apos a carencia; modulos wasm fecham o
// runtime. Idempotente; chamado em todo caminho de saida do hospedeiro.
func (s *SharedState) CloseExtensions() {
	s.ExtMu.Lock()
	defer s.ExtMu.Unlock()
	for _, backend := range s.Ext {
		_ = backend.Close(context.Background())
	}
}

func (vm *VM) CloseExtensions() { vm.shared.CloseExtensions() }
```

- [ ] **Step 4: Run the VM extension tests (wasm and process)**

Run: `GOFLAGS=-trimpath=false go test ./internal/vm -run 'Extension' -count=1`
Expected: PASS — the five wasm e2e tests keep passing (the verification path is shared) and the six process tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/extensions.go internal/vm/process_extensions_e2e_test.go
git commit -m "feat(vm): extensao kind = \"process\" no use — binario por plataforma, hash em noxy.sum, backend lazy (issue #80)"
```

---

### Task 15: `CloseExtensions` on every exit path

**Files:**
- Modify: `cmd/noxy/main.go` (`runWithConfig` after `machine := vm.NewWithConfig(...)`; `runREPL` after its `machine := ...`), `internal/vm/builtins_sys.go` (`sys_exit`)
- Test: `internal/vm/process_extensions_e2e_test.go` (append), `cmd/noxy/process_exit_test.go`

**Interfaces:**
- Consumes: `(*VM).CloseExtensions`, `exttest.ProcessAlive`, `exttest.BuildProcessGuest`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/vm/process_extensions_e2e_test.go`:

```go
func TestCloseExtensionsStopsTheProcess(t *testing.T) {
	root := t.TempDir()
	writeProcessExtensionPackage(t, root)
	machine := NewWithConfig(VMConfig{RootPath: root})
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		captured = args[0]
		return value.NewNull()
	})
	if err := machine.Interpret(compileVMSourceAtRoot(t, root, "use guest as g\ntest_report(g.pid())\n")); err != nil {
		t.Fatal(err)
	}
	pid := int(captured.Int())
	if !exttest.ProcessAlive(pid) {
		t.Fatal("guest must be alive after the call")
	}
	machine.CloseExtensions()
	deadline := time.Now().Add(3 * time.Second)
	for exttest.ProcessAlive(pid) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if exttest.ProcessAlive(pid) {
		t.Fatal("CloseExtensions must stop the plugin")
	}
	machine.CloseExtensions() // idempotente
}
```

Add `"time"` to that file's imports.

```go
// cmd/noxy/process_exit_test.go
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

	"noxy-vm/internal/ext/exttest"
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
		"noxy_ext.toml":            []byte(manifest),
		"guest.nx":                 []byte("func pid() -> int\n    return guest_pid()\nend\n"),
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
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("script must print the guest pid: %v", err)
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
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-trimpath=false go test ./cmd/noxy -run SysExitClosesProcessExtensions -count=1`
Expected: FAIL — the guest survives (`sys_exit` calls `os.Exit` without closing).

- [ ] **Step 3: Wire the exit paths**

`cmd/noxy/main.go`, in `runWithConfig` right after `machine := vm.NewWithConfig(vm.VMConfig{RootPath: rootPath})`:

```go
	// Extensoes por processo precisam de EOF/kill na saida (spec §4.5); o
	// defer cobre sucesso, erro de runtime e o desenrolar de um panic.
	defer machine.CloseExtensions()
```

In `runREPL` right after `machine := vm.NewWithConfig(vm.VMConfig{RootPath: "."})`: the same `defer machine.CloseExtensions()`.

`internal/vm/builtins_sys.go`, in `sys_exit` before `os.Exit(code)`:

```go
		// os.Exit nao roda defers: fecha os plugins por processo aqui, senao
		// eles ficariam orfaos ate perceberem o EOF (spec §4.5).
		vm.shared.CloseExtensions()
```

- [ ] **Step 4: Run**

Run: `GOFLAGS=-trimpath=false go test ./cmd/noxy ./internal/vm -run 'SysExitCloses|CloseExtensions|Extension' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/noxy/main.go cmd/noxy/process_exit_test.go internal/vm/builtins_sys.go internal/vm/process_extensions_e2e_test.go
git commit -m "feat(vm): CloseExtensions em todo caminho de saida — runFile, REPL, sys_exit (issue #80)"
```

---

## Part D — Package manager

### Task 16: Release helpers — base URL, `checksums.txt`, newest tag

**Files:**
- Create: `internal/pkgmanager/release.go`
- Test: `internal/pkgmanager/release_test.go`

**Interfaces:**
- Produces: `func ReleaseBaseURL(repoPath, tag string) (string, error)` (`github.com/u/r` + `v1.2.0` → `https://github.com/u/r/releases/download/v1.2.0/`); `func ParseChecksums(data []byte) (map[string]string, error)` (`sha256sum` format, `*name` accepted); `func newestSemverTag(lsRemote string) (string, bool)`; `func gitLsRemoteTags(gitURL string) (string, error)`; seams `var gitURLFor = toGitURL`, `var releaseBaseURL = ReleaseBaseURL`, `var resolveNewestTag func(gitURL string) (string, error)`, `var httpClient = &http.Client{Timeout: 60 * time.Second}`; `func toGitURL(repoURL string) string`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/pkgmanager/release_test.go
package pkgmanager

import (
	"strings"
	"testing"
)

func TestReleaseBaseURL(t *testing.T) {
	got, err := ReleaseBaseURL("github.com/estevaofon/noxy_terminal", "v0.2.0")
	if err != nil || got != "https://github.com/estevaofon/noxy_terminal/releases/download/v0.2.0/" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := ReleaseBaseURL("github.com/estevaofon", "v0.2.0"); err == nil {
		t.Fatal("a path without a repo cannot have releases")
	}
	if _, err := ReleaseBaseURL("github.com/estevaofon/noxy_terminal", "HEAD"); err == nil {
		t.Fatal("HEAD is not a release tag")
	}
}

func TestParseChecksums(t *testing.T) {
	data := []byte("" +
		"0000000000000000000000000000000000000000000000000000000000000001  noxy-plugin-a-linux-amd64\n" +
		"0000000000000000000000000000000000000000000000000000000000000002 *noxy-plugin-a-windows-amd64.exe\n" +
		"\n# comment\n")
	sums, err := ParseChecksums(data)
	if err != nil {
		t.Fatal(err)
	}
	if sums["noxy-plugin-a-linux-amd64"] != strings.Repeat("0", 63)+"1" || sums["noxy-plugin-a-windows-amd64.exe"] != strings.Repeat("0", 63)+"2" {
		t.Fatalf("parsed: %v", sums)
	}
	if _, err := ParseChecksums([]byte("nothex  file\n")); err == nil {
		t.Fatal("a non-hex digest is malformed")
	}
	if _, err := ParseChecksums([]byte("0000000000000000000000000000000000000000000000000000000000000001\n")); err == nil {
		t.Fatal("a line without a file name is malformed")
	}
}

func TestNewestSemverTag(t *testing.T) {
	ls := "" +
		"aaa\trefs/tags/v0.9.1\n" +
		"bbb\trefs/tags/v0.10.0\n" +
		"ccc\trefs/tags/v0.10.0^{}\n" +
		"ddd\trefs/tags/experimental\n" +
		"eee\trefs/tags/v0.2.0\n"
	tag, ok := newestSemverTag(ls)
	if !ok || tag != "v0.10.0" {
		t.Fatalf("%q %v", tag, ok)
	}
	if _, ok := newestSemverTag("ddd\trefs/tags/experimental\n"); ok {
		t.Fatal("no semver tag → not found")
	}
	if tag, ok := newestSemverTag("x\trefs/tags/1.2.3\n"); !ok || tag != "1.2.3" {
		t.Fatalf("tags without the v prefix count too: %q %v", tag, ok)
	}
}

func TestToGitURL(t *testing.T) {
	if got := toGitURL("github.com/a/b"); got != "https://github.com/a/b" {
		t.Fatal(got)
	}
	if got := toGitURL("git@github.com:a/b.git"); got != "git@github.com:a/b.git" {
		t.Fatal(got)
	}
	if got := toGitURL("https://x/y"); got != "https://x/y" {
		t.Fatal(got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/pkgmanager -run 'ReleaseBaseURL|ParseChecksums|NewestSemverTag|ToGitURL' -count=1`
Expected: FAIL — undefined functions.

- [ ] **Step 3: Implement**

```go
// internal/pkgmanager/release.go
package pkgmanager

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Costuras trocadas pelos testes (servidor httptest, repositorio local).
var (
	gitURLFor        = toGitURL
	releaseBaseURL   = ReleaseBaseURL
	resolveNewestTag = func(gitURL string) (string, error) {
		out, err := gitLsRemoteTags(gitURL)
		if err != nil {
			return "", err
		}
		tag, ok := newestSemverTag(out)
		if !ok {
			return "", fmt.Errorf("no semver tag found — process extensions are installed from a tagged release")
		}
		return tag, nil
	}
	httpClient = &http.Client{Timeout: 60 * time.Second}
)

func toGitURL(repoURL string) string {
	if strings.HasPrefix(repoURL, "http") || strings.HasPrefix(repoURL, "git@") {
		return repoURL
	}
	return "https://" + repoURL
}

// ReleaseBaseURL deriva a URL dos assets de uma release (spec §8.2):
// github.com/<user>/<repo> + tag → .../releases/download/<tag>/. Forges com
// o mesmo layout funcionam pela mesma regra.
func ReleaseBaseURL(repoPath, tag string) (string, error) {
	path := strings.TrimPrefix(strings.TrimPrefix(repoPath, "https://"), "http://")
	parts := strings.Split(strings.TrimSuffix(path, "/"), "/")
	if len(parts) < 3 || tag == "" || tag == "HEAD" {
		return "", fmt.Errorf("cannot derive a release URL from %q@%q", repoPath, tag)
	}
	return "https://" + strings.Join(parts[:3], "/") + "/releases/download/" + tag + "/", nil
}

// ParseChecksums le o formato do sha256sum: "<hex>  <nome>" por linha
// ("*nome" do modo binario e aceito).
func ParseChecksums(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("checksums.txt: malformed line %q", line)
		}
		digest := strings.ToLower(fields[0])
		if raw, err := hex.DecodeString(digest); err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("checksums.txt: %q is not a sha256 digest", fields[0])
		}
		out[strings.TrimPrefix(fields[1], "*")] = digest
	}
	return out, nil
}

var semverTagRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

// newestSemverTag escolhe a maior tag semver numa saida de
// `git ls-remote --tags` (linhas "<sha>\trefs/tags/<tag>"; "^{}" e a tag
// anotada resolvida — mesma versao).
func newestSemverTag(lsRemote string) (string, bool) {
	var best string
	var bestV [3]int
	found := false
	for _, line := range strings.Split(lsRemote, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		tag := strings.TrimSuffix(strings.TrimPrefix(fields[1], "refs/tags/"), "^{}")
		m := semverTagRE.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		var v [3]int
		for i := 0; i < 3; i++ {
			v[i], _ = strconv.Atoi(m[i+1])
		}
		if !found || v[0] > bestV[0] || (v[0] == bestV[0] && (v[1] > bestV[1] || (v[1] == bestV[1] && v[2] > bestV[2]))) {
			best, bestV, found = tag, v, true
		}
	}
	return best, found
}

func gitLsRemoteTags(gitURL string) (string, error) {
	out, err := exec.Command("git", "ls-remote", "--tags", gitURL).Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote --tags %s: %w", gitURL, err)
	}
	return string(out), nil
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/pkgmanager -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/release.go internal/pkgmanager/release_test.go
git commit -m "feat(pkgmanager): URL de release, checksums.txt e tag semver mais nova (issue #80)"
```

---

### Task 17: Download one platform asset, verify it, record all hashes

**Files:**
- Modify: `internal/pkgmanager/release.go` (append), `internal/pkgmanager/manager.go` (append `recordProcessSums`)
- Test: `internal/pkgmanager/release_fetch_test.go`

**Interfaces:**
- Produces: `func fetchProcessBinaries(client *http.Client, baseURL string, manifest *ext.Manifest, targetDir, goos, goarch string, out io.Writer) (map[string]string, error)` — returns `asset → sha256 hex` for **every** `[binaries]` entry, having downloaded and verified only the `goos/goarch` one into `targetDir/bin/<asset>` (0755 on POSIX); `func recordProcessSums(root, localPath string, manifestData []byte, binaries map[string]string) error` — writes `<pkg> noxy_ext.toml` and `<pkg> bin/<asset>` lines.
- Consumes: Task 16, `ext.Manifest.Binaries/BinaryFor/PublishedPlatforms`, `SumFile`.

- [ ] **Step 1: Write the failing tests**

```go
// internal/pkgmanager/release_fetch_test.go
package pkgmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"noxy-vm/internal/ext"
)

const fetchManifest = `
name = "guest"
abi = 1
kind = "process"

[binaries]
linux-amd64 = "guest-linux-amd64"
windows-amd64 = "guest-windows-amd64.exe"

[[export]]
name = "guest_noop"
params = []
returns = "void"
`

func hexSum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// serveRelease publica files em <url>/rel/<nome>.
func serveRelease(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/rel/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func releaseFiles(linux, windows []byte) map[string][]byte {
	checksums := hexSum(linux) + "  guest-linux-amd64\n" + hexSum(windows) + "  guest-windows-amd64.exe\n"
	return map[string][]byte{
		"guest-linux-amd64":       linux,
		"guest-windows-amd64.exe": windows,
		"checksums.txt":           []byte(checksums),
	}
}

func fetchManifestParsed(t *testing.T) *ext.Manifest {
	t.Helper()
	m, err := ext.ParseManifest([]byte(fetchManifest))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFetchProcessBinariesDownloadsOnlyThePlatformAsset(t *testing.T) {
	linux, windows := []byte("linux bits"), []byte("windows bits")
	srv := serveRelease(t, releaseFiles(linux, windows))
	dir := t.TempDir()
	sums, err := fetchProcessBinaries(srv.Client(), srv.URL+"/rel/", fetchManifestParsed(t), dir, "linux", "amd64", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if sums["guest-linux-amd64"] != hexSum(linux) || sums["guest-windows-amd64.exe"] != hexSum(windows) {
		t.Fatalf("all published hashes must be returned: %v", sums)
	}
	got, err := os.ReadFile(filepath.Join(dir, "bin", "guest-linux-amd64"))
	if err != nil || string(got) != "linux bits" {
		t.Fatalf("asset on disk: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bin", "guest-windows-amd64.exe")); !os.IsNotExist(err) {
		t.Fatal("only the platform asset is downloaded")
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(filepath.Join(dir, "bin", "guest-linux-amd64"))
		if info.Mode()&0o111 == 0 {
			t.Fatal("POSIX asset must be executable")
		}
	}
}

func TestFetchProcessBinariesErrors(t *testing.T) {
	linux, windows := []byte("linux bits"), []byte("windows bits")
	m := fetchManifestParsed(t)

	srv := serveRelease(t, releaseFiles(linux, windows))
	_, err := fetchProcessBinaries(srv.Client(), srv.URL+"/rel/", m, t.TempDir(), "darwin", "arm64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "no binary for darwin/arm64") || !strings.Contains(err.Error(), "linux/amd64") {
		t.Fatalf("unpublished platform: %v", err)
	}

	files := releaseFiles(linux, windows)
	files["checksums.txt"] = []byte(hexSum(linux) + "  guest-linux-amd64\n")
	srv2 := serveRelease(t, files)
	_, err = fetchProcessBinaries(srv2.Client(), srv2.URL+"/rel/", m, t.TempDir(), "linux", "amd64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "does not list \"guest-windows-amd64.exe\"") {
		t.Fatalf("asset missing from checksums: %v", err)
	}

	files = releaseFiles(linux, windows)
	files["guest-linux-amd64"] = []byte("tampered")
	srv3 := serveRelease(t, files)
	dir := t.TempDir()
	_, err = fetchProcessBinaries(srv3.Client(), srv3.URL+"/rel/", m, dir, "linux", "amd64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("corrupted asset: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "bin"))
	if len(entries) != 0 {
		t.Fatalf("a corrupted download leaves nothing behind, found %v", entries)
	}

	files = releaseFiles(linux, windows)
	delete(files, "guest-linux-amd64")
	srv4 := serveRelease(t, files)
	_, err = fetchProcessBinaries(srv4.Client(), srv4.URL+"/rel/", m, t.TempDir(), "linux", "amd64", io.Discard)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("missing asset names the status: %v", err)
	}
}

func TestRecordProcessSums(t *testing.T) {
	root := t.TempDir()
	err := recordProcessSums(root, "github_com/acme/guest", []byte(fetchManifest), map[string]string{
		"guest-linux-amd64":       strings.Repeat("a", 64),
		"guest-windows-amd64.exe": strings.Repeat("b", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(SumFilePath(root))
	for _, want := range []string{
		"github_com/acme/guest noxy_ext.toml sha256:" + hexSum([]byte(fetchManifest)),
		"github_com/acme/guest bin/guest-linux-amd64 sha256:" + strings.Repeat("a", 64),
		"github_com/acme/guest bin/guest-windows-amd64.exe sha256:" + strings.Repeat("b", 64),
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("noxy.sum missing %q:\n%s", want, data)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/pkgmanager -run 'FetchProcess|RecordProcessSums' -count=1`
Expected: FAIL — undefined `fetchProcessBinaries`, `recordProcessSums`.

- [ ] **Step 3: Implement** (append to `release.go`; add `"crypto/sha256"`, `"io"`, `"os"`, `"path/filepath"`, `"noxy-vm/internal/ext"` to its imports)

```go
// fetchProcessBinaries baixa checksums.txt e SO o asset da plataforma para
// targetDir/bin/ (spec §8.1), verifica o sha256 e devolve os hashes de
// TODOS os assets do manifesto — e o que faz o noxy.sum valer para o
// colega no macOS e para o Lambda no Linux.
func fetchProcessBinaries(client *http.Client, baseURL string, manifest *ext.Manifest, targetDir, goos, goarch string, out io.Writer) (map[string]string, error) {
	checksumsData, err := httpGet(client, baseURL+"checksums.txt")
	if err != nil {
		return nil, err
	}
	checksums, err := ParseChecksums(checksumsData)
	if err != nil {
		return nil, err
	}
	sums := map[string]string{}
	for _, asset := range manifest.Binaries {
		digest, ok := checksums[asset]
		if !ok {
			return nil, fmt.Errorf("checksums.txt does not list %q, which the manifest promises", asset)
		}
		sums[asset] = digest
	}
	asset, ok := manifest.BinaryFor(goos, goarch)
	if !ok {
		return nil, fmt.Errorf("no binary for %s/%s (published: %s)", goos, goarch, strings.Join(manifest.PublishedPlatforms(), ", "))
	}
	fmt.Fprintf(out, "Downloading %s...\n", asset)
	if err := downloadAsset(client, baseURL+asset, filepath.Join(targetDir, "bin", asset), sums[asset], goos); err != nil {
		return nil, err
	}
	return sums, nil
}

func httpGet(client *http.Client, url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// downloadAsset grava em <dest>.part com hash em fluxo, confere e renomeia;
// nada fica para tras em caso de erro.
func downloadAsset(client *http.Client, url, dest, wantHex, goos string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	file, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hasher), resp.Body)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(part)
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	if got != wantHex {
		_ = os.Remove(part)
		return fmt.Errorf("checksum mismatch for %s: checksums.txt has sha256:%s, download has sha256:%s", filepath.Base(dest), wantHex, got)
	}
	if goos != "windows" {
		if err := os.Chmod(part, 0o755); err != nil {
			_ = os.Remove(part)
			return err
		}
	}
	return os.Rename(part, dest)
}
```

Append to `manager.go`:

```go
// recordProcessSums grava manifesto + bin/<asset> de TODAS as plataformas
// publicadas (spec §8.1, passo 6).
func recordProcessSums(root, localPath string, manifestData []byte, binaries map[string]string) error {
	sums, err := ParseSumFile(SumFilePath(root))
	if err != nil {
		return err
	}
	pkg := strings.ReplaceAll(localPath, "\\", "/")
	sums.Set(pkg, "noxy_ext.toml", sha256Hex(manifestData))
	for asset, digest := range binaries {
		sums.Set(pkg, "bin/"+asset, digest)
	}
	return sums.Save(SumFilePath(root))
}
```

- [ ] **Step 4: Run**

Run: `go test ./internal/pkgmanager -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/release.go internal/pkgmanager/manager.go internal/pkgmanager/release_fetch_test.go
git commit -m "feat(pkgmanager): download verificado do asset da plataforma e noxy.sum com todos os assets (issue #80)"
```

---

### Task 18: `--get` flow — fresh clone, tag resolution, process branch

**Files:**
- Modify: `internal/pkgmanager/manager.go` (`downloadPackage` rewritten; helpers `splitPackageArg`, `localPackagePath`, `manifestKindAt`, `readManifest`)
- Test: `internal/pkgmanager/manager_get_test.go`

**Interfaces:**
- Consumes: Tasks 16–17 seams; `gitClone`, `gitCheckout`, `updateModFile`, `RecordExtensionSums` (unchanged).
- Behaviour (§8.1): fresh clone into a temp sibling, swap at the end; no version + `kind = "process"` → newest semver tag, printed and recorded in `noxy.mod`; invalid `noxy_ext.toml` fails `--get`; process kind → assets + `noxy.sum`; capabilities printed as `<name> declares: a, b`.

- [ ] **Step 1: Write the failing integration test**

```go
// internal/pkgmanager/manager_get_test.go
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
		asset:             mine,
		"guest-plan9-mips": other,
		"checksums.txt":   []byte(hexSum(mine) + "  " + asset + "\n" + hexSum(other) + "  guest-plan9-mips\n"),
	})

	project := filepath.Join(work, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)
	prevGit, prevRel, prevTag := gitURLFor, releaseBaseURL, resolveNewestTag
	gitURLFor = func(string) string { return repo }
	releaseBaseURL = func(string, string) (string, error) { return srv.URL + "/rel/", nil }
	resolveNewestTag = func(string) (string, error) { return "v0.1.0", nil }
	t.Cleanup(func() { gitURLFor, releaseBaseURL, resolveNewestTag = prevGit, prevRel, prevTag })

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
		"github_com/acme/guest noxy_ext.toml sha256:" + hexSum([]byte(manifest)),
		"github_com/acme/guest bin/" + asset + " sha256:" + hexSum(mine),
		"github_com/acme/guest bin/guest-plan9-mips sha256:" + hexSum(other),
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/pkgmanager -run 'GetProcessExtension|GetFailsWithout' -count=1`
Expected: FAIL — the current flow clones straight into `noxy_libs`, never downloads assets, and `git clone https://github.com/acme/guest` is attempted (the `gitURLFor` seam is not used yet).

- [ ] **Step 3: Rewrite `downloadPackage`** (replace the whole function; keep `Get`, `gitClone`, `gitPull` (now unused — delete it), `gitCheckout`, `updateModFile`, `RecordExtensionSums`, `sha256Hex`; add `"runtime"` and `"noxy-vm/internal/ext"` is already imported)

```go
func splitPackageArg(pkgArg string) (repoURL, version string) {
	parts := strings.SplitN(pkgArg, "@", 2)
	if len(parts) == 2 && parts[1] != "" {
		return parts[0], parts[1]
	}
	return parts[0], "HEAD"
}

// localPackagePath: github.com/user/repo → github_com/user/repo.
func localPackagePath(repoURL string) string {
	parts := strings.Split(repoURL, "/")
	parts[0] = strings.ReplaceAll(parts[0], ".", "_")
	return strings.Join(parts, "/")
}

// readManifest devolve (nil, nil, nil) quando o pacote nao e uma extensao;
// um manifesto presente mas invalido falha o --get — binarios dependem
// dele, e um typo nao pode virar "pacote sem extensao" em silencio.
func readManifest(dir string) (*ext.Manifest, []byte, error) {
	data, err := os.ReadFile(filepath.Join(dir, "noxy_ext.toml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, err
	}
	manifest, err := ext.ParseManifest(data)
	if err != nil {
		return nil, nil, err
	}
	return manifest, data, nil
}

func manifestKindAt(dir string) string {
	manifest, _, err := readManifest(dir)
	if err != nil || manifest == nil {
		return ""
	}
	return manifest.Kind
}

func downloadPackage(pkgArg string, isRoot bool, visited map[string]bool) error {
	repoURL, version := splitPackageArg(pkgArg)
	cacheKey := repoURL + "@" + version
	if visited[cacheKey] {
		return nil
	}
	visited[cacheKey] = true

	localPath := localPackagePath(repoURL)
	targetDir := filepath.Join(NoxyLibsDir, filepath.FromSlash(localPath))
	if isRoot {
		fmt.Printf("Getting package %s...\n", pkgArg)
	} else {
		fmt.Printf("Getting dependency %s...\n", pkgArg)
	}

	// Clone fresco num diretorio temporario irmao; o destino so e tocado no
	// fim (spec §8.1): o antigo "existe → git pull" nao atualizava nada
	// depois de remover o .git, e com binarios em disco um diretorio velho
	// guardaria um asset velho.
	if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
		return err
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(targetDir), ".get-"+filepath.Base(targetDir)+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := gitClone(gitURLFor(repoURL), tmpDir); err != nil {
		return fmt.Errorf("failed to clone package: %w", err)
	}

	// Sem versao, uma extensao por processo precisa de uma tag: os assets
	// pendem de uma release (spec §8.1, passo 2).
	resolved := version
	if version == "HEAD" && manifestKindAt(tmpDir) == ext.KindProcess {
		tag, err := resolveNewestTag(gitURLFor(repoURL))
		if err != nil {
			return fmt.Errorf("%s: %w", repoURL, err)
		}
		fmt.Printf("Resolved %s to %s\n", repoURL, tag)
		resolved = tag
	}
	if resolved != "HEAD" {
		if err := gitCheckout(tmpDir, resolved); err != nil {
			return fmt.Errorf("failed to checkout version %s: %w", resolved, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(tmpDir, ".git")); err != nil {
		fmt.Printf("Warning: failed to remove .git directory: %s\n", err)
	}

	manifest, manifestData, err := readManifest(tmpDir)
	if err != nil {
		return err
	}
	var binarySums map[string]string
	if manifest != nil && manifest.Kind == ext.KindProcess {
		base, err := releaseBaseURL(repoURL, resolved)
		if err != nil {
			return err
		}
		binarySums, err = fetchProcessBinaries(httpClient, base, manifest, tmpDir, runtime.GOOS, runtime.GOARCH, os.Stdout)
		if err != nil {
			return fmt.Errorf("%s@%s: %w", repoURL, resolved, err)
		}
	}

	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}
	if err := os.Rename(tmpDir, targetDir); err != nil {
		return fmt.Errorf("failed to install package: %w", err)
	}

	if manifest != nil {
		var sumErr error
		if manifest.Kind == ext.KindProcess {
			sumErr = recordProcessSums(".", localPath, manifestData, binarySums)
		} else {
			sumErr = RecordExtensionSums(".", targetDir, localPath)
		}
		if sumErr != nil {
			fmt.Printf("Warning: failed to record noxy.sum entries: %s\n", sumErr)
		}
		if len(manifest.Capabilities) != 0 {
			fmt.Printf("%s declares: %s\n", manifest.Name, strings.Join(manifest.Capabilities, ", "))
		}
	}

	if isRoot {
		if err := updateModFile(repoURL, resolved); err != nil {
			fmt.Printf("Warning: failed to update noxy.mod: %s\n", err)
		}
	}

	pkgModPath := filepath.Join(targetDir, "noxy.mod")
	if _, err := os.Stat(pkgModPath); err == nil {
		config, err := ParseModFile(pkgModPath)
		if err != nil {
			fmt.Printf("Warning: failed to parse %s: %s\n", pkgModPath, err)
		} else {
			for depPkg, depVer := range config.Require {
				depArg := depPkg
				if depVer != "" {
					depArg = depPkg + "@" + depVer
				}
				if err := downloadPackage(depArg, false, visited); err != nil {
					fmt.Printf("Warning: failed to download dependency %s: %s\n", depArg, err)
				}
			}
		}
	}

	if isRoot {
		fmt.Println("Done.")
	}
	return nil
}
```

`gitClone` must clone into an **existing empty** directory — `git clone <url> <dir>` accepts that; keep it as is. Delete `gitPull` (no caller left). Update the `RecordExtensionSums` doc comment: an invalid manifest is now rejected earlier by `readManifest`, so its "manifesto invalido apenas pula" sentence no longer applies — replace it with "manifesto ausente pula o registro; um invalido ja falhou em readManifest".

- [ ] **Step 4: Run**

Run: `go test ./internal/pkgmanager -count=1`
Expected: PASS (both integration tests plus the earlier ones).

- [ ] **Step 5: Commit**

```bash
git add internal/pkgmanager/manager.go internal/pkgmanager/manager_get_test.go
git commit -m "feat(pkgmanager): --get com clone fresco, tag mais nova e assets de extensao por processo (issue #80)"
```

---

## Part E — Deprecation, benchmarks, docs, version

### Task 19: `sys_load_plugin` deprecation warning

**Files:**
- Modify: `internal/vm/builtins_sys.go` (`sys_load_plugin` native, imports `sync/atomic`)
- Test: `internal/vm/builtins_sys_plugin_test.go`

**Interfaces:**
- Produces: `var pluginDeprecationWarned atomic.Bool`; `const pluginDeprecationWarning` (text verbatim from §10.1).

- [ ] **Step 1: Write the failing test**

```go
// internal/vm/builtins_sys_plugin_test.go
package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

// sys_load_plugin avisa UMA vez por processo que esta deprecado (spec
// §10.1); o comando inexistente faz a chamada devolver false como antes.
func TestSysLoadPluginWarnsDeprecationOnce(t *testing.T) {
	pluginDeprecationWarned.Store(false)
	machine := New()
	load := func() {
		callBuiltin(t, machine, "sys_load_plugin", value.NewString("nope"), value.NewString("noxy-plugin-does-not-exist"))
	}
	first := captureConcurrencyStderr(t, load)
	if !strings.Contains(first, "warning: sys_load_plugin is deprecated since v0.23.0 and will be removed in v0.25.0") {
		t.Fatalf("first call must warn, got %q", first)
	}
	second := captureConcurrencyStderr(t, load)
	if strings.Contains(second, "deprecated") {
		t.Fatalf("warn once per process, got %q", second)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `GOFLAGS=-trimpath=false go test ./internal/vm -run SysLoadPluginWarns -count=1`
Expected: FAIL — `undefined: pluginDeprecationWarned`.

- [ ] **Step 3: Implement**

At package level in `builtins_sys.go` (add `"sync/atomic"` to the imports):

```go
// pluginDeprecationWarned: um unico aviso por processo (spec 2026-08-29
// §10.1). sys_load_plugin, internal/plugin e compiler.PluginNativeNames
// saem juntos na v0.25.0.
var pluginDeprecationWarned atomic.Bool

const pluginDeprecationWarning = "warning: sys_load_plugin is deprecated since v0.23.0 and will be removed in v0.25.0; publish the plugin as a kind = \"process\" extension (docs/EXTENSIONS.md)"
```

As the first statement inside the `sys_load_plugin` closure (before `machine, contextErr := nativeVM(context)`):

```go
		if pluginDeprecationWarned.CompareAndSwap(false, true) {
			fmt.Fprintln(os.Stderr, pluginDeprecationWarning)
		}
```

- [ ] **Step 4: Run**

Run: `GOFLAGS=-trimpath=false go test ./internal/vm -run 'SysLoadPlugin|Plugin' -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/vm/builtins_sys.go internal/vm/builtins_sys_plugin_test.go
git commit -m "feat(vm): sys_load_plugin avisa deprecacao (remocao na v0.25.0) (issue #80)"
```

---

### Task 20: Benchmarks

**Files:**
- Create: `internal/ext/process_bench_test.go`

**Interfaces:**
- Consumes: `guestManifest`, `fnNoop`, `fnBytes`, `fnSleep` (Task 12).
- Produces the four benchmarks of §11; their numbers go into `docs/EXTENSIONS.md` in Task 21.

- [ ] **Step 1: Write the benchmarks**

```go
// internal/ext/process_bench_test.go
package ext

import (
	"context"
	"io"
	"strings"
	"testing"

	"noxy-vm/internal/ext/exttest"
	"noxy-vm/internal/value"
)

// benchGuest sobe o guest fora da medicao (o start e da primeira chamada).
func benchGuest(b *testing.B, concurrency string) *Process {
	b.Helper()
	path := exttest.BuildProcessGuest(b)
	p := NewProcess(guestManifest(b, path, concurrency, ""), ProcessConfig{Path: path, NoxyVersion: "bench", Log: io.Discard})
	b.Cleanup(func() { _ = p.Close(context.Background()) })
	if _, err := p.Call(context.Background(), fnNoop, nil); err != nil {
		b.Fatal(err)
	}
	return p
}

// Spec §11: quadro vazio, 1 KB, 1 MB — ao lado dos numeros do wasm em
// docs/EXTENSIONS.md.
func BenchmarkProcessRoundTripEmpty(b *testing.B) {
	p := benchGuest(b, "single")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := p.Call(context.Background(), fnNoop, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func benchBytes(b *testing.B, size int) {
	p := benchGuest(b, "single")
	payload := value.NewBytes(strings.Repeat("x", size))
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got, err := p.Call(context.Background(), fnBytes, []value.Value{payload})
		if err != nil || len(got.Obj.(string)) != size {
			b.Fatalf("round trip: %v", err)
		}
	}
}

func BenchmarkProcessRoundTrip1KB(b *testing.B) { benchBytes(b, 1<<10) }
func BenchmarkProcessRoundTrip1MB(b *testing.B) { benchBytes(b, 1<<20) }

// Spec §11, aceitacao: com um handler que dorme 1 ms, "concurrent" precisa
// render pelo menos 4x o throughput de "single" (prova da multiplexacao).
// RunParallel usa GOMAXPROCS goroutines — rode com GOMAXPROCS >= 8.
func BenchmarkProcessConcurrent8(b *testing.B) {
	for _, mode := range []string{"single", "concurrent"} {
		b.Run(mode, func(b *testing.B) {
			p := benchGuest(b, mode)
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if _, err := p.Call(context.Background(), fnSleep, []value.Value{value.NewInt(1)}); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}
```

- [ ] **Step 2: Run them and keep the output**

Run: `GOFLAGS=-trimpath=false go test ./internal/ext -run '^$' -bench 'BenchmarkProcess' -benchtime=2s -count=1`
Expected: four result lines; on Linux amd64 the empty round trip is under 100 µs and 1 KB under 120 µs, 1 MB under 10 ms, and `Concurrent8/concurrent` ns/op ≤ ¼ of `Concurrent8/single` (§11 acceptance — gate on the CI Linux runner; Windows numbers are recorded, not gated). Save the output for Task 21 and the PR body.

- [ ] **Step 3: Commit**

```bash
git add internal/ext/process_bench_test.go
git commit -m "bench(ext): ida-e-volta vazia, 1 KB, 1 MB e multiplexacao do backend de processo (issue #80)"
```

---

### Task 21: Docs and CI

**Files:**
- Modify: `docs/EXTENSIONS.md` (new section), `docs/PACKAGE_MANAGER.md` (`--get` for process extensions, fresh clone), `AGENTS.md` (layout table rows, "Extensões e plugins" paragraph), `.github/workflows/network-deadlines.yml` (SDK test step)
- Create: `sdk/noxyplugin/README.md`, `sdk/noxyplugin/release/build.sh`, `sdk/noxyplugin/release/github-release.yml`

- [ ] **Step 1: `docs/EXTENSIONS.md` — retitle and add the process section**

Change the H1 to `# Noxy Extensions (wasm and process)`, keep everything that exists under a new `## WASM extensions (tier A)` heading placed right after the scope paragraph, and append:

````markdown
## Process extensions (tier B)

A process extension is an executable that speaks `noxy-plugin/1` over its
stdin/stdout. It is the primary mechanism for I/O, OS access, drivers and
SDK bindings (issue #110): the author cross-compiles one binary per
platform with plain Go, the user never compiles. Design and protocol
contract: `docs/superpowers/specs/2026-08-29-process-extensions-design.md`.

### Package layout

```
noxy_terminal/
├── noxy_ext.toml          # kind = "process"
├── terminal.nx            # typed wrapper — same idiom as wasm
└── bin/                   # created by `noxy --get`: this platform's asset only
    └── noxy-plugin-terminal-windows-amd64.exe
```

`noxy --get github.com/you/noxy_terminal@v0.2.0` clones the repo, downloads
`checksums.txt` and the asset for your OS/arch from the release, verifies
it, and records the hashes of **every** published asset in `noxy.sum`, so
the committed lockfile is valid for a teammate on another OS. Without a
version, `--get` resolves the newest semver tag. No asset for your
platform is an error at `--get` time — never at runtime, and there is no
compile-from-source fallback.

### Manifest reference (process keys)

```toml
name = "terminal"
abi = 1
kind = "process"                # "wasm" (default) | "process"
min_noxy = "0.23.0"
concurrency = "concurrent"      # single (default) | stateless | concurrent (process only)
capabilities = ["tty"]          # declarative: shown by --get, never enforced
call_timeout_ms = 30000         # default 30000; 0 = no deadline
handshake_timeout_ms = 5000     # default 5000
restart = false                 # respawn after a crash; needs concurrency = "stateless"

[binaries]                      # "<GOOS>-<GOARCH>" = release asset name; windows-* end in .exe
linux-amd64   = "noxy-plugin-terminal-linux-amd64"
darwin-arm64  = "noxy-plugin-terminal-darwin-arm64"
windows-amd64 = "noxy-plugin-terminal-windows-amd64.exe"

[[export]]
name = "terminal_read_key"      # same rules as wasm: "<name>_" prefix, typed params/returns
params = []
returns = "string"
timeout_ms = 0                  # per-export override; 0 = blocks as long as it likes
```

`wasm`, `memory_max_mb` are rejected under `kind = "process"`; `binaries`,
the timeouts, `restart`, `timeout_ms` and `concurrency = "concurrent"` are
rejected under `kind = "wasm"`.

`single` lets one call in flight (the plugin may be single-threaded);
`stateless` multiplexes and forbids `stateful` exports; `concurrent`
multiplexes **and** allows handles, because there is one process and its
handles are process-wide — the mode an extension holding connections wants.

### Lifecycle

The process starts on the **first call** to any export, not at `use`
(`--smoke`-style runs that never call it need no binary at run time).
Every call runs under a deadline; on expiry the host sends CANCEL and
returns `extension 'x' timed out: <export> exceeded <N> ms` — the process
survives if it cancels within 1 s, otherwise it is killed and the extension
is poisoned like a wasm trap. A crash poisons the extension (`extension 'x'
is poisoned by an earlier trap`) unless `restart = true`. At exit the VM
closes the plugin's stdin (EOF) and kills it after 2 s if it lingers;
Linux adds `PDEATHSIG`, Windows a job object, so a hard-killed `noxy`
leaves no orphan. Stdout is the protocol channel; stderr passes through;
`noxyplugin.Logf` lands on stderr as `[ext <name>] <message>`.

### Errors

| situation | error text | poisons |
|---|---|---|
| handler returned an error | `extension 'x' failed: <message>` | no |
| result violates the declared return type | `extension 'x': result does not match declared return type "T"` | no |
| deadline expired, plugin cancelled in time | `extension 'x' timed out: <export> exceeded <N> ms` | no |
| deadline expired, no reply to CANCEL | `extension 'x' trapped: <export> exceeded <N> ms and did not cancel; process killed` | yes |
| exec or handshake failure | `extension 'x' trapped: start: ...` / `trapped: handshake: ...` | yes |
| process exited | `extension 'x' trapped: process exited (status N)` | yes |
| malformed frame, unknown id | `extension 'x' trapped: protocol violation: ...` | yes |

All are runtime errors with the Noxy stack of the call site, capturable
with `call_result`. Nothing returns `null` + a stderr line.

### Writing one in Go

```go
package main

import (
    "context"

    "github.com/estevaofon/noxy/sdk/noxyplugin"
)

func main() {
    p := noxyplugin.New()
    p.Handle("terminal_is_terminal", noxyplugin.Func0(isTerminal))
    p.Handle("terminal_read_key",    noxyplugin.Func0(readKey))
    p.Main() // serves stdin/stdout, exits with the protocol's status
}

func isTerminal(ctx context.Context) (bool, error)  { /* ... */ }
func readKey(ctx context.Context) (string, error)    { /* blocks until a key or ctx.Done() */ }
```

`Func0`…`Func5` check arity and convert arguments (`int64`, `float64`,
`bool`, `string`, `[]byte`, slices, maps, `noxyplugin.Struct`); an
untyped `Handler` gets `Args` with accessors. Handler errors become
`failed`; panics are recovered into `failed: panic: ...`; the handler's
`context.Context` is cancelled on CANCEL and at shutdown. `Main` protects
stdout (a stray `fmt.Println` goes to stderr) and refuses to run in a
terminal. Build the matrix and publish a release with `checksums.txt`
(`sdk/noxyplugin/release/build.sh`, or the GitHub Actions template next to
it); list exactly those asset names in `[binaries]`.

### `sys_load_plugin` (deprecated)

The line-delimited JSON plugin builtin is deprecated since v0.23.0 and
will be removed in v0.25.0 together with `internal/plugin` and the
compiler's `PluginNativeNames` special case; it prints a warning on first
use. Migrate by publishing the plugin as a `kind = "process"` extension.

### Measured cost (process, dev machine)

From `go test ./internal/ext -run '^$' -bench BenchmarkProcess` on the
author's machine (paste the four lines from Task 20 here, then the
one-line reading: empty round trip ≈ N µs vs. ≈ 4 µs for wasm; 1 KB ≈ N
µs; 1 MB ≈ N ms; `concurrent` is N× `single` with a 1 ms handler).
````

The last paragraph's "N" placeholders are filled with the numbers measured in Task 20 — the executor writes the real values, not the letters.

- [ ] **Step 2: `docs/PACKAGE_MANAGER.md`**

In "Get a Package", after the numbered list, add:

```markdown
`--get` always installs a fresh copy: the package directory under
`noxy_libs/` is replaced on every run (there is no `.git` left to pull).
For a process extension (`kind = "process"` in `noxy_ext.toml`) the
version is a release tag — omitted, `--get` resolves the newest semver
tag and prints it — and `--get` also downloads `checksums.txt` plus the
binary for your OS/arch from
`https://<host>/<user>/<repo>/releases/download/<tag>/` into
`noxy_libs/<pkg>/bin/`, verifying its sha256. A release without a binary
for your platform is an error here, never at runtime.
```

In "Integrity (`noxy.sum`)", replace the first sentence with:

```markdown
When a downloaded package contains an extension (`noxy_ext.toml`),
`noxy --get` records sha256 lines in `noxy.sum` next to your `noxy.mod`:
the manifest plus the `.wasm` artifact for wasm extensions, or the
manifest plus **every** published binary (`bin/<asset>`) for process
extensions — hashes of the assets your machine did not download come from
the release's `checksums.txt`, so one committed `noxy.sum` verifies a
teammate's macOS download and a Lambda's Linux download alike. At load
time the VM checks the manifest first and then the artifact it is about
to run.
```

- [ ] **Step 3: `AGENTS.md`**

Layout table: change the `internal/ext` row to `Extensões: wasm (wazero) e por processo (\`noxy-plugin/1\` sobre stdio); manifesto \`noxy_ext.toml\`, loader, \`Process\`, codec de quadros`; change the `internal/plugin` row description to `Plugins JSON legados (\`sys_load_plugin\`, deprecado — sai na v0.25.0)`; add a row `| | \`sdk/noxyplugin\` | SDK Go para extensões por processo — módulo aninhado \`github.com/estevaofon/noxy/sdk/noxyplugin\`, sem dependência de \`noxy-vm\` |`. Replace the "Extensões e plugins" paragraph with:

```markdown
**Extensões e plugins**: duas fronteiras, um `Backend` (`internal/ext/backend.go`).
wasm (`kind = "wasm"`, default) roda no wazero sem WASI, para computação pura.
Processo (`kind = "process"`, spec `docs/superpowers/specs/2026-08-29-process-extensions-design.md`)
é o meio principal para I/O, SO, drivers e SDKs: um executável por plataforma
publicado como asset de release, `noxy --get` baixa só o da máquina e grava
os hashes de todos em `noxy.sum`; protocolo `noxy-plugin/1` (quadros NXB por
stdio, start lazy, CANCEL cooperativo, poison/restart); SDK em `sdk/noxyplugin`
(testes rodam à parte: `go test ./...` dentro do módulo). `sys_load_plugin`
(JSON) está deprecado e sai na v0.25.0 junto com `internal/plugin` e
`compiler.PluginNativeNames`. Invariantes em
`docs/superpowers/specs/2026-08-29-extensibility-invariants-revision.md`.
```

- [ ] **Step 4: CI**

In `.github/workflows/network-deadlines.yml`, job `network-semantics`, after the `go test ./internal/... -count=1` step add:

```yaml
      - run: go test ./... -count=1
        working-directory: sdk/noxyplugin
```

- [ ] **Step 5: SDK README and release templates**

`sdk/noxyplugin/README.md`: the "Writing one in Go" example above, the type-mapping table of §9.3, the guarantees of §9.4, and the release recipe:

```sh
# sdk/noxyplugin/release/build.sh — run from the extension's module root
#!/usr/bin/env sh
set -eu
NAME="${1:?usage: build.sh <extension-name>}"
mkdir -p dist
for os in linux darwin windows; do for arch in amd64 arm64; do
  ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags=-s \
    -o "dist/noxy-plugin-$NAME-$os-$arch$ext" .
done; done
(cd dist && sha256sum -- * > checksums.txt)
echo "dist/ is ready: gh release create <tag> dist/*"
```

`sdk/noxyplugin/release/github-release.yml`: a workflow `on: push: tags: ['v*']` with `actions/checkout@v7`, `actions/setup-go@v7` (`go-version-file: go.mod`), a step running `sh release/build.sh <name>` (documented as "copy this file to `.github/workflows/` in your extension repo and replace `<name>`"), and `softprops/action-gh-release@v2` with `files: dist/*`.

- [ ] **Step 6: Check the Pages renderer hazards and commit**

Run: `grep -n '[{][{]\|[{]%' docs/EXTENSIONS.md docs/PACKAGE_MANAGER.md`
Expected: no output (no Liquid openers).
Run: `git diff --numstat`
Expected: only the touched files, with small line counts (no whole-file rewrites).

```bash
git add docs/EXTENSIONS.md docs/PACKAGE_MANAGER.md AGENTS.md .github/workflows/network-deadlines.yml sdk/noxyplugin/README.md sdk/noxyplugin/release
git commit -m "docs(ext): extensoes por processo — manifesto, ciclo de vida, erros, SDK, --get; CI do SDK (issue #80)"
```

---

### Task 22: Version bump, CHANGELOG, final validation, PR

**Files:**
- Modify: `internal/version/version.go` (`v0.22.0` → `v0.23.0`), `AGENTS.md` line 6 (`v0.22.0` → `v0.23.0`) and line 631 (`Noxy VM 0.22.0` → `Noxy VM 0.23.0`), `README.md` line 1 (both `0.22.0` in the badge) and line 225 (`Noxy REPL v0.22.0`), `docs/NOXY_LANGUAGE_SPEC.md` line 2516 (`v0.22.0`), `CHANGELOG.md` (new section on top)

The other `0.22.0` occurrences in the tree are historical ("since 0.22.0") and stay.

- [ ] **Step 1: Bump the version**

Apply the six replacements above with the Edit tool (never `sed -i`). Run: `grep -rn "0\.22\.0" internal/version README.md AGENTS.md docs/NOXY_LANGUAGE_SPEC.md | grep -v "since\|desde\|Desde\|spec §2.4\|0.22.0)"`
Expected: no remaining current-version references.

- [ ] **Step 2: CHANGELOG section** (insert above `## [0.22.0] - 2026-08-28`; date = the day of the PR)

```markdown
## [0.23.0] - AAAA-MM-DD

Extensões por processo (tier B) como meio principal de I/O — issue #80
(spec `docs/superpowers/specs/2026-08-29-process-extensions-design.md`,
decisão na #110). Uma fronteira, dois transportes: o wasm continua para
computação pura; I/O, SO, drivers e SDKs vão para um executável por
plataforma que o usuário nunca compila.

### Added
- `kind = "process"` em `noxy_ext.toml`: a extensão é um executável falando
  `noxy-plugin/1` — quadros NXB por stdin/stdout, handshake com binding de
  exports por nome, `id` multiplexado, LOG, CANCEL. Mesmo `use`, mesmo
  wrapper `.nx`, mesmos erros do wasm. Chaves novas: `[binaries]`,
  `call_timeout_ms`, `handshake_timeout_ms`, `restart`, `timeout_ms` por
  export, `concurrency = "concurrent"` (multiplexa e permite handles),
  `capabilities` declarativa (só exibida no `--get`).
- Ciclo de vida: o processo sobe no primeiro export chamado; prazo por
  chamada com cancelamento cooperativo (`extension 'x' timed out: …`) —
  só quem ignora o CANCEL é morto (`trapped` + poison); processo morto =
  `trapped: process exited (status N)`, `restart = true` só com
  `stateless`; todo backend é fechado na saída da VM (`runFile`, REPL,
  `sys_exit`), com `PDEATHSIG` no Linux e job object no Windows.
- SDK Go `github.com/estevaofon/noxy/sdk/noxyplugin` (módulo aninhado em
  `sdk/noxyplugin`, sem dependência de `noxy-vm`): `New/Handle/Serve/Main`,
  `Func0..Func5`, `Args`, `Logf`; protege o stdout do protocolo, recupera
  panics em `failed`, cancela o `context` no CANCEL e no EOF.
- `noxy --get` para extensões por processo: sem `@versão`, resolve a tag
  semver mais nova; baixa `checksums.txt` e **só** o asset da plataforma
  para `noxy_libs/<pkg>/bin/`, verifica o sha256 e grava em `noxy.sum` os
  hashes de **todos** os assets publicados (lockfile portável entre SOs).
  Sem asset para a plataforma = erro no `--get`, nunca em runtime.
- Benchmarks `BenchmarkProcess*` em `internal/ext`; números em
  `docs/EXTENSIONS.md`.

### Changed
- `noxy --get` substitui o diretório do pacote a cada execução (clone
  fresco): o caminho "existe → git pull" não atualizava nada sem `.git`.
- `noxy_ext.toml` presente mas inválido faz o `--get` falhar (antes só
  pulava o registro no `noxy.sum`).
- A verificação do `noxy.sum` cobre manifesto + o artefato que o backend
  executa (`.wasm` ou `bin/<asset>`).

### Deprecated
- `sys_load_plugin` (JSON por linha): aviso no stderr na primeira chamada;
  remoção na v0.25.0 junto com `internal/plugin` e
  `compiler.PluginNativeNames`. Migração: `kind = "process"` + SDK
  (`docs/EXTENSIONS.md`).
```

- [ ] **Step 3: Final validation**

Run, all green:

```
go test ./... -count=1
GOFLAGS=-trimpath=false go test ./internal/... -count=1
go test -race ./internal/vm -count=1
(cd sdk/noxyplugin && go test ./... -count=1 && go vet ./...)
go vet ./...
go run ./cmd/noxy noxy_examples/run_all_tests_concurrent.nx
go build -o noxy ./cmd/noxy
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/noxy
GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -o /dev/null ./cmd/noxy
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/noxy
git diff --numstat develop...HEAD
```

The last command must show no whole-file rewrites of pre-existing files (CRLF hazard).

- [ ] **Step 4: Commit and open the PR**

```bash
git add internal/version/version.go AGENTS.md README.md docs/NOXY_LANGUAGE_SPEC.md CHANGELOG.md
git commit -m "chore(version): v0.23.0 — extensoes por processo, SDK Go e --get por plataforma (issue #80)"
git push -u origin feature/issue-80-process-extensions
gh pr create --base develop --assignee @me --label "not available to review" \
  --title "feat(ext): extensões por processo — protocolo noxy-plugin/1, SDK Go e --get por plataforma (issue #80)" \
  --body-file <scratchpad>/pr_body.md
```

PR body (Summary / Components / Related Issues / Test Plan): Summary = the CHANGELOG bullets condensed; Components = `internal/ext` (backend, frame, manifest, process, spawn, testdata/processguest, exttest), `internal/vm` (extensions, builtins_sys, main wiring), `internal/pkgmanager` (release, manager), `sdk/noxyplugin`, docs, CI; Related Issues = `#80` (checkboxes 2, 3, 4, 7, 8-first-half), `#110`; Test Plan = checked items for every task's tests and the final validation, unchecked items for "CI green on ubuntu + windows", "benchmarks on the Linux runner meet §11", "owner review of the SDK API surface", "follow-up plan: migrate `noxy_terminal` and `noxy_dynamodb` (checkboxes 5–6)". Finish with the `🤖 Generated with [Claude Code](https://claude.com/claude-code)` line and the session link.

Then: **do not tick issue #80's checkboxes and do not tag `sdk/noxyplugin/v0.1.0`** — the owner does both after the merge.

---

## Out of this plan (next plan, after merge)

- Migrating `estevaofon/noxy_terminal` and `estevaofon/noxy_dynamodb` to `kind = "process"` with the SDK, releasing them with six assets + `checksums.txt`, validating `noxy --get` on Windows and Linux (issue #80 checkboxes 5 and 6; spec §10.2–§10.3). They live in other repositories and depend on the SDK tag.
- Removing `sys_load_plugin`, `internal/plugin` and `compiler.PluginNativeNames` (v0.25.0, spec §10.1).
- Spec §15 follow-ups (asset URL template, versioned `noxy.sum` keys, socket transport, REPL poisoning test).
