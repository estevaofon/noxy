# Terminal Module and Space Invaders Design

## Goal

Add a portable raw-terminal module to the Noxy runtime and use it to build a
real-time terminal Space Invaders game whose gameplay code is entirely Noxy.

## Scope

This work has two deliverables:

1. A reusable `terminal` standard-library module backed by native VM built-ins.
2. `noxy_examples/space_invaders.nx`, a real-time game using only Noxy source,
   the standard library, noxy-routines, and channels.

The first version targets interactive Windows and Unix terminals supported by
`golang.org/x/term`. It does not provide mouse input, terminal resizing, sound,
configurable key bindings, or multi-byte special-key decoding.

## Chosen Approach

Use `golang.org/x/term` for terminal detection, raw-mode entry, and restoration.
Keep key reads blocking at the native boundary. The game will call the blocking
read from a noxy-routine and send normalized key names to the main game loop
through a buffered channel. This avoids native polling and keeps game state
owned by one routine.

Platform-specific syscalls were rejected because they would duplicate terminal
handling for Windows and Unix. Shell commands such as `stty` or PowerShell were
rejected because they are environment-dependent and would make the game rely on
external commands.

## Terminal Module API

Create `internal/stdlib/terminal.nx` with these public types:

```noxy
struct TerminalResult
    ok: bool
    error: string
end

struct KeyEvent
    ok: bool
    key: string
    error: string
end
```

The module exports:

```noxy
func is_terminal() -> bool
func open_raw() -> TerminalResult
func read_key() -> KeyEvent
func close() -> bool
```

The Noxy wrappers delegate to typed native built-ins:

```text
terminal_is_terminal() -> bool
terminal_open_raw(TerminalResult) -> TerminalResult
terminal_read_key(KeyEvent) -> KeyEvent
terminal_close() -> bool
```

### API Behavior

- `is_terminal()` reports whether standard input is an interactive terminal.
- `open_raw()` saves the current terminal state, enables raw mode, and returns
  `{ok: true, error: ""}`.
- Calling `open_raw()` while raw mode is already active is idempotent and
  succeeds without replacing the saved original state.
- If stdin is not interactive or raw mode cannot be enabled, `open_raw()`
  returns `{ok: false, error: <message>}` without changing terminal state.
- `read_key()` blocks until it can return one normalized key event. It returns
  `{ok: false, key: "", error: <message>}` when raw mode is inactive, stdin
  ends, or reading fails.
- `close()` restores the exact saved state. It returns `true` when restoration
  succeeds or raw mode is already inactive, and `false` on restoration failure.

### Key Normalization

`read_key()` returns these stable names:

| Input | `key` value |
| --- | --- |
| ASCII letters | Lowercase letter, such as `"a"` |
| Space | `"space"` |
| Enter | `"enter"` |
| Ctrl+C | `"ctrl+c"` |
| Other printable Unicode rune | The rune as a string |

Other control bytes are returned as a deterministic string beginning with
`"unknown:"`; they do not crash the VM. Multi-byte terminal sequences such as
arrow keys are outside this version's contract because distinguishing a lone
Escape key from an escape-sequence prefix requires platform-sensitive timing.

## Runtime Architecture

Add a terminal runtime object to `SharedState`. It owns:

- the saved `x/term` state;
- whether raw mode is active;
- the input reader and key-sequence decoder;
- a state mutex for open/close operations;
- a read mutex so only one noxy-routine reads stdin at a time;
- an injectable terminal backend used by unit tests.

Because spawned VMs already share `SharedState`, raw-mode ownership and input
serialization will be consistent across the main VM and all noxy-routines.
The state mutex must not be held during a blocking input read, so restoration is
not prevented by a reader waiting for a key.

Register the native built-ins from a focused `internal/vm/builtins_terminal.go`
file and call that registration from the existing built-in registry.

## Cleanup and Failure Handling

`VM.Interpret` restores an active terminal before returning, both on success and
on runtime errors. `terminal.close()` remains the normal explicit cleanup path
for scripts. Restoration is idempotent so both paths may run safely.

The Space Invaders input routine recognizes `q` and `ctrl+c` as quit
commands. The main routine closes the terminal after leaving its game loop.
Abrupt process termination that bypasses Go defers, such as an external force
kill, is outside this version's guarantees.

## Space Invaders Design

Create `noxy_examples/space_invaders.nx`. All gameplay, rendering, timing, and
concurrency orchestration live in this file and use Noxy syntax only.

### Game State

The main routine exclusively owns mutable game state:

- player horizontal position;
- three lives and current score;
- a four-row, eight-column formation of invaders with alive/dead flags;
- formation position, direction, and movement timer;
- player and enemy projectile arrays;
- game status: playing, victory, defeat, or quit.

The input noxy-routine owns no gameplay state. It calls `terminal.read_key()` and
sends key names to a buffered `chan string`.

### Main Loop

Each frame performs these steps in order:

1. Drain available input commands using `when` with a `default` branch.
2. Apply movement, shooting, or quit commands.
3. Advance the invader formation every 500 milliseconds, accelerating by 25
   milliseconds per destroyed invader down to a 100-millisecond minimum.
4. Move projectiles and resolve collisions.
5. Spawn at most one enemy shot every 800 milliseconds from the lowest living
   invader in a cycling column.
6. Detect victory or defeat.
7. Render one complete ANSI frame using cursor-home positioning.
8. Sleep long enough to target approximately 20 frames per second.

The controls are `A` to move left, `D` to move right, space to fire, and
`Q`/Ctrl+C to quit. Player movement and projectiles are clamped to a 40-column,
20-row playfield. Only one player projectile and three enemy projectiles may be
active at a time, which bounds collision and rendering work. Each destroyed
invader awards 10 points. An enemy projectile removes one life and is then
discarded; the player is invulnerable for 750 milliseconds after a hit.

### Rendering

The playfield is represented as a one-dimensional string array and rebuilt for
each frame. Entities are projected into the array and then concatenated into a
single output buffer. ANSI cursor-home and clear-screen codes prevent scrolling.
The header shows score and lives, the footer shows controls, and the terminal
cursor is hidden only while the game is active and shown again during cleanup.

ASCII glyphs are used for predictable width across terminals:

- player: `A`;
- invader: `W`;
- player projectile: `|`;
- enemy projectile: `!`;
- barrier/border: `#`.

### Game Completion

Destroying every invader displays a victory message. Losing all lives or letting
the invader formation reach the player row displays a defeat message. Quitting
displays a short exit message. Each path restores the terminal and cursor before
the script ends.

## Testing

### Go Tests

Add focused tests covering:

- terminal built-ins are registered with complete native signatures;
- `open_raw()` rejects non-terminal stdin without state changes;
- open is idempotent and preserves the first saved state;
- close restores once and is idempotent;
- spawned VMs observe the same terminal state;
- ASCII, space, Enter, Ctrl+C, Unicode, and unknown control bytes
  normalize as specified;
- read failure and inactive raw mode produce structured errors;
- `Interpret` restores raw state after normal completion and runtime error.

Tests use an injected fake backend and in-memory readers; they do not modify the
developer's real terminal.

### Noxy Validation

Compile and launch the game in a non-interactive smoke mode controlled by a
script argument. Smoke mode skips raw terminal setup, runs a bounded number of
deterministic frames, and exits successfully after exercising state updates and
render construction. Normal execution remains interactive.

Run the project-required validation commands:

```text
go test ./internal/...
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
go build ./...
go vet ./...
```

## Compatibility

Existing `input()` behavior remains unchanged. Existing scripts do not enter raw
mode unless they import and call the new module. No external executable is
required by the game. The only new Go dependency is `golang.org/x/term`, chosen
at a version compatible with the project's Go 1.24 toolchain and existing
`golang.org/x/sys` version.
