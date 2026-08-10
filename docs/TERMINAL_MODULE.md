# Experimental Terminal Module

> **Status: Experimental.** The `terminal` module API and behavior may change
> between Noxy releases without the compatibility guarantees of stable standard
> library modules.

The `terminal` module provides real-time keyboard input from an interactive
standard input terminal. It is intended for terminal games, text interfaces,
and other programs that need to read keys without waiting for Enter.

## Import

```noxy
use terminal
```

## API

The module exposes the following types and functions:

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

func is_terminal() -> bool
func open_raw() -> TerminalResult
func read_key() -> KeyEvent
func close() -> bool
```

### `is_terminal`

```noxy
func is_terminal() -> bool
```

Reports whether standard input is an interactive terminal.

### `open_raw`

```noxy
func open_raw() -> TerminalResult
```

Saves the current terminal state and enables raw mode. `TerminalResult.ok` is
`false`, and `error` describes the failure, when standard input is not
interactive or raw mode cannot be enabled. Opening an already active raw
session succeeds without replacing the saved state.

### `read_key`

```noxy
func read_key() -> KeyEvent
```

Blocks until one key is read without requiring Enter. It is available only
while raw mode is active and returns either
`{ok: true, key: ..., error: ""}` or
`{ok: false, key: "", error: ...}` when input is unavailable or fails.

### `close`

```noxy
func close() -> bool
```

Restores the saved terminal state. It returns `true` after a successful restore
or when raw mode is already inactive, and `false` if restoration fails.

## Key Representation

`read_key()` normalizes ASCII letters to lowercase and uses names for selected
control keys:

| Input | Returned key |
|-------|--------------|
| Space | `"space"` |
| Enter (CR or LF) | `"enter"` |
| Ctrl+C | `"ctrl+c"` |
| Printable Unicode rune | The rune as a string |
| Other control byte | `"unknown:0xNN"` |

Multi-byte special-key sequences, including arrow keys, are not supported in
this experimental version.

## Lifecycle and Cleanup

Always close an opened raw session explicitly:

```noxy
use terminal

let opened: terminal.TerminalResult = terminal.open_raw()
if opened.ok then
    let event: terminal.KeyEvent = terminal.read_key()
    print(event.key)
    terminal.close()
end
```

The VM also attempts to restore an active terminal when root interpretation
ends normally, after a runtime error, or while an unexpected VM panic unwinds,
and before `sys.exit()` terminates the process. This automatic restoration is a
safety mechanism, not a replacement for explicit cleanup.

ANSI presentation state is separate from raw input state. Programs that hide
the cursor, change colors, or otherwise alter terminal presentation must reset
those settings themselves.

## Concurrent Reads and Close

Reads are serialized. Closing restores raw state even when a `read_key()` call
is already blocked. That active read is not cancelled and must be released by
input or process termination. The behavior of other concurrent reads during
`close()` is experimental; callers should coordinate a single reader and stop
its loop before closing the terminal.

## Space Invaders Example

`noxy_examples/space_invaders.nx` demonstrates the experimental module in an
interactive terminal. Use `A` and `D` to move, Space to fire, and `Q` or Ctrl+C
to quit.

```powershell
go run cmd/noxy/main.go noxy_examples/space_invaders.nx
```

The example also provides a deterministic, non-interactive smoke mode for
automated validation:

```powershell
go run cmd/noxy/main.go noxy_examples/space_invaders.nx --smoke
```

On normal completion, the example explicitly closes raw mode and emits the ANSI
cursor-show sequence. If an unexpected runtime error occurs after the cursor is
hidden, automatic cleanup attempts to restore raw mode, but the terminal may
still require a cursor-show or reset sequence.
