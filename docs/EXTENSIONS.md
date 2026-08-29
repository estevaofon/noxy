# Noxy WASM Extensions (experimental, M1)

Extensions let third parties ship native-performance modules — compression,
hashing, codecs, parsers — as a single platform-independent `.wasm` artifact,
loaded by the VM's embedded WebAssembly runtime (wazero). Design:
`docs/superpowers/specs/2026-08-23-wasm-extension-mechanism-design.md`.

**Scope (issue #110).** WASM extensions are for pure computation over their
arguments — buffer in, buffer out. I/O, OS access, network, drivers and SDK
bindings are the job of process plugins (tier B, issue #80); the wasm
capability plan (`capabilities`, M2) is suspended. Pick the backend by the
API's profile. Invariants: `docs/superpowers/specs/2026-08-29-extensibility-invariants-revision.md`.

## Package layout

```
my_ext/
├── noxy_ext.toml     # manifest
├── ext.wasm          # compiled extension (wasm32, no WASI)
└── my_ext.nx         # typed Noxy wrapper
```

Install with `noxy --get github.com/you/my_ext` (artifact hashes are recorded
in `noxy.sum`); import with `use github_com.you.my_ext.my_ext as my_ext`.

## Manifest reference

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

## ABI v1 summary

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

## Errors

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

## Granularity (normative)

The unit of a call is a buffer, a document, a batch — never an element. A
boundary crossing costs on the order of a microsecond plus copies; a per-item
call in a hot loop is two orders of magnitude slower than a builtin.

## Compilation cache

Loading a `.wasm` module compiles it with wazero. To avoid recompiling on
every `noxy script.nx` invocation, the loader keeps a persistent compilation
cache at `<user cache dir>/noxy/wazero` (e.g. `%LOCALAPPDATA%\noxy\wazero`
on Windows, `~/.cache/noxy/wazero` on Linux). The cache is best-effort: if it
cannot be created (read-only filesystem, missing permissions, ...), the load
proceeds without it rather than failing.

## Measured cost (M1, dev machine)

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
