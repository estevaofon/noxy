# External Terminal Library Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an experimental external terminal package under `noxy_libs`, backed by an out-of-process Go plugin, and run the complete Space Invaders example against it without terminal-specific VM or embedded-stdlib code.

**Architecture:** A typed Noxy wrapper lazily loads `noxy-plugin-terminal` through the existing generic plugin system. The plugin reserves stdin/stdout for line-delimited JSON RPC, opens the controlling terminal separately through `/dev/tty` or `CONIN$`, and owns raw-mode lifecycle and key normalization.

**Tech Stack:** Noxy 0.1.0, Go 1.24.0/toolchain 1.24.11, `golang.org/x/term v0.39.0`, line-delimited JSON RPC, PowerShell and POSIX shell build scripts.

## Global Constraints

- Start from `origin/develop` on branch `feat/external-terminal-library`.
- Keep the complete Space Invaders game and deterministic `--smoke` mode.
- Preserve `TerminalResult`, `KeyEvent`, `is_terminal`, `open_raw`, `read_key`, and `close`.
- Import the package as `github_com.estevaofon.noxy_terminal.terminal`.
- Add no terminal-specific builtin under `internal/vm` and no terminal module under `internal/stdlib`.
- Do not change the generic plugin protocol.
- Support one blocking terminal reader; callers stop the reader loop before `close`.
- Use `/dev/tty` on non-Windows systems and `CONIN$` on Windows.
- Keep stdin/stdout exclusively for plugin JSON RPC; diagnostics go to stderr.
- Keep tests boundary-oriented and do not add a VM test that references Space Invaders.
- The package remains experimental and in-repository; publishing a separate repository is out of scope.

---

### Task 1: Build the external raw-terminal runtime

**Files:**
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/go.mod`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/go.sum`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/terminal.go`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/terminal_unix.go`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/terminal_windows.go`
- Test: `noxy_libs/github_com/estevaofon/noxy_terminal/terminal_test.go`

**Interfaces:**
- Consumes: `golang.org/x/term` for `IsTerminal`, `MakeRaw`, and `Restore`.
- Produces: `newTerminalRuntime(driver terminalDriver) *terminalRuntime`; methods `isTerminal() bool`, `openRaw() bool`, `readKey() (string, bool)`, `lastError() string`, `close() bool`, and `shutdown()`; pure helper `normalizeKey(rune) string`.

- [ ] **Step 1: Create the nested Go module**

Create `go.mod` exactly as:

~~~go
module github.com/estevaofon/noxy_terminal

go 1.24.0

toolchain go1.24.11

require golang.org/x/term v0.39.0
~~~

Run:

~~~powershell
go mod download golang.org/x/term@v0.39.0
~~~

from `noxy_libs/github_com/estevaofon/noxy_terminal`.

Expected: `go.sum` is generated and the dependency download exits 0.

- [ ] **Step 2: Write failing lifecycle and normalization tests**

Define test fakes implementing these interfaces:

~~~go
type terminalDevice interface {
	io.Reader
	io.Closer
	Fd() uintptr
}

type terminalDriver interface {
	open() (terminalDevice, error)
	isTerminal(fd int) bool
	makeRaw(fd int) (*terminalSnapshot, error)
	restore(fd int, snapshot *terminalSnapshot) error
}
~~~

Add these tests before production code:

~~~go
func TestRuntimeRejectsNonTerminal(t *testing.T)
func TestRuntimeOpenReadCloseIsIdempotent(t *testing.T)
func TestRuntimeReportsRestoreFailure(t *testing.T)
func TestNormalizeKey(t *testing.T)
func TestShutdownRestoresAndClosesDevice(t *testing.T)
~~~

`TestRuntimeOpenReadCloseIsIdempotent` uses input `"A"` and asserts:

- two `openRaw` calls succeed;
- `makeRaw` is called once;
- `readKey` returns `"a", true`;
- two `close` calls succeed;
- `restore` and device `Close` are each called once.

`TestNormalizeKey` uses this exact table:

~~~go
tests := []struct {
	input rune
	want  string
}{
	{'A', "a"},
	{' ', "space"},
	{'\r', "enter"},
	{'\n', "enter"},
	{'\x03', "ctrl+c"},
	{'é', "é"},
	{'\x01', "unknown:0x01"},
}
~~~

Run:

~~~powershell
go test ./... -run 'TestRuntime|TestNormalizeKey' -count=1
~~~

Expected RED: compilation fails because `terminalRuntime` and `normalizeKey` do not exist.

- [ ] **Step 3: Implement terminal driver and runtime**

In `terminal.go` define:

~~~go
type terminalSnapshot struct {
	state *term.State
}

type xTermDriver struct{}

type terminalRuntime struct {
	stateMu  sync.Mutex
	readMu   sync.Mutex
	driver   terminalDriver
	device   terminalDevice
	input    *bufio.Reader
	saved    *terminalSnapshot
	raw      bool
	stopping bool
	lastErr  string
}
~~~

Implement `xTermDriver` as:

~~~go
func (xTermDriver) open() (terminalDevice, error) {
	return openTerminalDevice()
}

func (xTermDriver) isTerminal(fd int) bool {
	return term.IsTerminal(fd)
}

func (xTermDriver) makeRaw(fd int) (*terminalSnapshot, error) {
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return &terminalSnapshot{state: state}, nil
}

func (xTermDriver) restore(fd int, snapshot *terminalSnapshot) error {
	return term.Restore(fd, snapshot.state)
}
~~~

Implement runtime behavior with these exact rules:

- `isTerminal` opens a temporary device, checks `driver.isTerminal`, closes it,
  and records any open/close error.
- `openRaw` holds `stateMu`; it rejects `stopping`, returns true when already
  raw, opens the device, verifies it is a terminal, calls `makeRaw`, and stores
  device, reader, and snapshot only after success.
- `readKey` serializes through `readMu`, snapshots the active reader under
  `stateMu`, reads one rune without holding `stateMu`, normalizes it, and records
  read failures.
- `close` restores before closing the device. Restore failure returns false and
  retains state so a caller may retry.
- `shutdown` marks `stopping`, attempts restore, closes the device even if
  restore fails, and clears held resources.
- A successful operation clears `lastErr`; `lastError` returns it under lock.

Implement `normalizeKey` with the exact table from Step 2 and lowercase only
ASCII `A` through `Z`.

Create platform files:

~~~go
//go:build !windows

package main

import "os"

func openTerminalDevice() (terminalDevice, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}
~~~

~~~go
//go:build windows

package main

import "os"

func openTerminalDevice() (terminalDevice, error) {
	return os.OpenFile("CONIN$", os.O_RDWR, 0)
}
~~~

- [ ] **Step 4: Verify GREEN and format**

Run from the external package directory:

~~~powershell
gofmt -w terminal.go terminal_unix.go terminal_windows.go terminal_test.go
go mod tidy
go test ./... -run 'TestRuntime|TestNormalizeKey|TestShutdown' -count=1
go test ./...
~~~

Expected: all external package tests PASS with no warning output.

- [ ] **Step 5: Commit the terminal runtime**

~~~powershell
git add -- noxy_libs/github_com/estevaofon/noxy_terminal/go.mod noxy_libs/github_com/estevaofon/noxy_terminal/go.sum noxy_libs/github_com/estevaofon/noxy_terminal/terminal.go noxy_libs/github_com/estevaofon/noxy_terminal/terminal_unix.go noxy_libs/github_com/estevaofon/noxy_terminal/terminal_windows.go noxy_libs/github_com/estevaofon/noxy_terminal/terminal_test.go
git commit -m "feat: add external terminal runtime"
~~~

---

### Task 2: Add the JSON plugin server and parent-exit cleanup

**Files:**
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/main.go`
- Test: `noxy_libs/github_com/estevaofon/noxy_terminal/server_test.go`
- Modify: `noxy_libs/github_com/estevaofon/noxy_terminal/terminal_test.go`

**Interfaces:**
- Consumes: the `terminalRuntime` methods from Task 1.
- Produces: `pluginRequest`, `pluginResponse`, `pluginServer`, `newPluginServer(runtime, output)`, `serve(input) error`, and `handle(request) pluginResponse`.

- [ ] **Step 1: Write failing protocol tests**

Add these tests:

~~~go
func TestPluginServerPrimitiveResultsAndLastError(t *testing.T)
func TestPluginServerRestoresTerminalOnParentEOF(t *testing.T)
func TestPluginServerRejectsUnknownMethod(t *testing.T)
~~~

The primitive-results test calls `handle` directly and asserts:

- `is_terminal` returns boolean true;
- `open_raw` returns boolean true;
- `read_key` returns string `"a"`;
- `close` returns boolean true;
- after a forced read error, `last_error` returns the driver error text.

The EOF test uses a blocking fake device whose `Read` waits until `Close`. Feed
the server these two JSON lines followed by EOF:

~~~json
{"method":"open_raw","params":[]}
{"method":"read_key","params":[]}
~~~

Assert `restore` and device `Close` are called once and `serve` returns.

Run:

~~~powershell
go test ./... -run TestPluginServer -count=1
~~~

Expected RED: compilation fails because `pluginServer` is undefined.

- [ ] **Step 2: Implement request dispatch**

Define:

~~~go
type pluginRequest struct {
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type pluginResponse struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type pluginServer struct {
	runtime *terminalRuntime
	encoder *json.Encoder
	writeMu sync.Mutex
	workers sync.WaitGroup
}
~~~

`handle` returns primitive results with this switch:

~~~go
switch request.Method {
case "is_terminal":
	return pluginResponse{Result: server.runtime.isTerminal()}
case "open_raw":
	return pluginResponse{Result: server.runtime.openRaw()}
case "read_key":
	key, ok := server.runtime.readKey()
	if !ok {
		return pluginResponse{Result: nil}
	}
	return pluginResponse{Result: key}
case "last_error":
	return pluginResponse{Result: server.runtime.lastError()}
case "close":
	return pluginResponse{Result: server.runtime.close()}
default:
	return pluginResponse{Error: "unknown method: " + request.Method}
}
~~~

`write` holds `writeMu` and writes through `json.Encoder`. JSON decode failures
produce an error response and do not terminate the scanner.

- [ ] **Step 3: Implement EOF-safe serving**

`serve` scans stdin continuously. Handle `read_key` in a tracked worker so the
scanner can still observe pipe EOF; handle other methods synchronously.

On scanner EOF:

1. call `runtime.shutdown()`;
2. wait for read workers;
3. return `scanner.Err()`.

Use a copied byte slice for each scanned line before passing it to a worker.
Never write diagnostics to the JSON output stream.

`main` constructs `newTerminalRuntime(xTermDriver{})`, defers `shutdown`,
installs an `os.Interrupt`/`syscall.SIGTERM` handler that calls `shutdown` and
then `os.Exit(0)`, and serves `os.Stdin` to `os.Stdout`. If `serve` returns an
error, print it to stderr and exit nonzero; never print diagnostics to stdout.

- [ ] **Step 4: Verify GREEN and build**

Run:

~~~powershell
gofmt -w main.go server_test.go terminal_test.go
go test ./... -run TestPluginServer -count=1
go test ./...
go build ./...
~~~

Expected: protocol tests PASS and the plugin builds.

- [ ] **Step 5: Commit the plugin server**

~~~powershell
git add -- noxy_libs/github_com/estevaofon/noxy_terminal/main.go noxy_libs/github_com/estevaofon/noxy_terminal/server_test.go noxy_libs/github_com/estevaofon/noxy_terminal/terminal_test.go
git commit -m "feat: add terminal plugin protocol"
~~~

---

### Task 3: Add the typed Noxy wrapper and build tooling

**Files:**
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/terminal.nx`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/noxy.mod`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/build_plugin.ps1`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/build_plugin.sh`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/README.md`
- Create: `noxy_libs/github_com/estevaofon/noxy_terminal/.gitignore`

**Interfaces:**
- Consumes: generic builtin `sys_load_plugin(name, executable)` and dynamic
  native `terminal_request(method)`.
- Produces: typed external module `TerminalResult`, `KeyEvent`,
  `is_terminal() bool`, `open_raw() TerminalResult`,
  `read_key() KeyEvent`, and `close() bool`.

- [ ] **Step 1: Create module metadata and build scripts**

Create `noxy.mod`:

~~~text
module github.com/estevaofon/noxy_terminal

noxy v0.1.0
~~~

Create `build_plugin.ps1`:

~~~powershell
$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    go build -o noxy-plugin-terminal.exe .
    Write-Host "Created noxy-plugin-terminal.exe"
} finally {
    Pop-Location
}
~~~

Create `build_plugin.sh`:

~~~bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
go build -o noxy-plugin-terminal .
echo "Created noxy-plugin-terminal"
~~~

Create `.gitignore` exactly as:

~~~gitignore
/noxy-plugin-terminal
/noxy-plugin-terminal.exe
~~~

Mark `build_plugin.sh` executable in Git:

~~~powershell
git update-index --add --chmod=+x noxy_libs/github_com/estevaofon/noxy_terminal/build_plugin.sh
~~~

- [ ] **Step 2: Implement the lazy typed wrapper**

Create `terminal.nx` with this API and lazy-load state:

~~~noxy
let _terminal_loaded: bool = false
let _terminal_attempted: bool = false
let _terminal_load_error: string = ""

struct TerminalResult
    ok: bool
    error: string
end

struct KeyEvent
    ok: bool
    key: string
    error: string
end

func _ensure_terminal_plugin() -> bool
    if _terminal_loaded then return true end
    if _terminal_attempted then return false end

    _terminal_attempted = true
    _terminal_loaded = sys_load_plugin("terminal", "noxy-plugin-terminal")
    if !_terminal_loaded then
        _terminal_load_error = "noxy-plugin-terminal was not found or could not be started"
    end
    return _terminal_loaded
end

func _terminal_error() -> string
    if !_terminal_loaded then return _terminal_load_error end
    let result: any = terminal_request("last_error")
    if result == null then return "terminal plugin request failed" end
    return to_str(result)
end

func _terminal_bool(method: string) -> bool
    let result: any = terminal_request(method)
    return result != null && to_str(result) == "true"
end

func is_terminal() -> bool
    if !_ensure_terminal_plugin() then return false end
    return _terminal_bool("is_terminal")
end

func open_raw() -> TerminalResult
    if !_ensure_terminal_plugin() then
        return TerminalResult(false, _terminal_error())
    end
    let ok: bool = _terminal_bool("open_raw")
    if !ok then return TerminalResult(false, _terminal_error()) end
    return TerminalResult(true, "")
end

func read_key() -> KeyEvent
    if !_ensure_terminal_plugin() then
        return KeyEvent(false, "", _terminal_error())
    end
    let result: any = terminal_request("read_key")
    if result == null then return KeyEvent(false, "", _terminal_error()) end
    return KeyEvent(true, to_str(result), "")
end

func close() -> bool
    if !_terminal_loaded then return true end
    return _terminal_bool("close")
end
~~~

- [ ] **Step 3: Document package usage**

`README.md` must include:

- experimental status and current in-repository location;
- build prerequisites: Go 1.24+;
- PowerShell and POSIX build commands;
- import path `github_com.estevaofon.noxy_terminal.terminal`;
- the complete public API signatures;
- supported key names;
- one-reader lifecycle rule;
- explicit `close` guidance;
- Windows `CONIN$` and Unix `/dev/tty` behavior;
- Space Invaders build and run commands.

- [ ] **Step 4: Build and compile the wrapper boundary**

Run:

~~~powershell
./build_plugin.ps1
go test ./...
go build ./...
~~~

from the external package directory.

Expected: the terminal plugin is built, its Go tests pass, and the package
compiles. The Noxy import and typed-wrapper boundary is exercised with the game
smoke in Task 4, avoiding a dependency on an unrelated external plugin.

- [ ] **Step 5: Commit wrapper and tooling**

~~~powershell
git add -- noxy_libs/github_com/estevaofon/noxy_terminal/terminal.nx noxy_libs/github_com/estevaofon/noxy_terminal/noxy.mod noxy_libs/github_com/estevaofon/noxy_terminal/build_plugin.ps1 noxy_libs/github_com/estevaofon/noxy_terminal/build_plugin.sh noxy_libs/github_com/estevaofon/noxy_terminal/README.md noxy_libs/github_com/estevaofon/noxy_terminal/.gitignore
git commit -m "feat: expose external terminal module"
~~~

---

### Task 4: Port the complete Space Invaders example

**Files:**
- Create: `noxy_examples/space_invaders.nx`
- Modify: `noxy_examples/run_all_tests_concurrent.nx`
- Modify: `CHANGELOG.md`
- Modify: `docs/PACKAGE_MANAGER.md`

**Interfaces:**
- Consumes: external module
  `github_com.estevaofon.noxy_terminal.terminal` from Task 3.
- Produces: the complete existing Space Invaders example with unchanged game
  behavior and `--smoke` output `SPACE_INVADERS_SMOKE_OK`.

- [ ] **Step 1: Restore the existing complete game**

Use `e6d68f5:noxy_examples/space_invaders.nx` as the exact source artifact.
Create the file byte-for-byte from that revision, then replace only:

~~~noxy
use terminal
~~~

with:

~~~noxy
use github_com.estevaofon.noxy_terminal.terminal as terminal
~~~

Do not change game constants, structs, routines, channels, rendering, controls,
cleanup, or smoke logic.

- [ ] **Step 2: Keep the interactive game out of the generic runner**

Add `"space_invaders.nx"` to the `exclusions` array in
`noxy_examples/run_all_tests_concurrent.nx`. Do not add a VM Go test for the
game.

- [ ] **Step 3: Update root documentation**

Add changelog entries under `Unreleased / Added` for:

- the experimental external terminal package backed by a Go plugin;
- the complete Space Invaders example using that package.

In `docs/PACKAGE_MANAGER.md`, replace the vague import note with the concrete
external-module example:

~~~noxy
use github_com.estevaofon.noxy_terminal.terminal as terminal
~~~

State that the current package copy is provisional inside `noxy_libs` and may
move to its own repository later.

Do not add `terminal` to the embedded standard-library list in
`docs/NOXY_LANGUAGE_SPEC.md`.

- [ ] **Step 4: Verify game compilation and smoke**

From the repository root run:

~~~powershell
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
~~~

Expected exact output:

~~~text
SPACE_INVADERS_SMOKE_OK
~~~

Run the plugin build from its package directory, then manually start the game
when an interactive terminal is available:

~~~powershell
./build_plugin.ps1
Set-Location ../../../..
go run cmd/noxy/main.go noxy_examples/space_invaders.nx
~~~

Expected manual behavior: A/D move, Space fires, Q or Ctrl+C exits, and the
cursor and terminal mode are restored.

- [ ] **Step 5: Commit the example and root docs**

~~~powershell
git add -- noxy_examples/space_invaders.nx noxy_examples/run_all_tests_concurrent.nx CHANGELOG.md docs/PACKAGE_MANAGER.md
git commit -m "feat: add external terminal space invaders"
~~~

---

### Task 5: Verify boundaries, cross-platform compilation, and repository regression

**Files:**
- Verify: `noxy_libs/github_com/estevaofon/noxy_terminal/`
- Verify: `noxy_examples/space_invaders.nx`
- Verify: `noxy_examples/run_all_tests_concurrent.nx`
- Verify: `CHANGELOG.md`
- Verify: `docs/PACKAGE_MANAGER.md`

**Interfaces:**
- Consumes: all outputs from Tasks 1–4.
- Produces: evidence that the package is external, cross-platform files compile,
  the game smoke passes, and the repository remains green.

- [ ] **Step 1: Verify no terminal implementation entered VM or stdlib**

Run:

~~~powershell
git diff --name-only origin/develop...HEAD
rg -n "terminal_(is_terminal|open_raw|read_key|close)|defineTerminalBuiltins|terminalRuntime" internal/vm internal/stdlib
~~~

Expected: the changed-file list contains no terminal implementation under
`internal/vm` or `internal/stdlib`, and `rg` finds no newly introduced terminal
implementation.

- [ ] **Step 2: Verify the external Go module**

From `noxy_libs/github_com/estevaofon/noxy_terminal` run:

~~~powershell
gofmt -d terminal.go terminal_unix.go terminal_windows.go terminal_test.go main.go server_test.go
go test ./...
go build ./...
$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build ./...
$env:GOOS = "windows"
$env:GOARCH = "amd64"
go build ./...
Remove-Item Env:GOOS
Remove-Item Env:GOARCH
~~~

Expected: formatting diff is empty; all tests and native/cross-platform builds
exit 0.

- [ ] **Step 3: Verify Noxy smoke and internal tests**

From repository root run:

~~~powershell
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
~~~

Expected: internal packages PASS and smoke prints
`SPACE_INVADERS_SMOKE_OK`.

- [ ] **Step 4: Run the complete Noxy example runner**

~~~powershell
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
~~~

Expected: zero failures and final line `TODOS TESTES PASSARAM.`.

- [ ] **Step 5: Run repository build and vet**

~~~powershell
go build ./...
go vet ./...
git diff --check
git status -sb
~~~

Expected: all commands exit 0 and the branch is clean.

- [ ] **Step 6: Review and publish**

Request code review for the complete range:

~~~powershell
$externalTerminalBase = git merge-base origin/develop HEAD
$externalTerminalHead = git rev-parse HEAD
git diff --stat "$externalTerminalBase..$externalTerminalHead"
~~~

Resolve Critical and Important findings, re-run affected tests, then push
`feat/external-terminal-library` and open a separate pull request targeting
`develop`.
