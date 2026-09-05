# Process Extensions (tier B) — Protocol and Mechanism Design

Brief: issue #80 (spec + implementation of tier B). Decision record: issue
#110. Invariants: `2026-08-29-extensibility-invariants-revision.md`. Sibling
mechanism: `2026-08-23-wasm-extension-mechanism-design.md` (wasm, tier A),
whose sections are cited below as "wasm §N". Baseline: v0.22.0; target
release: v0.23.0.

This document is the written contract for the `noxy-plugin/1` protocol and
for everything around it — manifest, lifecycle, package manager, Go SDK,
migration. It covers the eight requirements of issue #80 in order: §2
protocol, §4 lifecycle, §6 errors, §5 concurrency, §7 manifest, §8 package
manager, §9 SDK, §10 migration.

## Goal and scope

**Goal.** An extension written in Go (or any language with a complete
standard library) is installed with `noxy --get` and used exactly like a wasm
extension — same `use`, same typed `.nx` wrapper, same error vocabulary —
with nothing compiled on the user's machine. The author produces every
platform binary with one command and no C toolchain:

```sh
for os in linux darwin windows; do for arch in amd64 arm64; do
  ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -o "dist/noxy-plugin-terminal-$os-$arch$ext" .
done; done
```

Process extensions are the **primary** mechanism for I/O, OS access, drivers
and SDK bindings (issue #110). Wasm stays for pure computation.

**Non-goals (v1).**

- OS-level sandboxing of the child process. The extension does what the user
  can do; `capabilities` are declarative (§7), never enforced (invariant 5
  was dropped).
- Sockets or named pipes as transport. Stdio is the transport; a socket
  transport is a follow-up if an extension needs stdio for itself (§15).
- Callbacks from the extension into Noxy code, streaming payloads,
  cancellation initiated by Noxy code. Only host-initiated cancellation on
  timeout exists (§4.3).
- A package registry, authenticated release downloads, hosts other than the
  `releases/download/<tag>/<asset>` layout (§8.2, §15).
- Any change to the wasm ABI v1 or to NXB v1.

## Constraints from the current implementation

Facts this design leans on, verified against the tree at f00eeec:

- **The embryo.** `internal/plugin/plugin.go` runs the executable with
  line-delimited JSON on stdio, looks it up via `PATH` → `noxy_libs/<name>/`
  → cwd, serializes every call under one mutex, has no version, no timeout,
  no shutdown, and on any error prints to stderr and returns `null`.
  `sys_load_plugin` (`internal/vm/builtins_sys.go`) registers one untyped
  native `<name>_request(method, ...params)`; the compiler special-cases
  that name in `compiler.PluginNativeNames` (`internal/compiler/known_globals.go`),
  called from `cmd/noxy/main.go` (twice) and `internal/vm/modules.go`.
- **The seam.** `internal/vm/extensions.go` `ensureExtensionLoaded(dir)`:
  parse manifest → `CheckMinNoxy` → name-collision check → atomic pre-check
  of every export against existing globals → read + hash-verify the artifact
  (`verifyExtensionSum`) → `ext.LoadModule` → one
  `DefineContextualNativeWithSignature` per export whose closure calls
  `module.Call(ctx, index, args)`. `SharedState.Ext` is
  `map[string]*ext.Module`. The hook fires in `loadResolvedModule` for
  `resolvedFileModule` only, before the wrapper `.nx` compiles.
- **The codec.** `internal/ext/nxb.go`: tags 0x00–0x08, `0x09` reserved,
  depth 64, `Limits.MaxBytes` 64 MiB, `EncodeArgs` (u32 count + values) /
  `DecodeValue`. `checkDeclaredReturn` in `call.go` enforces the manifest's
  return type at the boundary.
- **The semantics to mirror.** `internal/ext/loader.go` / `call.go`: default
  call timeout 30 s, poisoning, error phrasing `extension 'x' failed: <msg>`,
  `extension 'x' trapped: <detail>`, `extension 'x' is poisoned by an earlier
  trap`. `nx_log` writes `[ext <name>] <msg>` to stderr (no `diagOut` on the
  VM yet).
- **The manifest.** `internal/ext/manifest.go`: unknown keys are errors;
  `concurrency` ∈ {`single`, `stateless`}; `stateless` forbids `stateful`
  exports; non-empty `capabilities` rejected; `wasm` defaults to `ext.wasm`.
- **The package manager.** `internal/pkgmanager/manager.go`: `git clone` into
  `noxy_libs/<domain>/<user>/<repo>` (dots in the domain become `_`),
  `git checkout <version>` (default `HEAD`), `.git` removed, then
  `RecordExtensionSums` writes `<pkg> noxy_ext.toml` and `<pkg> <wasm>` lines
  (`sumfile.go`, key `<pkg> <file>`, value `sha256:<hex>`). An existing
  package directory is "updated" with `git pull`, which is a no-op once
  `.git` is gone — re-running `--get` with a new version does nothing today.
- **Concurrency is real.** `spawn_task` runs each task on its own goroutine
  with its own `VM` over the shared `SharedState`
  (`internal/vm/builtins_tasks.go`); natives are called concurrently.
- **Exit paths.** `runFile` → `runWithConfig` returns an exit code (defers
  run); `sys_exit` calls `os.Exit` directly (`builtins_sys.go`); the REPL has
  its own loop; `main` recovers panics. There is no VM close hook.
- **Module path.** `go.mod` says `module noxy-vm` — not fetchable; the
  repository is `github.com/estevaofon/noxy`. `golang.org/x/sys` is already
  a dependency (Windows job objects are available without a new import).
- **The two real extensions.** `estevaofon/noxy_terminal` (JSON; opens
  `/dev/tty` or `CONIN$` itself, so stdio stays free for RPC; `read_key`
  blocks until a key arrives; the server already handles `read_key` on a
  goroutine so `close` could interleave; tag `v0.1.0`, no releases) and
  `estevaofon/noxy_dynamodb` (JSON; AWS SDK; client handles are UUID
  strings; no tags, no releases; README tells the user to run `go build`).

## 1. The mechanism — one boundary, two transports

A process extension is a normal Noxy package plus a manifest, a wrapper and
one binary per platform, of which the user's machine holds exactly one:

```
noxy_libs/github_com/estevaofon/noxy_terminal/
├── noxy_ext.toml                                  # kind = "process"
├── terminal.nx                                    # typed wrapper, unchanged idiom
└── bin/
    └── noxy-plugin-terminal-windows-amd64.exe     # this platform's asset, fetched by --get
```

Load flow at `use github_com.estevaofon.noxy_terminal.terminal`:

1. `resolveModule` resolves the wrapper as today.
2. `ensureExtensionLoaded(dir)` parses the manifest. `kind` selects the
   backend. For `kind = "process"`: `min_noxy`, the name-collision check and
   the atomic export pre-check run unchanged; the host resolves
   `binaries["<GOOS>-<GOARCH>"]` (§7) — no entry is a load error naming the
   platform and the available ones; the file `dir/bin/<asset>` must exist
   (missing = error `run 'noxy --get' to download it`); the manifest and the
   binary are hash-verified against `noxy.sum` (§8.4, same TOFU rules as
   wasm); a `processBackend` is constructed **without starting the process**
   (§4.1); each export is registered through the same
   `DefineContextualNativeWithSignature` call the wasm path uses.
3. The wrapper's top level runs. Nothing downstream is extension-specific.

The seam becomes an interface:

```go
// internal/ext
type Backend interface {
    Call(ctx context.Context, fnIndex int, args []value.Value) (value.Value, error)
    Close(ctx context.Context) error
}
```

`*ext.Module` (wasm) already satisfies it; `*ext.Process` is the new
implementation (`internal/ext/process.go`, with `process_unix.go` /
`process_windows.go` for the death-signal / job-object details of §4.5).
`SharedState.Ext` becomes `map[string]ext.Backend`. `internal/plugin` (the
JSON embryo) is untouched until its removal (§10).

## 2. The protocol — `noxy-plugin/1`

### 2.1 Transport

- The child's **stdin** carries host→plugin frames; its **stdout** carries
  plugin→host frames. Both are reserved for the protocol: a stray
  `fmt.Println` on stdout is a protocol violation (§6) — the SDK guards
  against it (§9.4).
- The child's **stderr** is inherited from the host, unstructured, exactly
  as today: crash dumps and ad-hoc debugging land in the user's terminal.
  Structured diagnostics go through LOG frames (§2.6).
- Streams are **binary**. Go pipes are binary on every OS; SDKs in other
  languages must switch stdio to binary mode on Windows.
- The host starts the executable by **absolute path**, with **no
  arguments**, the host's **environment** and **working directory**, so a
  relative path passed by the script means the same thing to the extension
  as to the VM's own file builtins. There is no `PATH` lookup, ever: the
  file whose hash was verified is the file that runs.
- Frames are the only unit; there is no line structure, no text.

### 2.2 Frame layout

```
offset  size  field
0       4     length   u32 LE — number of bytes after this field (header + body); >= 12
4       1     kind     u8     — §2.3
5       1     flags    u8     — must be 0 in v1
6       2     reserved u16    — must be 0 in v1
8       4     id       u32 LE — call id (§2.5); 0 for frames outside a call
12      4     fn       u32 LE — export index for CALL; 0 otherwise
16      ...   body     NXB, kind-dependent
```

The header is 12 bytes; `length` covers header and body. Maximum `length`
is `Limits.MaxBytes + 12` (64 MiB + 12 by default, the NXB cap). A receiver
that reads a `length` below 12 or above the cap, a non-zero `flags` or
`reserved`, or an unknown `kind` has lost framing and **must not try to
resynchronize**: the host treats it as a trap (§6), the plugin exits with
status 2 and a message on stderr.

### 2.3 Frame kinds

| kind | name   | direction     | id        | fn    | body |
|------|--------|---------------|-----------|-------|------|
| 0x01 | HELLO  | both          | 0         | 0     | NXB map (§2.4) |
| 0x02 | CALL   | host → plugin | n ≥ 1     | index | NXB argument list: u32 count + values (byte-identical to `ext.EncodeArgs`) |
| 0x03 | RESULT | plugin → host | n         | 0     | one NXB value |
| 0x04 | ERROR  | plugin → host | 0 or n    | 0     | NXB map, required key `message: string` |
| 0x05 | LOG    | plugin → host | 0         | 0     | NXB map `level: int`, `message: string` |
| 0x06 | CANCEL | host → plugin | n         | 0     | empty (`length` = 12) |

Every kind is known within v1; there is no "skip unknown frames" rule (a
skipped CALL would hang the caller). Additive evolution happens inside the
map bodies (§2.8).

### 2.4 Handshake

The host writes HELLO immediately after starting the process:

```
{ "protocol":  "noxy-plugin/1",
  "noxy":      "v0.23.0",                       # informational
  "extension": "terminal",                      # manifest name
  "exports":   ["terminal_is_terminal", "terminal_open_raw",
                "terminal_read_key", "terminal_close"] }   # manifest order = fn index
```

The plugin answers with HELLO:

```
{ "protocol": "noxy-plugin/1",
  "sdk":      "noxyplugin-go/0.1.0" }           # informational
```

or with ERROR (`id` 0), for example `{ "message": "no handler for export
\"terminal_open_raw\"" }`.

Rules:

- The plugin's first frame must be HELLO or ERROR; anything else, or no frame
  within `handshake_timeout_ms` (§7; the window starts at process start and
  also covers exec latency), is a trap: `extension 'terminal' trapped:
  handshake: <reason>`.
- `protocol` must be exactly `noxy-plugin/1` in both directions in this
  version (negotiation rule for later versions in §2.8).
- **Exports bind by name at handshake.** The host sends the manifest's
  export names in index order; the plugin builds its dispatch table from
  that list and refuses the handshake if any name has no handler. Extra
  handlers in the plugin are allowed. Dispatch after the handshake is by
  integer index, as in wasm — but a binary built from an older export list
  fails at first use with the missing name, never by mis-dispatching an
  index.
- The host sends no CALL before the plugin's HELLO. The plugin may send LOG
  at any time after receiving the host's HELLO.

### 2.5 Calls

- The host assigns `id` from a counter per plugin process, starting at 1 and
  skipping 0 on wrap; an `id` is unique among in-flight calls.
- For each CALL the plugin sends **exactly one** RESULT or ERROR with the same
  `id`, in any order relative to other calls (out-of-order completion is
  expected in `concurrent` and `stateless` modes, §5).
- RESULT's body is one NXB value, checked by the host against the manifest's
  declared return type with the existing `checkDeclaredReturn`; a violation
  is a runtime error without poisoning, as in wasm §6.
- A RESULT/ERROR whose `id` is not in flight (unknown, or already answered)
  is a protocol violation: the plugin's bookkeeping is broken and the stream
  can no longer be trusted (§6). The only exception is a late reply to a
  cancelled call (§4.3), which the host silently drops while the id is in
  its "cancelled, awaiting reply" set.

### 2.6 Logging

LOG frames carry `level` (0 debug, 1 info, 2 warn, 3 error) and `message`.
The host writes `[ext <name>] <message>` to stderr — the same sink and format
as `nx_log` — and to `diagOut` when the VM grows one. LOG is the structured
channel; raw stderr stays the unstructured one.

### 2.7 Shutdown

The host shuts a plugin down by **closing its stdin**. On EOF the plugin must
stop accepting work, cancel or abandon in-flight calls, and exit; the host
waits up to a fixed grace period (2 s, a host constant) and then kills the
process. Exit status at shutdown is not an error.

EOF-means-exit is **normative for every plugin, SDK or not**: it is the one
orphan guard that works on all three operating systems, because the pipe
closes whenever the host dies, however it dies (§4.5).

### 2.8 Evolution

- The version string is `noxy-plugin/<n>`. v1 freezes: the frame layout,
  kinds 0x01–0x06 and their header rules, the body of each kind, NXB v1
  (append-only tags; `0x09` stays reserved for packed arrays).
- **Additive within v1:** new keys in the HELLO, ERROR and LOG maps.
  Receivers ignore unknown keys. That is the whole additive channel, by
  design — `flags` and `reserved` are not an extension point in v1.
- **Breaking → `noxy-plugin/2`.** Negotiation rule, fixed now so that v1
  plugins keep working under a v2 host: the host's HELLO keeps
  `protocol = "noxy-plugin/1"` and adds `protocols = ["noxy-plugin/2",
  "noxy-plugin/1"]`; a plugin echoes in `protocol` the version it will speak,
  which must be `protocol` or one of `protocols`; a v1 plugin ignores the new
  key and echoes v1. The host serves both versions for at least two minor
  releases, mirroring wasm §10.
- The manifest's `abi` field stays at 1: the manifest schema is shared with
  wasm, and process-specific keys are additive (§7).

## 3. Data contract

Wasm §3 applies verbatim: **everything crosses by copy** in NXB; functions,
closures, natives, channels, waitgroups, tasks and `ref` values do not cross
(the encoder rejects them at the boundary, naming the extension); struct
instances go out as name + fields and come back as struct-shaped maps; the
wrapper `.nx` is the typed façade, the manifest is the wire truth.

Two differences, both consequences of "one process, not one instance":

- **Extension-held state** lives in the process behind an **integer handle**
  minted by the plugin and wrapped in a struct by the `.nx` (the sqlite
  idiom). Because there is a single process, a handle stays valid across
  concurrent calls — the reason `concurrent` mode can allow `stateful`
  exports (§5). `noxy_dynamodb`'s UUID-string ids are replaced by integer
  handles in the migration (§10.3).
- **No memory bound.** There is no `memory_max_mb`; the operating system is
  the limit, and an extension that exhausts it dies like any process (§6).

## 4. Lifecycle

### 4.1 Load and lazy start

At `use`, the host validates everything it can validate without running the
binary (§1) and registers the exports. The process is started **on the
first call to any export**, not at load. Two reasons: a wrapper imported for
its types alone, or a program in a test mode (`space_invaders.nx --smoke`),
pays nothing and needs no working binary at run time; and startup errors are
reported at the call that needed the extension, with the Noxy stack of that
call. Concurrent first calls share one start: they wait on the same
handshake and all observe its result.

### 4.2 Start

`exec` by absolute path (§2.1); stdin and stdout as pipes, stderr inherited;
platform death guard (§4.5); write HELLO; wait for the reply under
`handshake_timeout_ms`. Failure to exec, a wrong first frame, a version
mismatch, a missing handler or a timeout → `extension 'x' trapped: start:
<os error>` or `trapped: handshake: <reason>`; the process (if any) is
killed; the extension is poisoned or, with `restart = true`, retried on the
next call (§4.4).

### 4.3 Per-call timeout and cancellation

Every call runs under a deadline: the export's `timeout_ms`, else the
manifest's `call_timeout_ms`, else 30 000 ms (the wasm default). `0` means
no deadline — required for calls that block by design, such as
`terminal_read_key`.

The host cannot preempt code inside another process, so on expiry it asks:
it sends CANCEL with the call's `id`, moves the id to a "cancelled, awaiting
reply" set, and returns `extension 'x' timed out: terminal_read_key exceeded
30000 ms` to the caller immediately. In `single` mode the caller itself
waits for the plugin's reply to CANCEL (at most the cancel grace) before
returning, because the mode's mutex must not be released while the plugin
is still busy; in the other modes the wait happens in the background. The
plugin must answer the cancelled
call (RESULT or ERROR — the SDK cancels the handler's `context.Context` and
replies with whatever comes back) within a cancel grace period (1 s, host
constant); the host drops that reply. A plugin that does not reply in time
is not cooperating: the host kills it and the failure becomes a trap for
every in-flight call (§6), with poisoning.

This is the one place tier B deliberately differs from wasm, where a timeout
is always a trap: a single slow DynamoDB query must not destroy the clients
every other task is using.

### 4.4 Death, poisoning, restart

When the plugin's stdout hits EOF or the process exits, every in-flight call
fails with `extension 'x' trapped: process exited (status N)` and the
extension is **poisoned**: later calls fail with `extension 'x' is poisoned
by an earlier trap` until the host process ends — the wasm rule.

`restart = true` in the manifest changes the last step: the next call starts
a fresh process (full handshake). It is only valid with
`concurrency = "stateless"`, because a handle minted by the dead process
would dangle in the new one; the manifest parser rejects the other
combinations.

### 4.5 Host exit and orphans

`SharedState.CloseExtensions()` closes every backend (§2.7: close stdin, wait
≤ 2 s, kill). It is called from every exit path: `runWithConfig` after
`Interpret` returns (success or error), the REPL's exit, `sys_exit` before
its `os.Exit`, and `main`'s panic recovery. Tasks still blocked in a call at
that moment receive a trap; the process is exiting anyway.

Belt and braces for the paths no hook can cover (host killed by a signal,
`kill -9`, a crash in Go's runtime):

- Linux: `SysProcAttr.Pdeathsig = SIGKILL`.
- Windows: the child is assigned to a job object created with
  `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` (`golang.org/x/sys/windows`); the job
  handle dies with the host.
- macOS: no kernel facility; the EOF rule (§2.7) is the guard.

### 4.6 Windows

Assets for `windows-*` end in `.exe` (manifest validation, §7). The child
inherits the console, so an extension that wants the terminal opens
`CONIN$`/`CONOUT$` (as `noxy_terminal` does today) while stdio stays the
protocol channel. No `CREATE_NO_WINDOW` or new-console flags: the extension
is a helper of the console program that started it.

## 5. Concurrency

One process per extension, multiplexed by `id`. The manifest's `concurrency`
picks how many calls the host lets in flight:

| value | in flight | `stateful` exports | `restart` | meaning |
|---|---|---|---|---|
| `single` (default) | 1 | allowed | no | the host serializes calls; the plugin may be single-threaded |
| `stateless` | unbounded | forbidden | allowed | pure functions of their arguments; a dead process loses nothing |
| `concurrent` (process only) | unbounded | allowed | no | the plugin serves calls concurrently and its handles are process-wide |

`concurrent` has no wasm counterpart because a wasm instance cannot be
entered concurrently while a process can; it is the mode an I/O extension
with connections wants (`noxy_dynamodb`) and the mode that lets
`terminal_close` interleave with a blocked `terminal_read_key`. The parser
rejects `concurrent` under `kind = "wasm"`.

Host implementation: writes happen under a mutex from the calling goroutine
(a frame is written whole); one reader goroutine per process demultiplexes
RESULT/ERROR by `id` into per-call channels, prints LOG, and on EOF fails
every pending call (§4.4). `single` adds a call-level mutex around the
CALL/reply exchange — not around the start — held, after a timeout, until
the cancelled call is answered or the process is killed (§4.3), so the next
caller never talks to a plugin that is still busy. No ordering is promised
across ids.

## 6. Errors

The two-channel philosophy is unchanged: raise for bugs, results for data.
Every failure at the boundary is a **runtime error with the Noxy stack of the
call site**, capturable with `call_result`, whose `_result` twins the wrapper
builds in pure Noxy. Nothing ever returns `null` plus a stderr line.

| situation | error text | poisons |
|---|---|---|
| ERROR frame for the call | `extension 'x' failed: <message>` | no |
| RESULT violates the declared return type | `extension 'x': result does not match declared return type "T"` | no |
| deadline expired, plugin answered CANCEL in time | `extension 'x' timed out: <export> exceeded <N> ms` | no |
| deadline expired, no reply to CANCEL within the grace | `extension 'x' trapped: <export> exceeded <N> ms and did not cancel; process killed` | yes |
| exec failure / handshake failure | `extension 'x' trapped: start: <os error>` / `trapped: handshake: <reason>` | yes* |
| process exited or stdout closed | `extension 'x' trapped: process exited (status N)` | yes* |
| malformed frame, unknown id, non-zero flags, NXB decode error | `extension 'x' trapped: protocol violation: <detail>` (process killed) | yes* |
| any call after poisoning | `extension 'x' is poisoned by an earlier trap` | — |

\* with `restart = true` the next call restarts instead (§4.4).

`failed` and `trapped` keep the wasm meaning (declared failure vs. host-
detected breakage); `timed out` is new and exists only because a process
can be cancelled cooperatively (§4.3). Wrapper authors documenting a
stateful extension must say that a trap loses its handles, as wasm requires.

Plugin-side rules: a handler error becomes ERROR; the SDK recovers a handler
panic into ERROR `panic: <value>` and keeps serving (a panic in one request
must not destroy the connections of every other task — this is the process
analogue of "a recovered panic is not a trap"); a malformed frame from the
host makes the plugin exit with status 2.

## 7. Manifest

The same `noxy_ext.toml`, with `kind` choosing the transport. `kind` defaults
to `"wasm"`, so every existing manifest keeps its meaning.

```toml
name = "terminal"
abi = 1
kind = "process"                  # "wasm" (default) | "process"
min_noxy = "0.23.0"
concurrency = "concurrent"        # single (default) | stateless | concurrent (process only)
capabilities = ["tty"]            # process: declarative, shown by --get, never enforced
call_timeout_ms = 30000           # process only; default 30000; 0 = no deadline
handshake_timeout_ms = 5000       # process only; default 5000
restart = false                   # process only; needs concurrency = "stateless"

[binaries]                        # process only; "<GOOS>-<GOARCH>" = release asset name
linux-amd64   = "noxy-plugin-terminal-linux-amd64"
linux-arm64   = "noxy-plugin-terminal-linux-arm64"
darwin-amd64  = "noxy-plugin-terminal-darwin-amd64"
darwin-arm64  = "noxy-plugin-terminal-darwin-arm64"
windows-amd64 = "noxy-plugin-terminal-windows-amd64.exe"
windows-arm64 = "noxy-plugin-terminal-windows-arm64.exe"

[[export]]
name = "terminal_read_key"        # must start with "<name>_", as today
params = []
returns = "string"
stateful = false
timeout_ms = 0                    # process only; overrides call_timeout_ms; 0 = no deadline
```

Validation, on top of today's rules (unknown keys are errors; export names,
prefix, duplicates, type vocabulary `int float bool string bytes any T[]
map[K]V Struct void` unchanged):

- `kind = "process"` **requires** `[binaries]` with at least one entry.
  Keys match `^[a-z0-9]+-[a-z0-9]+$` (the host looks up
  `runtime.GOOS + "-" + runtime.GOARCH`); values match `^[A-Za-z0-9._-]+$`
  — a file name, no path — and values under `windows-*` keys end in `.exe`.
- `kind = "process"` **rejects** `wasm`, `memory_max_mb` (no sandbox, no
  cap). `capabilities` is accepted as a free-form list of
  `^[a-z][a-z0-9_]*$` names (suggested vocabulary: `net`, `fs`, `env`,
  `exec`, `tty`), displayed by `--get` and by nothing else.
- `kind = "wasm"` **rejects** `binaries`, `call_timeout_ms`,
  `handshake_timeout_ms`, `restart`, `timeout_ms`, and `concurrency =
  "concurrent"`; `capabilities` stays rejected there (M2 suspended).
- `restart = true` requires `concurrency = "stateless"`.
- Timeouts are non-negative integers of milliseconds. `handshake_timeout_ms
  = 0` means no handshake deadline (an author's choice; the default is
  5000).

Exports, `params`, `returns` and `stateful` mean exactly what they mean for
wasm; the wrapper `.nx` and `signatureTypeName` translation are shared.

## 8. Package manager

### 8.1 What `--get` does for `kind = "process"`

`noxy --get github.com/estevaofon/noxy_terminal@v0.2.0`:

1. Clone fresh. **The package directory is replaced on every `--get`**
   (clone into a temporary directory, then swap) — the current "exists →
   `git pull`" branch cannot update anything once `.git` is gone, and with
   binaries on disk a stale directory would silently keep an old asset.
2. **Version.** If the user gave one, check it out as today. Otherwise read
   `noxy_ext.toml` from the default branch: if it says `kind = "process"`,
   assets hang off a release, so a tag is required — resolve the newest
   semver tag with `git ls-remote --tags` (no forge API), print it
   (`Resolved github.com/estevaofon/noxy_terminal to v0.2.0`), check it out
   and record it in `noxy.mod`; no tags → error telling the author to
   publish a release. Any other package stays on `HEAD` as today. Remove
   `.git`.
3. Parse the checked-out `noxy_ext.toml` — the authoritative one. If `kind`
   is not `process`, continue as today.
4. **Checksums.** Download `<base>/checksums.txt` (§8.2), `sha256sum` format:
   `<hex>  <asset>` per line. Every `[binaries]` value must appear; a missing
   one is an error naming it — the manifest promises binaries the release
   does not carry.
5. **Platform asset.** Look up `[binaries]["<GOOS>-<GOARCH>"]` of the
   machine running `--get`. Missing → error: `noxy_terminal v0.2.0 has no
   binary for windows/arm64 (published: linux/amd64, linux/arm64, ...)`.
   Download it to `<pkg>/bin/<asset>` (streamed to a temporary file, renamed
   into place), verify its sha256 against `checksums.txt` (mismatch → delete
   + error naming both hashes), `chmod 0755` on POSIX.
6. **Lockfile.** Record in `noxy.sum`: `<pkg> noxy_ext.toml sha256:<hex>`
   (as today) and **one line per `[binaries]` entry**, `<pkg> bin/<asset>
   sha256:<hex from checksums.txt>` — the hashes of assets this machine did
   not download included. A committed `noxy.sum` therefore verifies a
   teammate's macOS download and a Lambda's Linux download without either
   machine having seen the other's binary.
7. Print the declared `capabilities`, if any:
   `noxy_terminal declares: tty`.

There is no compile-from-source fallback, by design (invariant 4). An author
who wants a platform the matrix does not cover adds it to the matrix.

### 8.2 Where assets come from

For a package path `github.com/<user>/<repo>` and tag `<tag>`, the base URL
is `https://github.com/<user>/<repo>/releases/download/<tag>/`. Forges with
the same layout (Gitea, Codeberg) work by the same rule; a manifest-level
URL template and authenticated downloads are follow-ups (§15). Downloads use
Go's `net/http` with a 60 s timeout per file; no browser, no quarantine
attribute on macOS (Go's linker ad-hoc signs `darwin/arm64` binaries, so
they run unsigned from the command line).

### 8.3 What the author publishes

```sh
mkdir -p dist
for os in linux darwin windows; do for arch in amd64 arm64; do
  ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags=-s \
    -o "dist/noxy-plugin-terminal-$os-$arch$ext" .
done; done
(cd dist && sha256sum * > checksums.txt)
gh release create v0.2.0 dist/*
```

The `[binaries]` table lists exactly those names. The SDK ships this script
and a GitHub Actions workflow that runs it on tag push (§9.5).

### 8.4 Runtime verification

`verifyExtensionSum` generalizes from "manifest + wasm" to "manifest + the
artifact the backend will run": for process extensions that is
`bin/<asset>` of the running platform. Rules are unchanged: the manifest
hash is checked first (it decides which artifact to verify), a mismatch
refuses the load, a package with no entries at all loads with the TOFU
warning, and layouts outside `noxy_libs` (an author's checkout with a locally
built `bin/`) are not verified.

### 8.5 Development layout

An author runs their extension without the matrix: `[binaries]` names their
own platform, `go build -o bin/<asset> .`, `use` from a project whose
`noxy_libs` symlinks or copies the checkout. The wasm story is the same.

## 9. SDK (Go)

### 9.1 Where it lives

A **nested Go module in this repository**: `sdk/noxyplugin/go.mod` with
`module github.com/estevaofon/noxy/sdk/noxyplugin`, tagged
`sdk/noxyplugin/v0.1.0`, imported as `github.com/estevaofon/noxy/sdk/noxyplugin`.
It depends on nothing from `noxy-vm` (it cannot: the root module's path is
not fetchable and `internal/` is sealed) and carries its own NXB
implementation over Go types — the wire format is the contract, and the two
ends of a wire are two codecs, not a duplicate. Conformance is enforced by
**shared golden vectors** under `internal/ext/testdata/nxb/`, read by both
test suites through the checkout.

Alternatives: a separate repository (spec, host and SDK drift; cross-repo
integration tests) or renaming the root module to
`github.com/estevaofon/noxy` (touches every import in the tree for a
by-product). The nested module keeps one PR = one protocol change.

### 9.2 API

```go
package main

import (
    "context"

    "github.com/estevaofon/noxy/sdk/noxyplugin"
)

func main() {
    p := noxyplugin.New()
    p.Handle("terminal_is_terminal", noxyplugin.Func0(isTerminal))
    p.Handle("terminal_open_raw",    noxyplugin.Func0(openRaw))
    p.Handle("terminal_read_key",    noxyplugin.Func0(readKey))
    p.Handle("terminal_close",       noxyplugin.Func0(closeTerminal))
    p.Main() // serves os.Stdin/os.Stdout, exits with the protocol's status
}

func readKey(ctx context.Context) (string, error) { /* blocks until a key or ctx.Done() */ }
```

Surface:

```go
type Handler func(ctx context.Context, args Args) (any, error)
func New() *Plugin
func (p *Plugin) Handle(name string, h Handler)
func (p *Plugin) Main()                                   // Serve + os.Exit
func (p *Plugin) Serve(r io.Reader, w io.Writer) error    // testable core
func Func0[R any](f func(context.Context) (R, error)) Handler
func Func1[A, R any](f func(context.Context, A) (R, error)) Handler
// ... up to Func5 (noxy_dynamodb's update_item takes five arguments)
func Logf(level Level, format string, args ...any)        // LOG frame
type Args []any    // Int(i), Float(i), Bool(i), String(i), Bytes(i), Map(i), Array(i), Struct(i)
type Struct struct { Name string; Fields []Field }        // inbound 0x08 keeps its name
```

`FuncN` adapters check the argument count and convert each argument to the
declared Go type, returning ERROR `argument 2: expected int, got string` on
mismatch — the plugin-side twin of the host's `checkDeclaredReturn`.

### 9.3 Type mapping

| NXB | Go in (`Args`) | Go out (return) |
|---|---|---|
| null | `nil` | `nil` |
| bool | `bool` | `bool` |
| int | `int64` (adapters also accept `int`) | `int`, `int64`, `int32`, ... |
| float | `float64` | `float64`, `float32` |
| string | `string` | `string` |
| bytes | `[]byte` | `[]byte` |
| array | `[]any` (adapters convert `[]T` for scalar `T`) | any slice |
| map | `map[string]any` when every key is a string, `map[int64]any` when every key is an int, else `map[any]any` | `map[string]V`, `map[int64]V` |
| struct | `noxyplugin.Struct` (name preserved) | `noxyplugin.Struct` or a struct-shaped `map[string]any` |

Outbound `Struct` is encoded as tag 0x08; the host turns it into a
struct-shaped map either way (wasm §3).

### 9.4 Behaviour the SDK guarantees

- Reads frames from stdin, one goroutine per CALL (the host enforces
  `single`), replies under a write mutex.
- Cancels the handler's `context.Context` on CANCEL and on stdin EOF; replies
  to cancelled calls with whatever the handler returns.
- Recovers handler panics into ERROR `panic: <value>` (§6).
- **Protects stdout**: at start it keeps the real stdout for the protocol
  and points `os.Stdout` at stderr, so a stray Go-level print cannot
  corrupt the stream (raw fd-1 writes from cgo are not intercepted).
- Exits 0 on stdin EOF after a bounded wait for handlers; exits 2 on a
  malformed frame or a write error (EPIPE means the host is gone).
- Run by hand (stdin is a terminal), prints `this program is a Noxy
  extension; install it with 'noxy --get'` and exits 2.
- Ignores SIGINT: the host owns Ctrl-C; the child leaves on EOF.

### 9.5 Deliverables

The module, its tests (frame codec, golden NXB vectors, a fake host over
`io.Pipe`), `README.md` with the ~30-line example above, the release script
of §8.3 and a GitHub Actions workflow template. The host's own integration
tests use a test plugin built **with the SDK** at test time
(`internal/ext/testdata/processguest`, `go build` via a `replace` to
`../../../../sdk/noxyplugin`, cached like `exttest.BuildGuest`), so host and
SDK are tested against each other on Linux and Windows CI. A nested module
is invisible to the root module's `go test ./...`, so CI runs the tests of
both modules (whether through a `go.work` file or two invocations is an
implementation detail). Authors in other
languages implement §2 directly; NXB plus framing is a few hundred lines
anywhere.

## 10. Migration

### 10.1 `sys_load_plugin`

- **v0.23.0 (this delivery):** the builtin keeps working. Its first call in a
  process prints once to stderr: `warning: sys_load_plugin is deprecated
  since v0.23.0 and will be removed in v0.26.0; publish the plugin as a kind
  = "process" extension (docs/EXTENSIONS.md)`. `docs/` mark it deprecated;
  the CHANGELOG states the window.
- **v0.26.0** (janela estendida na v0.25.0, que só renomeou o módulo Go): remove `sys_load_plugin`, `internal/plugin`,
  `compiler.PluginNativeNames` and its three call sites, and the JSON
  protocol from `docs/`.

`PluginNativeNames` goes **with** the builtin, not before: its only job is to
make wrappers that call `sys_load_plugin` compile (`<name>_request` is born
at run time). Removing it while the builtin still exists would turn every
such wrapper into `undefined global 'terminal_request'` at compile time —
removal without the window issue #80 asks for. Issue #80's checkbox 8 is
read as "deprecate now, remove both at the end of the window".

### 10.2 `noxy_terminal` (reference, validated on Windows and Linux)

- Manifest: `kind = "process"`, `concurrency = "concurrent"` (the server
  already serves `read_key` on a goroutine; `close` may interleave),
  `capabilities = ["tty"]`, exports `terminal_is_terminal() -> bool`,
  `terminal_open_raw() -> void`, `terminal_read_key() -> string` with
  `timeout_ms = 0`, `terminal_close() -> void`. `last_error` disappears:
  failures travel as ERROR frames.
- `main.go`: handlers over the existing `terminalRuntime`; the JSON server,
  `build_plugin.*` and the SIGPIPE shims are replaced by the SDK.
- `terminal.nx` keeps its public API (`TerminalResult`, `KeyEvent`,
  `is_terminal`, `open_raw`, `read_key`, `close`) for `space_invaders.nx`;
  `_ensure_terminal_plugin`/`sys_load_plugin` go away; failures become
  results through `call_result`:

```noxy
use errors select *

func open_raw() -> TerminalResult
    let r = call_result(terminal_open_raw)   // Result<...> typed by the callee
    if r.ok then return TerminalResult(true, "") end
    return TerminalResult(false, r.failure.message)
end
```

- Release `v0.2.0` with the six assets and `checksums.txt`; `min_noxy =
  "0.23.0"`.

### 10.3 `noxy_dynamodb` (reference, validated on Windows and Linux)

- Manifest: `kind = "process"`, `concurrency = "concurrent"`,
  `capabilities = ["net", "env"]` (AWS credentials come from the
  environment), `call_timeout_ms = 60000`; exports
  `dynamodb_connect(map[string]any) -> int` (`stateful = true`),
  `dynamodb_put_item(int, string, map[string]any) -> void`,
  `dynamodb_get_item(int, string, map[string]any) -> map[string]any`,
  `dynamodb_query`,
  `dynamodb_scan`, `dynamodb_update_item`, `dynamodb_delete_item`,
  `dynamodb_close(int) -> void`.
- Handles: `Client(handle: int)` replaces the UUID string.
- Wrapper: `connect` raises on failure instead of returning `Client("")`;
  `put_item -> bool` and friends keep their shape via `call_result`; the
  `loaded` flag and the startup warning disappear.
- Lambda note in its README: deploy `noxy_libs/.../bin/<linux asset>` with
  the execute bit and commit `noxy.sum`; `--get` on a Windows workstation
  records the Linux hashes the Lambda will verify.

## 11. Cost model and acceptance

Budget per call: two pipe crossings (~5–20 µs on Linux, ~20–60 µs on
Windows) plus NXB, linear in payload (~1 µs/KB class). Against wasm's
~4 µs this is one order of magnitude — noise for anything that touches a
socket or a disk, and the reason the granularity rule of wasm §11 stays
normative: the unit of a call is a document, a batch, a query.

Benchmarks in `internal/ext` against the SDK-built test plugin, recorded in
`docs/EXTENSIONS.md` beside the wasm numbers (issue #80 checkbox 7):

- `BenchmarkProcessRoundTripEmpty` — CALL with no arguments, `void` result.
- `BenchmarkProcessRoundTrip1KB` — `(bytes[1 KB]) -> bytes[1 KB]`.
- `BenchmarkProcessRoundTrip1MB` — `(bytes[1 MB]) -> bytes[1 MB]`.
- `BenchmarkProcessConcurrent8` — eight tasks calling a handler that sleeps
  1 ms, `concurrent` vs. `single`.

Acceptance on the Linux amd64 CI runner: empty round trip under 100 µs, 1 KB
under 120 µs, 1 MB under 10 ms, and `concurrent` at least 4× the throughput
of `single` in the last benchmark (proof that multiplexing works). Windows
numbers are recorded, not gated.

## 12. Testing

- **Unit:** frame encode/decode and header validation (`length`, `flags`,
  `kind`), limits; manifest validation per kind (every rule of §7 has a
  negative test); `noxy.sum` recording of `[binaries]` entries.
- **Host against a fake plugin** (in-process `io.Pipe` pair driven by the
  test — no subprocess): handshake mismatch and missing handler, RESULT/ERROR
  routing, out-of-order completion, LOG, EOF mid-call, malformed frames,
  timeout with CANCEL honoured and not honoured, poisoning and `restart`.
- **Subprocess:** `internal/ext/testdata/processguest` built with the SDK at
  test time, on Linux and Windows CI: lazy start, `single` serialization,
  `concurrent` interleaving (a blocking call plus a second call that
  completes first), shutdown through `sys_exit`, orphan check (host process
  killed → plugin exits within the grace).
- **End to end through the VM:** a package under `noxy_libs` with manifest,
  wrapper and `bin/`; `noxy.sum` verification including a tampered binary
  and a manifest repointed at another asset.
- **Package manager:** `--get` against an `httptest` server serving
  `checksums.txt` and one asset: happy path, missing platform, bad checksum,
  asset absent from `checksums.txt`, no tags.
- **Acceptance:** `noxy_terminal` and `noxy_dynamodb` installed with `--get`
  and exercised on Windows and Linux (issue #80 checkboxes 5–6).
- `internal/ext` tests need `GOFLAGS=-trimpath=false` on machines whose
  `go.env` sets `-trimpath` (known, not a regression).

## 13. Alternatives considered

- **JSON lines (today).** No `bytes` without base64, no int64 fidelity, no
  ids, and slower; NXB already exists on the host. Rejected.
- **msgpack** (wasm §15's original suggestion). A second codec and a
  dependency on both sides for nothing NXB lacks. Rejected.
- **gRPC / HashiCorp go-plugin.** TCP or named pipes, TLS handshakes,
  protobuf codegen, heavy dependencies; over-engineered for a CLI's helper
  process and a tax on non-Go authors. Rejected.
- **Sockets as transport.** Only needed by an extension that wants stdio for
  itself; `noxy_terminal` opens the tty directly. Follow-up on demand.
- **A process per call.** Milliseconds per call and no handles. Rejected.
- **Eager start at `use`.** Breaks `--smoke`-style runs and charges startup
  to programs that never call the extension. Rejected (issue #80).
- **Kill on timeout, no CANCEL.** Simpler, but one slow query would poison
  the extension every other task shares. CANCEL costs one frame kind.
- **Positional `fn` without name binding.** A binary built from an older
  export list would mis-dispatch silently. Rejected.
- **`stateless` as the only multiplexing mode.** Forbids the handle-holding
  I/O extension that is tier B's whole reason to exist. Hence `concurrent`.
- **Separate SDK repository / renamed root module.** §9.1.

## 14. Delivery

One release, v0.23.0, in the order of issue #80's checkboxes:

1. This spec (checkbox 1).
2. `Backend` interface, `ext.Process`, framing, handshake, timeouts, CANCEL,
   poisoning/restart, `CloseExtensions` on every exit path, manifest `kind =
   "process"`, tests against the fake plugin (checkbox 2).
3. SDK module, `processguest`, subprocess tests on both OSes (checkbox 4).
4. `--get` for process extensions, portable `noxy.sum`, fresh-clone
   semantics (checkbox 3).
5. Benchmarks and `docs/EXTENSIONS.md` (checkbox 7).
6. `noxy_terminal` migrated and released (checkbox 5).
7. `noxy_dynamodb` migrated and released (checkbox 6).
8. Deprecation warning for `sys_load_plugin`, CHANGELOG entry with the
   removal window (checkbox 8, first half; second half at v0.26.0).

Steps 2–4 are one PR to `develop`; 5–8 follow as small PRs.

## 15. Open questions and follow-ups

- **Asset URL template and authenticated downloads** for forges without the
  GitHub layout or for private releases.
- **Versioned `noxy.sum` keys.** The M1 key `<pkg> <file>` is overwritten on
  upgrade; a Go-style `<pkg>@<version>` key is part of the pending
  `noxy.sum` spec (wasm §15).
- **Socket transport**, **streaming payloads**, **callbacks into Noxy** —
  each its own design, as for wasm.
- **REPL:** a poisoned process extension in a long-lived session deserves a
  test, as wasm §15 already notes.
- **`--get` fresh-clone semantics for all package kinds** — specified here
  for every package because the current update path is a no-op; worth a line
  in `docs/PACKAGE_MANAGER.md`.
- **TLS in the core** — independent, its own issue, priority above any wasm
  M2 work (invariants revision, "Related").
