# External Terminal Library Design

## Context

Noxy is still evolving, and the terminal API is intended for personal
experimentation rather than as a stable part of the VM. The existing
`develop` branch already supports external libraries under `noxy_libs` and
out-of-process native plugins loaded with `sys_load_plugin`.

This change will keep the complete Space Invaders example while moving all
terminal-specific native behavior out of `internal/vm` and `internal/stdlib`.
The first version will live inside this repository at
`noxy_libs/github_com/estevaofon/noxy_terminal`. It can become a separate
repository later without changing its public import path or package layout.

## Goals

- Provide raw terminal input as an external Noxy library backed by a Go plugin.
- Preserve the current typed Noxy API used by Space Invaders.
- Keep the complete game and its deterministic `--smoke` mode.
- Support Windows and Unix-like terminals.
- Require no terminal-specific builtin, embedded stdlib module, or VM lifecycle
  change.
- Keep automated coverage focused on the external package and public example.

## Non-Goals

- Publishing `noxy_terminal` as a separate GitHub repository in this change.
- Changing the generic Noxy plugin protocol.
- Supporting multiple simultaneous terminal readers.
- Cancelling an active blocking key read through `close`.
- Stabilizing the terminal API for backward compatibility.
- Adding cursor movement, colors, screen clearing, or other ANSI presentation
  helpers.

## Package Layout

The new package will live at:

~~~text
noxy_libs/github_com/estevaofon/noxy_terminal/
├── terminal.nx
├── noxy.mod
├── README.md
├── go.mod
├── go.sum
├── main.go
├── terminal.go
├── terminal_unix.go
├── terminal_windows.go
├── terminal_test.go
├── build_plugin.sh
└── build_plugin.ps1
~~~

Responsibilities:

- `terminal.nx` exposes the typed Noxy API and lazily loads the plugin.
- `main.go` implements the line-delimited JSON request loop used by the generic
  Noxy plugin client.
- `terminal.go` owns raw-mode state, key normalization, error state, and
  platform-independent operations.
- `terminal_unix.go` opens `/dev/tty`.
- `terminal_windows.go` opens `CONIN$`.
- `terminal_test.go` tests the external runtime through a fake terminal driver.
- The build scripts produce `noxy-plugin-terminal` in the package directory so
  the existing recursive `sys_load_plugin` lookup can find it.
- `README.md` documents build, import, lifecycle, supported keys, and the
  experimental status.

## Public Noxy API

The external wrapper preserves the API already consumed by the game:

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
func open_raw() -> TerminalResult
func read_key() -> KeyEvent
func close() -> bool
~~~

Space Invaders imports it with:

~~~noxy
use github_com.estevaofon.noxy_terminal.terminal as terminal
~~~

The wrapper loads `noxy-plugin-terminal` only when a terminal function is first
called. Consequently, `space_invaders.nx --smoke` does not require a built
plugin and does not print a plugin warning.

## Plugin Protocol

The generic plugin transport continues to use line-delimited JSON on the
plugin's stdin and stdout. Those streams are reserved exclusively for RPC and
must never be used as the interactive terminal.

The plugin exposes these methods:

| Method | Result | Meaning |
|---|---|---|
| `is_terminal` | boolean | Whether the controlling terminal can be opened |
| `open_raw` | boolean | Whether raw mode was opened |
| `read_key` | string or null | Normalized key, or null on failure |
| `last_error` | string | Most recent operational failure |
| `close` | boolean | Whether terminal state was restored |

Operational failures return `false` or `null` and update `last_error`. Unknown
methods and malformed RPC requests use the plugin protocol's error field.

`terminal.nx` converts these primitive/dynamic responses into
`TerminalResult` and `KeyEvent`. This keeps plugin serialization independent of
Noxy struct representation.

## Terminal Access and Lifecycle

The plugin is launched with pipes for RPC, so file descriptor 0 is not the
interactive console. It opens the controlling terminal separately:

- Unix-like systems: `/dev/tty`.
- Windows: `CONIN$`.

`golang.org/x/term` performs terminal detection, raw-mode activation, and
restoration on the opened handle.

The runtime stores the opened file, saved terminal state, raw flag, and last
error behind a mutex. Open and close are idempotent. A single blocking reader
is supported. The caller must allow its reader loop to finish before calling
`close`; this is already how Space Invaders handles `q` and `ctrl+c`.

The RPC scanner must remain able to observe parent-pipe EOF while `read_key` is
blocked. It dispatches the single in-flight request to a worker and continues
watching stdin. On EOF it restores and closes the terminal handle. Signal
handling also attempts restoration before the plugin exits. Explicit
`terminal.close()` remains the primary cleanup path.

## Key Representation

The external library preserves the current experimental normalization:

- ASCII letters become lowercase.
- Space becomes `"space"`.
- CR or LF becomes `"enter"`.
- Ctrl+C becomes `"ctrl+c"`.
- Printable Unicode is returned as a one-rune string.
- Other control runes use `"unknown:0xNN"`.
- Multi-byte special-key sequences such as arrows are not supported.

## Space Invaders

The complete game from the terminal feature remains at
`noxy_examples/space_invaders.nx`. Game logic, concurrency, rendering, controls,
and deterministic smoke behavior remain unchanged. Only the terminal import
changes to the external package path.

Interactive setup requires building the plugin first. The README and example
documentation provide both PowerShell and shell commands.

## Error Handling

- Failure to find or start the plugin becomes a failed typed result rather than
  a VM crash.
- Terminal detection/open/read/restore failures update `last_error`.
- `read_key` returns `KeyEvent(false, "", error)` on failure.
- `close` returns `false` when restoration fails.
- JSON protocol diagnostics go to stderr, never stdout.
- Cleanup failure does not replace an existing protocol parse/dispatch error.

## Testing

Coverage remains intentionally small and boundary-oriented:

1. External Go package tests:
   - reject unavailable/non-terminal handles;
   - idempotent open/close;
   - representative key normalization;
   - restoration on explicit close and parent EOF;
   - primitive protocol results and `last_error`.
2. Compile the external Noxy module and the complete Space Invaders example.
3. Run `space_invaders.nx --smoke` as a separate integration command.
4. Run `go test ./internal/...` and the existing concurrent Noxy example runner.
5. Build the plugin on the current platform.
6. Perform an optional manual interactive game run when a real terminal is
   available.

No test under `internal/vm` references Space Invaders.

## Documentation

- The package README is the authoritative API and build guide.
- The root changelog describes the external package and example.
- The language specification does not list `terminal` as an embedded standard
  library module.
- Package-manager documentation may use `noxy_terminal` as an external package
  example but must state that this in-repository copy is provisional.

## Success Criteria

- The branch is created from the current `origin/develop`.
- No terminal-specific production code exists under `internal/vm` or
  `internal/stdlib`.
- The plugin builds from the external package directory on the current OS.
- Space Invaders compiles and its smoke mode passes with the external import.
- Internal Go tests and the concurrent Noxy runner pass.
- The new branch is pushed and a separate pull request targets `develop`.
