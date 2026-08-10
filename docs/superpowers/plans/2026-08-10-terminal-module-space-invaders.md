# Terminal Module and Space Invaders Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add portable raw-key terminal input to Noxy and build a real-time terminal Space Invaders game written entirely in Noxy.

**Architecture:** A process-level terminal runtime stored in SharedState uses golang.org/x/term to enter and restore raw mode. A blocking native key reader runs in a noxy-routine and sends normalized commands through a channel to a single-owner game loop.

**Tech Stack:** Go 1.24, golang.org/x/term v0.39.0, Noxy standard-library modules, noxy-routines/channels, ANSI terminal rendering.

## Global Constraints

- Preserve the blocking, line-oriented behavior of input().
- Keep all gameplay code in noxy_examples/space_invaders.nx.
- Do not invoke external commands for terminal input or rendering.
- Support Windows and Unix terminals recognized by golang.org/x/term.
- Normalize individual keys only; multi-byte special keys and mouse input are out of scope.
- Use a 40 by 20 playfield, four rows by eight columns of invaders, three lives, one player projectile, and at most three enemy projectiles.
- Target approximately 20 frames per second and provide deterministic --smoke execution.
- Add space_invaders.nx to the concurrent runner exclusion list.

---

### Task 1: Shared raw-terminal runtime and key normalization

**Files:**
- Modify: go.mod
- Modify: go.sum
- Modify: internal/vm/vm.go
- Create: internal/vm/terminal_runtime.go
- Create: internal/vm/terminal_runtime_test.go

**Interfaces:**
- Produces: terminalRuntime.openRaw() error, terminalRuntime.readKey() (string, error), terminalRuntime.close() error, and terminalRuntime.isTerminal() bool.
- Produces: SharedState.Terminal *terminalRuntime, shared by every VM created with NewWithShared.
- Consumes: golang.org/x/term v0.39.0 and os.Stdin.

- [ ] **Step 1: Write failing runtime tests**

Create a fake driver which records makeRaw and restore calls without touching the real console:

~~~go
type fakeTerminalDriver struct {
    terminal bool
    makeErr  error
    closeErr error
    made     int
    restored int
}

func (f *fakeTerminalDriver) isTerminal(int) bool { return f.terminal }
func (f *fakeTerminalDriver) makeRaw(int) (*terminalSnapshot, error) {
    f.made++
    if f.makeErr != nil { return nil, f.makeErr }
    return &terminalSnapshot{}, nil
}
func (f *fakeTerminalDriver) restore(int, *terminalSnapshot) error {
    f.restored++
    return f.closeErr
}
~~~

Add these exact tests:

~~~go
func TestTerminalOpenRejectsNonTerminal(t *testing.T)
func TestTerminalOpenAndCloseAreIdempotent(t *testing.T)
func TestTerminalCloseReportsRestoreFailure(t *testing.T)
func TestTerminalRuntimeNormalizesKeys(t *testing.T)
func TestTerminalReadRequiresRawMode(t *testing.T)
func TestSpawnedVMsShareTerminalRuntime(t *testing.T)
~~~

The normalization table asserts A -> "a", space -> "space", CR and LF -> "enter", byte 3 -> "ctrl+c", é -> "é", and byte 1 -> "unknown:0x01".

- [ ] **Step 2: Run the tests and verify the expected failure**

~~~powershell
go test ./internal/vm -run 'TestTerminal(Open|Close|Runtime|Read)|TestSpawnedVMsShareTerminalRuntime' -v
~~~

Expected: build failure because terminalRuntime, terminalSnapshot, and SharedState.Terminal do not exist.

- [ ] **Step 3: Add the pinned terminal dependency**

~~~powershell
go get golang.org/x/term@v0.39.0
~~~

Expected: go.mod gains a direct golang.org/x/term v0.39.0 requirement, go.sum gains checksums, and the Go version remains 1.24.

- [ ] **Step 4: Implement the minimal runtime abstraction**

Use these contracts in terminal_runtime.go:

~~~go
type terminalSnapshot struct { state *term.State }

type terminalDriver interface {
    isTerminal(fd int) bool
    makeRaw(fd int) (*terminalSnapshot, error)
    restore(fd int, snapshot *terminalSnapshot) error
}

type terminalRuntime struct {
    stateMu sync.Mutex
    readMu  sync.Mutex
    driver  terminalDriver
    input   *bufio.Reader
    fd      int
    raw     bool
    saved   *terminalSnapshot
}
~~~

Implement xTermDriver with term.IsTerminal, term.MakeRaw, and term.Restore. openRaw preserves the first saved state. close clears state only after successful restoration. readKey checks raw mode under stateMu, releases stateMu before blocking, then serializes reads with readMu.

Use this exact normalization:

~~~go
switch r {
case ' ':
    return "space", nil
case '\r', '\n':
    return "enter", nil
case '\x03':
    return "ctrl+c", nil
}
if unicode.IsControl(r) {
    return fmt.Sprintf("unknown:0x%02x", r), nil
}
if r >= 'A' && r <= 'Z' {
    r = unicode.ToLower(r)
}
return string(r), nil
~~~

Initialize the runtime once in NewWithConfig using bufio.NewReader(os.Stdin) and int(os.Stdin.Fd()). NewWithShared must reuse the SharedState pointer without replacing Terminal.

- [ ] **Step 5: Run focused and package tests**

~~~powershell
go test ./internal/vm -run 'TestTerminal(Open|Close|Runtime|Read)|TestSpawnedVMsShareTerminalRuntime' -v
go test ./internal/vm
~~~

Expected: all tests pass and the real terminal is never modified.

- [ ] **Step 6: Commit the runtime**

~~~powershell
git add go.mod go.sum internal/vm/vm.go internal/vm/terminal_runtime.go internal/vm/terminal_runtime_test.go
git commit -m "feat: add shared raw terminal runtime"
~~~

---

### Task 2: Typed terminal built-ins and standard-library module

**Files:**
- Modify: internal/vm/builtins.go
- Modify: internal/vm/builtins_registry_test.go
- Create: internal/vm/builtins_terminal.go
- Create: internal/vm/builtins_terminal_test.go
- Create: internal/stdlib/terminal.nx
- Modify: internal/compiler/module_exports_test.go

**Interfaces:**
- Consumes: terminalRuntime from Task 1.
- Produces built-ins: terminal_is_terminal() -> bool, terminal_open_raw(any) -> any, terminal_read_key(any) -> any, and terminal_close() -> bool.
- Produces Noxy API: terminal.is_terminal, terminal.open_raw, terminal.read_key, and terminal.close.

- [ ] **Step 1: Add failing registry, signature, wrapper, and behavior tests**

Extend the registry snapshot with the four alphabetically placed terminal names. Add signature cases:

~~~go
{name: "terminal_is_terminal", arity: 0, returnType: "bool"},
{name: "terminal_open_raw", arity: 1, params: []value.ParamInfo{{TypeName: "any"}}, returnType: "any"},
{name: "terminal_read_key", arity: 1, params: []value.ParamInfo{{TypeName: "any"}}, returnType: "any"},
{name: "terminal_close", arity: 0, returnType: "bool"},
~~~

In builtins_terminal_test.go construct ObjStruct definitions for TerminalResult and KeyEvent, call the natives, and assert exact fields for success, inactive reads, read errors, and close failures.

Add a module-export test compiling:

~~~noxy
use terminal
let available: bool = terminal.is_terminal()
let opened: terminal.TerminalResult = terminal.open_raw()
let event: terminal.KeyEvent = terminal.read_key()
let closed: bool = terminal.close()
~~~

- [ ] **Step 2: Run the tests and verify failure**

~~~powershell
go test ./internal/vm ./internal/compiler -run 'TestBuiltinRegistrySnapshot|TestBuiltinNativeSignatures|TestTerminalBuiltins|Test.*Module.*Terminal' -v
~~~

Expected: registry mismatch and module-not-found/compiler failures.

- [ ] **Step 3: Register typed native adapters**

Call vm.defineTerminalBuiltins() from defineBuiltins. Register fixed NativeSignature values. terminal_open_raw and terminal_read_key validate that their definition argument is *value.ObjStruct, create an instance, and fill:

~~~noxy
TerminalResult(ok, error)
KeyEvent(ok, key, error)
~~~

Malformed internal calls return value.NewNull(). Operational failures are returned in struct fields and never printed.

- [ ] **Step 4: Add the embedded Noxy wrapper**

Create internal/stdlib/terminal.nx:

~~~noxy
struct TerminalResult
    ok: bool
    error: string
end

struct KeyEvent
    ok: bool
    key: string
    error: string
end

func is_terminal() -> bool
    return terminal_is_terminal()
end

func open_raw() -> TerminalResult
    return terminal_open_raw(TerminalResult)
end

func read_key() -> KeyEvent
    return terminal_read_key(KeyEvent)
end

func close() -> bool
    return terminal_close()
end
~~~

- [ ] **Step 5: Run focused and full internal tests**

~~~powershell
go test ./internal/vm ./internal/compiler -run 'TestBuiltinRegistrySnapshot|TestBuiltinNativeSignatures|TestTerminalBuiltins|Test.*Module.*Terminal' -v
go test ./internal/...
~~~

Expected: all tests pass.

- [ ] **Step 6: Commit the terminal API**

~~~powershell
git add internal/vm/builtins.go internal/vm/builtins_registry_test.go internal/vm/builtins_terminal.go internal/vm/builtins_terminal_test.go internal/stdlib/terminal.nx internal/compiler/module_exports_test.go
git commit -m "feat: expose raw terminal module"
~~~

---

### Task 3: Automatic terminal restoration

**Files:**
- Modify: internal/vm/executor.go
- Modify: internal/vm/terminal_runtime_test.go

**Interfaces:**
- Consumes: terminalRuntime.close() error.
- Produces: InterpretWithGlobals always restores an active terminal before returning.

- [ ] **Step 1: Write failing lifecycle tests**

Add:

~~~go
func TestInterpretRestoresTerminalAfterSuccess(t *testing.T)
func TestInterpretRestoresTerminalAfterRuntimeError(t *testing.T)
func TestInterpretReportsRestoreErrorWhenExecutionSucceeds(t *testing.T)
func TestInterpretPreservesRuntimeErrorWhenRestoreAlsoFails(t *testing.T)
~~~

Each test installs a fake active terminal, interprets either let value: int = 1 or 1 / 0, and checks restore count and error precedence.

- [ ] **Step 2: Run tests and verify restoration is missing**

~~~powershell
go test ./internal/vm -run 'TestInterpret(Restores|Reports|Preserves)' -v
~~~

Expected: failures showing zero restore calls.

- [ ] **Step 3: Implement cleanup with explicit error precedence**

Use:

~~~go
runErr := vm.run(1)
restoreErr := vm.shared.Terminal.close()
if runErr != nil { return runErr }
if restoreErr != nil { return fmt.Errorf("restore terminal: %w", restoreErr) }
return nil
~~~

Do not clean up inside spawned run calls; the root interpreter owns process terminal lifecycle.

- [ ] **Step 4: Run lifecycle and regression tests**

~~~powershell
go test ./internal/vm -run 'TestInterpret(Restores|Reports|Preserves)' -v
go test ./internal/...
~~~

Expected: all tests pass.

- [ ] **Step 5: Commit cleanup**

~~~powershell
git add internal/vm/executor.go internal/vm/terminal_runtime_test.go
git commit -m "fix: restore terminal after interpretation"
~~~

---

### Task 4: Deterministic Space Invaders core and smoke mode

**Files:**
- Create: noxy_examples/space_invaders.nx
- Create: internal/vm/space_invaders_example_test.go

**Interfaces:**
- Produces: space_invaders.nx --smoke, a bounded non-interactive integration path.
- Produces: Invader, Projectile, and GameState Noxy structs.
- Consumes: time.now_ms, arrays, structs, sys.argv, and ANSI string construction.

- [ ] **Step 1: Write a failing example smoke test**

Read ../../noxy_examples/space_invaders.nx, set os.Args temporarily, compile with module root ../.., and interpret:

~~~go
func TestSpaceInvadersSmokeMode(t *testing.T) {
    source, err := os.ReadFile("../../noxy_examples/space_invaders.nx")
    if err != nil { t.Fatal(err) }

    previous := os.Args
    os.Args = []string{"noxy", "space_invaders.nx", "--smoke"}
    t.Cleanup(func() { os.Args = previous })

    code := compileVMSourceWithRoot(t, string(source), "../..")
    machine := NewWithConfig(VMConfig{RootPath: "../.."})
    if err := machine.Interpret(code); err != nil { t.Fatal(err) }
}
~~~

The local compileVMSourceWithRoot helper uses compiler.NewWithStateAndRoot so imports are type checked.

- [ ] **Step 2: Run the test and verify the file is absent**

~~~powershell
go test ./internal/vm -run TestSpaceInvadersSmokeMode -v
~~~

Expected: FAIL opening noxy_examples/space_invaders.nx.

- [ ] **Step 3: Implement game data and update functions**

Define:

~~~noxy
let WIDTH: int = 40
let HEIGHT: int = 20
let INVADER_ROWS: int = 4
let INVADER_COLS: int = 8
let FRAME_MS: int = 50

struct Invader
    x: int
    y: int
    alive: bool
end

struct Projectile
    x: int
    y: int
    active: bool
end

struct GameState
    player_x: int
    lives: int
    score: int
    invader_dx: int
    invader_step_ms: int
    last_invader_step: int
    last_enemy_shot: int
    enemy_column: int
    invulnerable_until: int
    status: string
end
~~~

Implement new_invaders, new_enemy_shots, apply_command, step_invaders, step_player_shot, step_enemy_shots, spawn_enemy_shot, count_alive, update_status, build_frame, and run_smoke. Mutation signatures are explicit references:

~~~noxy
func apply_command(state: ref GameState, shot: ref Projectile, command: string)
func step_invaders(state: ref GameState, invaders: ref Invader[], now: int)
func step_player_shot(state: ref GameState, shot: ref Projectile, invaders: ref Invader[])
func step_enemy_shots(state: ref GameState, shots: ref Projectile[], now: int)
~~~

run_smoke initializes state, applies a, d, and space, runs 12 frames with synthetic timestamps spaced by FRAME_MS, builds each frame without ANSI output, checks player bounds/lives/projectile limits/frame dimensions, prints SPACE_INVADERS_SMOKE_OK, and returns. A failed invariant prints a specific message and calls sys.exit(1).

- [ ] **Step 4: Dispatch smoke mode before terminal setup**

Scan sys.argv() for --smoke. If present call run_smoke and skip terminal.open_raw entirely. Without --smoke, print a short message that interactive mode is added in the next task and return without touching the terminal. Task 5 replaces this branch with run_interactive.

- [ ] **Step 5: Run smoke tests**

~~~powershell
go test ./internal/vm -run TestSpaceInvadersSmokeMode -v
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
~~~

Expected: both pass; the second prints SPACE_INVADERS_SMOKE_OK without waiting.

- [ ] **Step 6: Commit the game core**

~~~powershell
git add noxy_examples/space_invaders.nx internal/vm/space_invaders_example_test.go
git commit -m "feat: add space invaders game core"
~~~

---

### Task 5: Real-time Noxy input, gameplay, and rendering

**Files:**
- Modify: noxy_examples/space_invaders.nx
- Modify: internal/vm/space_invaders_example_test.go
- Modify: noxy_examples/run_all_tests_concurrent.nx

**Interfaces:**
- Consumes: terminal.read_key, terminal.close, noxy-routines, and chan string.
- Produces: an interactive 20 FPS game controlled by A, D, space, Q, or Ctrl+C.

- [ ] **Step 1: Add a failing scripted interactive integration test**

Add TestSpaceInvadersScriptedInteractiveInput. Compile the example without --smoke, replace machine.shared.Terminal with a terminal runtime using a fake terminal driver and strings.NewReader("ad q"), then run Interpret in a goroutine with a two-second timeout. Assert that raw mode was opened exactly once, restored exactly once, and execution completed without error. This tests the real Noxy routine/channel/input wiring without changing the developer's terminal.

- [ ] **Step 2: Run the contract test and verify failure**

~~~powershell
go test ./internal/vm -run TestSpaceInvadersScriptedInteractiveInput -v
~~~

Expected: FAIL because the current non-smoke path does not open or consume the fake raw terminal.

- [ ] **Step 3: Add the input noxy-routine**

~~~noxy
func read_controls(commands: chan string)
    while true do
        let event: terminal.KeyEvent = terminal.read_key()
        if !event.ok then
            chan_send(commands, "q")
            return
        end
        chan_send(commands, event.key)
        if event.key == "q" || event.key == "ctrl+c" then return end
    end
end
~~~

The main routine creates make_chan(16), spawns the reader, and drains every available command with when/default before updating.

- [ ] **Step 4: Add interactive lifecycle and renderer**

run_interactive must:

1. Refuse non-terminal stdin with a clear message.
2. Open raw mode and report TerminalResult.error on failure.
3. Hide the cursor with \u001b[?25l and clear the screen.
4. Run command/update/render/sleep frames until status changes.
5. Close raw mode, show the cursor with \u001b[?25h, and print victory, defeat, or quit.

Build each frame as one string and call iprint("\u001b[H" + frame). Render score/lives, borders, 32 invaders, player, projectiles, and controls. Formation begins at 500 ms per step, accelerates by 25 ms per kill to 100 ms, and emits at most one enemy shot every 800 ms from the lowest living invader in a cycling column. Hits award 10 points or remove one life with 750 ms invulnerability.

- [ ] **Step 5: Exclude the game from the concurrent runner**

Add "space_invaders.nx" beside other interactive/visual exclusions. Do not change worker behavior.

- [ ] **Step 6: Verify automated and interactive behavior**

~~~powershell
go test ./internal/vm -run TestSpaceInvadersSmokeMode -v
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
go run cmd/noxy/main.go noxy_examples/space_invaders.nx
~~~

Expected: smoke passes; interactively A/D move without Enter, space fires, Q exits, frames do not scroll, and cursor/canonical terminal mode are restored.

- [ ] **Step 7: Commit the playable game**

~~~powershell
git add noxy_examples/space_invaders.nx internal/vm/space_invaders_example_test.go noxy_examples/run_all_tests_concurrent.nx
git commit -m "feat: add real-time terminal space invaders"
~~~

---

### Task 6: Documentation and full verification

**Files:**
- Modify: docs/NOXY_LANGUAGE_SPEC.md
- Modify: docs/superpowers/specs/2026-08-10-terminal-module-space-invaders-design.md
- Modify: docs/superpowers/plans/2026-08-10-terminal-module-space-invaders.md

**Interfaces:**
- Documents the terminal API, blocking behavior, normalized keys, cleanup, and game commands.

- [ ] **Step 1: Document module and example**

Add the four signatures, TerminalResult/KeyEvent fields, normalized key names, the rule that read_key blocks without requiring Enter, and this cleanup example:

~~~noxy
use terminal
let opened: terminal.TerminalResult = terminal.open_raw()
if opened.ok then
    let event: terminal.KeyEvent = terminal.read_key()
    print(event.key)
    terminal.close()
end
~~~

Document:

~~~powershell
go run cmd/noxy/main.go noxy_examples/space_invaders.nx
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
~~~

- [ ] **Step 2: Review documentation against the public API**

Compare the documented function names, result fields, normalized keys, blocking behavior, and cleanup example directly with internal/stdlib/terminal.nx and the approved design. Correct every mismatch before validation.

- [ ] **Step 3: Run formatting and mandatory validation**

~~~powershell
gofmt -w internal/vm internal/compiler
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go build ./...
go vet ./...
git diff --check
~~~

Expected: every command exits zero; smoke prints SPACE_INVADERS_SMOKE_OK; the runner does not list space_invaders.nx; no formatting errors remain.

- [ ] **Step 4: Inspect final scope**

~~~powershell
git status --short
git diff --stat
git diff -- go.mod go.sum internal/vm internal/compiler internal/stdlib/terminal.nx noxy_examples/space_invaders.nx noxy_examples/run_all_tests_concurrent.nx docs/NOXY_LANGUAGE_SPEC.md
~~~

Expected: only terminal runtime/API, game, tests, runner exclusion, dependency, and documentation changes.

- [ ] **Step 5: Commit documentation and verification**

~~~powershell
git add docs/NOXY_LANGUAGE_SPEC.md docs/superpowers/specs/2026-08-10-terminal-module-space-invaders-design.md docs/superpowers/plans/2026-08-10-terminal-module-space-invaders.md
git commit -m "docs: document terminal input and space invaders"
~~~
