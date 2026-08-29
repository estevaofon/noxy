# Noxy Extensions (wasm and process)

Extensions let third parties ship native-performance modules — compression,
hashing, codecs, parsers — as a single platform-independent `.wasm` artifact,
loaded by the VM's embedded WebAssembly runtime (wazero). Design:
`docs/superpowers/specs/2026-08-23-wasm-extension-mechanism-design.md`.

**Scope (issue #110).** WASM extensions are for pure computation over their
arguments — buffer in, buffer out. I/O, OS access, network, drivers and SDK
bindings are the job of process plugins (tier B, issue #80); the wasm
capability plan (`capabilities`, M2) is suspended. Pick the backend by the
API's profile. Invariants: `docs/superpowers/specs/2026-08-29-extensibility-invariants-revision.md`.

## WASM extensions (tier A)

### Package layout

```
my_ext/
├── noxy_ext.toml     # manifest
├── ext.wasm          # compiled extension (wasm32, no WASI)
└── my_ext.nx         # typed Noxy wrapper
```

Install with `noxy --get github.com/you/my_ext` (artifact hashes are recorded
in `noxy.sum`); import with `use github_com.you.my_ext.my_ext as my_ext`.

### Manifest reference

```toml
name = "zstd"            # ^[a-z][a-z0-9_]*$; export prefix
abi = 1                  # only 1 is supported
min_noxy = "0.18.0"      # optional minimum VM version
concurrency = "stateless" # "single" (default) | "stateless"
memory_max_mb = 64        # optional; host ceiling 256
capabilities = []         # M1: must be empty
wasm = "ext.wasm"         # optional artifact name

[[export]]
name = "zstd_compress"    # must start with "<name>_"
params = ["bytes", "int"] # int float bool string bytes any T[] map[K]V Struct
returns = "bytes"         # ... or "void"
stateful = false          # true = mints handles; forbidden under stateless
```

Unknown keys are errors. `stateless` extensions get an instance pool and may
be called concurrently; `single` extensions get one instance and calls are
serialized — required whenever the extension keeps state behind handles,
because a handle only means something to the instance that minted it.

A non-empty `capabilities` list is rejected outright: the loader errors on
`noxy_ext.toml` parsing rather than silently ignoring capabilities the host
does not implement (the capability plan is suspended — issue #110; I/O
belongs to process plugins). M1 ships zero capabilities — every extension is
a pure function of its declared arguments (see ABI v1 summary below).

Export names must match `^[a-z][a-z0-9_]*$` and start with `<name>_`. Before
registering any export, the loader checks every declared export against the
VM's existing globals; if a name collides with a stdlib native or with
another already-loaded extension's export, the whole load fails explicitly
(atomically — no partial registration, no silent shadowing).

### ABI v1 summary

The guest exports `nx_abi_version() -> u32` (return 1),
`nx_alloc(u32) -> u32`, `nx_free(u32, u32)`, and
`nx_call(fn_index: u32, args_ptr: u32, args_len: u32) -> u64` returning
`(ptr << 32) | len` of the NXB-encoded result, or 0 after calling the host's
`nx_fail(ptr, len)`. Host imports live in module `"noxy:host/v1"`: `nx_fail`
and `nx_log(level, ptr, len)`. Everything crosses **by copy** in NXB
(tag byte + little-endian scalars + u32-length blobs; tags: null 0x00,
bool 0x01, int 0x02, float 0x03, string 0x04, bytes 0x05, array 0x06,
map 0x07, struct 0x08). Functions, channels, tasks and `ref` values do not
cross. Structs arrive back in Noxy as struct-shaped maps.

Target `wasm32-unknown-unknown` (Rust) or equivalent. WASI is **not**
provided: an extension importing anything outside `noxy:host/v1` fails to
load, which is also the permission model — a capability-free extension is a
pure function of its arguments. A complete minimal guest in Rust lives at
`internal/ext/testdata/rustguest/` (allocator, `nx_call` dispatch, `nx_fail`,
NXB bytes result) — copy it as your starting point.

### Errors

`nx_fail` + return 0 → Noxy runtime error `extension 'x' failed: <msg>`
(capturable with `call_result`). A trap (out-of-bounds, unreachable, memory
cap, host-side call timeout) → `extension 'x' trapped: ...` and the instance
is **poisoned**: closed and, in `single` mode, the whole extension refuses
further calls in this process. State held by a poisoned instance is gone —
document that in your extension's README.

Every `nx_call` runs under a host-side timeout — 30 seconds by default,
enforced by cancelling the guest's execution context. A guest stuck in an
infinite loop does not hang the host process: the call expires, surfaces as
a trap, and poisons the instance exactly like any other trap.

### Granularity (normative)

The unit of a call is a buffer, a document, a batch — never an element. A
boundary crossing costs on the order of a microsecond plus copies; a per-item
call in a hot loop is two orders of magnitude slower than a builtin.

### Compilation cache

Loading a `.wasm` module compiles it with wazero. To avoid recompiling on
every `noxy script.nx` invocation, the loader keeps a persistent compilation
cache at `<user cache dir>/noxy/wazero` (e.g. `%LOCALAPPDATA%\noxy\wazero`
on Windows, `~/.cache/noxy/wazero` on Linux). The cache is best-effort: if it
cannot be created (read-only filesystem, missing permissions, ...), the load
proceeds without it rather than failing.

### Measured cost (M1, dev machine)

Numbers below are from `internal/ext`'s benchmarks on the author's dev
machine (`go test -bench` under `internal/ext`) — a snapshot, not a
portability guarantee:

- Rust guest boundary round-trip, 1 KB `bytes` payload: **≈3.5–4.0 µs**
  (`BenchmarkRustRoundTrip1KB`).
- Rust `sha256` of 1 MB: **≈7–8×** the Go native `crypto/sha256` (asm/SHA-NI)
  time for the same input (`BenchmarkRustSHA256_1MB` vs.
  `BenchmarkNativeSHA256_1MB`).
- Binary size delta from embedding wazero: **≈4.0 MiB**.
- Module load, compilation cache warm: **≈29 ms**.
- Module load, compilation cache cold (no cache directory): **≈565 ms**.

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
author's machine (Windows 11, 11th Gen Intel Core i7-1165G7, 8 logical
CPUs, `GOFLAGS=-trimpath=false`):

```
BenchmarkProcessRoundTripEmpty-8               53938        45309 ns/op
BenchmarkProcessRoundTrip1KB-8                 47551        49856 ns/op     20.54 MB/s
BenchmarkProcessRoundTrip1MB-8                    627      3873768 ns/op    270.69 MB/s
BenchmarkProcessConcurrent8/single-8             1734      1414920 ns/op
BenchmarkProcessConcurrent8/concurrent-8        13261       182567 ns/op
```

Reading: empty round trip ≈45 µs; 1 KB round trip ≈50 µs against ≈4 µs for
the wasm 1 KB round trip (the extra cost is the OS pipe/process boundary,
not the protocol); 1 MB ≈3.9 ms, dominated by the copy across the pipe;
`concurrent` mode delivers ≈7.7× the throughput of `single` mode with a
handler that sleeps 1 ms, since `single` serializes calls on one instance
while `concurrent` multiplexes them over the same process (Windows, 8
CPUs).
