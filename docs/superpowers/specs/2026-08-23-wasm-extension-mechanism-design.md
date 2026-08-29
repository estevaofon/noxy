# WASM Extension Mechanism — Design

RFC: issue #78 (extensibility strategy for native modules). This spec designs
the recommended approach from that RFC: WebAssembly extensions executed by an
embedded pure-Go runtime (wazero). The out-of-process plugin protocol
(`sys_load_plugin`, `internal/plugin`) remains as a second tier for use cases
WASM cannot express; formalizing it is a separate spec. In-process dynamic
libraries were rejected in the RFC for violating invariant 5 and are not
revisited here.

> **Amended 2026-08-29 (issue #110).** The RFC's five invariants were revised
> by the author — see `2026-08-29-extensibility-invariants-revision.md`.
> Invariant 5 ("the VM cannot be destabilized") is no longer a requirement;
> "cross-compile, and the user never compiles" is the decisive filter.
> Consequences for this document: tier B (process plugins) is the **primary**
> path for I/O, OS access and drivers — not a second tier; the capability
> plan of §9 and phase M2 of §14 are suspended; approach C stays out for the
> cross-compilation cost it imposes on authors, not for invariant 5. The
> original text is kept as written, with amendment notes where superseded.

## Goal and Scope

**Goal:** a third party can publish a native-performance module — compression,
crypto, parsing, format codecs — as a single platform-independent artifact
that `noxy --get` installs and a Noxy program imports like any stdlib module,
without a PR to this repository, without a VM release, and without the user
compiling anything.

**Non-goals:**

- Import syntax changes. Loading rides the existing `use` / `resolveModule`
  path (§7); no new keywords.
- Registry or repository design, and `noxy.sum` integrity format — a
  prerequisite (§8) but its own spec.
- Migrating any existing core module out of the binary. §14 names candidates
  and the measurement that gates them; the extraction itself is future work.
- Exposing VM values by reference across the boundary (a host-side handle
  table with accessor imports). Deliberately deferred — §3 explains why the
  copy-only contract is the v1 choice and what evidence would justify the
  extension.
- WASI. Extensions target bare `wasm32-unknown-unknown`; every capability is
  an explicit host import (§9). No `wasi_snapshot_preview1` is provided.

## Constraints from the current implementation

Facts this design leans on, verified against the tree:

- The build is already pure Go with `CGO_ENABLED=0` (CI enforces it;
  sqlite is `modernc.org/sqlite`). wazero is the only mature Go WASM runtime
  that preserves this — wasmtime-go and wasmer-go both require cgo. wazero is
  itself dependency-free and has a compiling engine on amd64/arm64 with an
  interpreter fallback elsewhere.
- A native is an `ObjNative` (`internal/value/native.go`) bound in the shared
  root environment; `NativeSignature{Arity, Params, ReturnType}` drives
  runtime validation in `internal/vm/call_validation.go`. Extensions plug in
  at exactly this seam — the executor cannot tell an extension native from a
  built-in one.
- Static typing for modules comes from typed `.nx` wrappers over untyped
  global natives: `internal/stdlib/sqlite.nx` wraps `sqlite_open`,
  `internal/compiler/module_exports.go` reads the wrapper's declarations.
  Extensions reuse this pattern unchanged (§7), so **no compiler changes are
  required**.
- `Value` (`internal/value/value.go`) is a 32-byte tagged struct whose layout
  is test-locked and actively evolving (perf series #66). Nothing of it may
  leak into the ABI.
- Natives already return struct-shaped maps that typed annotations admit
  structurally (the `call_result` envelope precedent,
  2026-08-19-call-result-design.md §"Representation"). Extension return
  values reuse that admission rule (§3).
- Extension-held state already has a stdlib idiom: the sqlite module keeps
  resources host-side behind an integer id wrapped in a struct
  (`Database(0, false)`). Extensions keep their state guest-side behind the
  same shape (§3).
- The package manager (`internal/pkgmanager`) clones git repos into
  `noxy_libs/` with no checksums. Shipping executable artifacts makes
  integrity verification non-optional (§8).

## 1. The mechanism

An extension package is a normal Noxy package plus two files at its root:

```
noxy_libs/github_com/acme/zstd/
├── noxy_ext.toml     # manifest: name, ABI version, exports, capabilities
├── ext.wasm          # one artifact, all platforms
└── zstd.nx           # typed wrapper, same pattern as internal/stdlib/*.nx
```

Load flow, at module resolution time:

1. `resolveModule` (`internal/vm/modules.go`) resolves `use
   github_com.acme.zstd.zstd` to `zstd.nx` as it does today.
2. Before executing the module's top level, the VM checks the package root
   for `noxy_ext.toml`. If present and not yet loaded (keyed in the module
   cache, like `.nx` modules), it: parses and validates the manifest, checks
   `abi` and `min_noxy`, checks the capability grants (§9), instantiates
   `ext.wasm` under wazero, performs the version handshake, and registers one
   `ObjNative` per manifest export into `shared.Root` — with a
   `NativeSignature` built from the manifest, via the same
   `DefineNativeWithSignature` path builtins use.
3. The module's `.nx` top level then runs; its typed wrapper functions call
   the freshly registered natives. From here on, nothing downstream is
   extension-specific.

Export names are required to start with `<manifest name>_` (`zstd_compress`),
matching the `io_*`/`net_*`/`sqlite_*` convention and making global-namespace
collisions a manifest-validation error rather than a silent rebind
(`DefineLocalIfAbsent` would otherwise drop the second registration quietly).

Loading is idempotent and concurrent-safe for the same reasons module loading
is: the module cache dedupes, and `bindingStore` serializes definition.

## 2. The ABI

Two named, versioned surfaces: what the guest exports, and what the host
imports provide. Version 1 of both is frozen by this spec; evolution rules in
§10.

### Guest exports (required)

```
nx_abi_version() -> u32            ; must return 1
nx_alloc(size: u32) -> u32         ; guest allocator; host writes args here
nx_free(ptr: u32, size: u32)
nx_call(fn_index: u32, args_ptr: u32, args_len: u32) -> u64
```

`fn_index` is the export's position in the manifest (0-based), fixed at load
time — the guest dispatches on an integer, not a string. The return value
packs `(ptr << 32) | len` of the NXB-encoded result (§2, encoding) in guest
memory; `0` means "failed — the failure was reported via `nx_fail`".

### Host imports (module `"noxy:host/v1"`)

```
nx_fail(msg_ptr: u32, msg_len: u32)      ; set this call's failure message
nx_log(level: u32, ptr: u32, len: u32)   ; diagnostics to the VM's diagOut
```

That is the entire unconditional surface. Everything else — clocks, random,
filesystem, network — is a capability import (§9) that only instantiates if
granted. An extension importing anything outside `noxy:host/v1` plus its
granted capability modules fails at instantiation with a diagnostic naming
the offending import.

### Calling convention

For each call the host: encodes the argument list to NXB, calls
`nx_alloc(len)`, copies the bytes in, calls `nx_call(idx, ptr, len)`, decodes
the result from the returned region, and calls `nx_free` on both regions.
The guest owns its memory; the host owns the copies. **No pointer into guest
memory survives the call in either direction**, which is what makes §4
trivial.

### NXB encoding (v1)

A minimal deterministic binary encoding of the Noxy value tree — one tag
byte, little-endian fixed-width scalars, length-prefixed variable data:

```
0x00 null
0x01 bool      + 1 byte
0x02 int       + i64
0x03 float     + f64 (IEEE bits)
0x04 string    + u32 len + UTF-8 bytes
0x05 bytes     + u32 len + bytes
0x06 array     + u32 count + elements
0x07 map       + u32 count + (key, value) pairs   ; keys: int or string
0x08 struct    + string name + u32 count + (field name, value) pairs
```

Tag `0x09` is **reserved** for homogeneous packed arrays — `int[]`/`float[]`
as one contiguous little-endian block with no per-element tag, so a 1M-element
`int[]` costs one header instead of a tag per element. It is a planned
additive extension, not part of v1; do not repurpose the tag.

NXB is host-internal vocabulary shared with guest SDKs; it is not exposed to
Noxy code. Functions, closures, natives, channels, waitgroups, tasks, and
`ref` values have no encoding and are rejected at the boundary (§3). Nesting
depth is capped (64) and total encoded size per crossing is capped
(host-configurable, default 64 MB) — both produce runtime errors naming the
extension, not truncation.

## 3. Data contract

**Everything crosses by copy.** Scalars, strings, bytes, arrays, maps, and
struct instances are NXB-encoded on the way in and decoded on the way out.
This is the single most consequential decision in the design, so its
rationale is recorded:

- The alternative — VM values pinned host-side behind handles, with
  `array_get`/`map_get`/`struct_set` accessor imports — is a second stdlib:
  a wide, forever-compatible API surface that would recreate at the ABI layer
  exactly the maintenance concentration issue #78 exists to shed, and it
  would couple the ABI to the semantics of `internal/value` CoW just as that
  machinery is being rebuilt for performance.
- The realistic first wave of extensions (compression, hashing, codecs,
  parsers) are value-in/value-out transformers. Copying a buffer that the
  extension was going to read in full anyway costs one memcpy on top of
  irreducible work.
- Copy-only makes the GC story (§4) and the concurrency story (§5) simple
  enough to state in one paragraph each, and it makes extensions
  automatically respect Noxy's value semantics — a guest cannot mutate a
  caller's array because it never sees it.

**Consequences, stated plainly:**

- `ref` parameters do not cross. A manifest export cannot declare a `ref`
  parameter; the loader rejects the manifest. An extension that needs to
  "mutate" a value returns the new value.
- Callables do not cross. No callbacks from guest to Noxy code in v1
  (follow-up in §15 — the task-boundary vocabulary would carry it).
- Struct instances cross outward as name+fields and return inward as
  **struct-shaped maps**, exactly like the `call_result` envelope: typed
  annotations admit them structurally, nominal gates (`spawn_task` argument
  validation) reject them. The wrapper `.nx` idiom already in use — have the
  native fill a wrapper-constructed instance, or annotate the map
  structurally — carries over. This is a known, already-documented seam, not
  a new one. *(Later note, 0.22.0 — issue #105: the `call_result` envelope is
  now a genuine `errors.Result<T>` instance, not a struct-shaped map; the seam
  described here remains for maps returned by natives — spec §7,
  "Representation".)*

**Extension-held state** stays inside the guest instance, behind an integer
id the guest mints, wrapped in a struct by the `.nx` wrapper:

```noxy
// zstd.nx
struct Compressor
    handle: int
end

func new_compressor(level: int) -> Compressor
    return Compressor(zstd_new_compressor(level))
end
```

This is the sqlite idiom verbatim, with the table living guest-side instead
of in `SharedState`. The guest frees its table entries on explicit close
calls; everything is reclaimed when the instance closes. A stale id is the
extension's error to report (via `nx_fail`), same as a closed `Database`
today.

**Large values.** The copy cost is linear in payload size and is the price of
the isolation. The granularity guidance in §11 is normative for extension
authors: design chunky APIs (whole buffers, whole documents), never per-item
calls. A streaming/chunked convention is follow-up (§15), not v1.

## 4. GC interaction

There is none, by construction, and this section exists to keep it that way.

The VM-side copies are ordinary Go values owned by the Go GC, like every
value a builtin native produces today. Guest memory is a `[]byte` owned by
the wazero instance, reclaimed when the instance closes. No VM value is
referenced by the guest after `nx_call` returns; no guest pointer is
referenced by the host after the call returns. There is nothing to pin, no
retain/release protocol across the boundary, and no interaction with the
CoW `Owners` refcount beyond what `callNative` already does for any native's
arguments.

Any future handle-table extension (§15) must re-open this section; until
then, "no VM value crosses by reference" is an invariant of the ABI.

## 5. Concurrency

A wazero module instance is not safe for concurrent invocation — it has one
linear memory and (typically) one stack. Two execution modes, declared in the
manifest:

- `concurrency = "stateless"` — the extension mints no cross-call state (no
  handle-returning exports; the loader verifies none are declared). The host
  keeps a pool of instances per module (grown on demand, bounded by
  host config, default `GOMAXPROCS`); any noxy routine grabs any instance.
  Pure transformers scale with the machine.
- `concurrency = "single"` (default) — one instance, one mutex, calls
  serialize. Correct for stateful extensions because a guest-minted handle is
  only meaningful in the instance that minted it; pooling would make
  `Compressor(3)` from instance A dangle in instance B. Contention is real
  but honest — a stateful extension that becomes a bottleneck is the signal
  to redesign its API or move it to tier B (a process, which multiplexes).

In both modes, host imports that touch `SharedState` (in v1: only `nx_log`,
and capability imports later) take the same locks builtins take. Noxy
routines otherwise interact with extensions exactly as they interact with any
native: through `callValue` on their own VM, against the shared root
environment.

**Runaway guests.** Instantiation uses
`wazero.WithCloseOnContextDone(true)`, and each `nx_call` runs under a
host-side timeout (default 30 s, host-configurable): expiry cancels the
guest via the context, surfaces as a trap, and poisons the instance — an
infinite loop in a guest costs one bounded call, not the process. A
manifest-declared soft deadline (per-extension tuning) remains follow-up.

**Memory bound.** Each instance's linear memory is capped
(`WithMemoryLimitPages`; host default 64 MB, manifest may request up to a
host-configured ceiling via `memory_max_mb`). A guest that exhausts its cap
traps, and the trap is an error (§6) — not a VM OOM.

## 6. Errors and traps

The two-channel philosophy (raise for bugs, results for data) applies to
extensions unchanged; the boundary only defines how the raise channel is fed.

- **Declared failure.** The guest calls `nx_fail(msg)` and returns 0 from
  `nx_call`. The host raises a runtime error:
  `extension 'zstd' failed: <msg>` — a `RuntimeError` with the Noxy stack
  captured at the call site, `kind = "runtime"` when observed through
  `call_result` or a task boundary. The message is the capturable text;
  advisory hints, if any, follow the `AdvisedError` convention.
- **Trap.** Out-of-bounds access, `unreachable`, stack or memory exhaustion
  inside the guest surface as a wazero error with a wasm stack trace. The
  host wraps it: `extension 'zstd' trapped: <wazero error>` — also a runtime
  error, also capturable. A trap additionally poisons the instance: it is
  closed and (stateless mode) replaced, or (single mode) the module is marked
  failed and subsequent calls error with `extension 'zstd' is poisoned by an
  earlier trap` until process end. State behind a poisoned instance is gone;
  this is the honest consequence of guest-side state, and it is still a
  contained, observable failure — invariant 5 asks for no silent corruption,
  not for immortality.
- **Protocol violation.** Malformed NXB from the guest, a result violating
  the manifest's declared return type, `nx_call` returning a region outside
  guest memory: runtime error naming the extension and the violation. The
  manifest's `ReturnType` is enforced by the host on decode, so a lying
  extension is caught at the boundary, not downstream.
- **`_result` twins.** Extension wrappers build them in pure Noxy over
  `call_result`, which is precisely what `call_result` was designed for. The
  boundary itself never returns envelopes.

## 7. Static typing integration

No compiler changes. The manifest declares each export's Noxy signature:

```toml
name = "zstd"
abi = 1
min_noxy = "0.18.0"
concurrency = "stateless"
capabilities = []

[[export]]
name = "zstd_compress"
params = ["bytes", "int"]
returns = "bytes"
```

Accepted type names in v1: `int`, `float`, `bool`, `string`, `bytes`, `any`,
`T[]`, `map[K]V` with string/int keys, and struct names declared in the
wrapper `.nx`. The loader translates these into the `NativeSignature` /
`RuntimeTypeInfo` vocabulary the VM already validates
(`internal/vm/call_validation.go`, `runtime_type_validation.go`) — extension
natives are therefore *signed* natives from day one, which the `call_result`
spec already identified as the desirable end state for builtins.

Compile-time typing comes from the wrapper `.nx`, discovered by
`module_exports.go` exactly as for stdlib modules: the user-facing API is
`zstd.compress(data, 3)` with full static checking; the raw `zstd_compress`
native remains the untyped global floor, same as `sqlite_open` today.

The manifest and the wrapper can disagree (wrapper declares `-> Compressor`,
manifest declares `returns = "int"`); that is by design — the wrapper is the
typed façade, the manifest is the wire truth, and the runtime type validator
sits between them, as it does for every native-backed stdlib function today.

## 8. Packaging and the package manager

The author publishes the package as a git repo (existing model): manifest,
`ext.wasm`, wrapper `.nx`, docs. One artifact per release regardless of
platform — the decisive packaging advantage of this approach.

`noxy --get` changes:

1. After clone/checkout, if `noxy_ext.toml` exists: validate it, and record
   the sha256 of `ext.wasm` and of the manifest in `noxy.sum` (new file,
   sibling of `noxy.mod`; format speced separately, but its existence is a
   **hard prerequisite** — this design does not ship before integrity
   verification does, because it moves executable code through the package
   manager).
2. Display the manifest's `capabilities` list and record the user's grant in
   `noxy.mod` (§9). Getting a capability-free extension stays prompt-free.

At load time the VM re-verifies `ext.wasm` against `noxy.sum` and refuses a
mismatch. `min_noxy` is checked against `internal/version.Version`.

## 9. Capabilities

The sandbox gives permissions for free: a guest can only do what its imports
let it do. v1 policy:

- The unconditional surface is `noxy:host/v1` (`nx_fail`, `nx_log`) — no
  clock, no random, no I/O. A capability-free extension is a pure function of
  its arguments, and that is verifiable from its import section alone.
- Capability modules are versioned import namespaces granted per extension:
  `noxy:cap/random/v1`, `noxy:cap/clock/v1`, `noxy:cap/fs/v1` (path-scoped),
  `noxy:cap/net/v1`. v1 of this design ships **only `random` and `clock`**
  (cheap, low-risk, needed by crypto and by anything time-stamped); fs and
  net are designed as names but not implemented until an extension needs
  them — each is its own review surface.
- The manifest declares needed capabilities; `noxy --get` shows them and
  records the grant as a `grant <pkg> <cap>...` line in `noxy.mod`;
  instantiation fails on any undeclared or ungranted import. Grants are
  per-project and visible in diff review — the same trust checkpoint as a
  dependency bump.

Tier B (process plugins) can make no such guarantee; its spec must say so.

> **Amended 2026-08-29 (issue #110).** Invariant 5 was dropped, so tier B
> no longer needs to make that guarantee — process isolation is a bonus, not
> a requirement. The `random`/`clock`/`fs`/`net` capability namespaces above
> are **suspended**: I/O is tier B's job (issue #80). A restricted WASI
> preview1 grant (clock, random, stdout→`nx_log`; no filesystem mount, no
> sockets) is the candidate answer to authoring friction, in its own issue
> if pursued.

## 10. ABI evolution

- `noxy:host/v1` is additive-only: new imports may be added (instantiation
  only links what the guest imports), existing ones never change signature or
  semantics. Breaking changes mint `noxy:host/v2`; the host serves both for
  a deprecation window of at least two minor releases.
- NXB tags are append-only; existing tags are frozen.
- `nx_abi_version` returning an unknown version fails the load with a
  message naming both versions and `min_noxy`.
- The manifest schema is versioned by the `abi` field; unknown keys are
  errors (not ignored), so typos fail loudly at publish time, not silently at
  runtime.
- What this buys, concretely: `internal/value` can change its representation
  arbitrarily — NaN-boxing, arena allocation, new tags — without any
  published extension noticing, because the only shared vocabulary is NXB
  and two integer-typed export signatures.

## 11. Cost model and granularity guidance

Order-of-magnitude budget per call (amd64, compiling engine): wazero
host→guest invocation ~0.1–0.5 µs; NXB encode/decode + two copies, linear in
payload — sub-µs for scalar args, ~1 µs/KB class for buffers. Total: a
`zstd_compress(1 MB)` call pays low-single-digit ms of copy against tens of
ms of compression — noise. A per-element call in a loop pays ~1–2 µs against
~20 ns for a builtin — two orders of magnitude, unacceptable.

Normative guidance for extension authors (goes in the extension authoring
doc): the unit of a call is a buffer, a document, a batch — never an element.
The stdlib's own `_result`/batch API shapes are the template.

Acceptance benchmark for the implementation (gates merge):

- round-trip overhead (encode + call + decode) for `(bytes[1KB], int) ->
  bytes[1KB]` under 5 µs on the CI amd64 runner;
- a wasm sha256 of 1 MB within 3× of `crypto`'s native sha256;
- binary size delta from embedding wazero measured and recorded in the PR
  (expected ~4–6 MB over the current 14 MB; if it exceeds 8 MB, revisit with
  a build-tag escape hatch `noxy_noext` for size-constrained builds).

## 12. Alternatives considered

- **Out-of-process plugins as the only mechanism** (RFC approach B): survives
  as tier 2. Rejected as primary for the per-call cost (~50–500 µs — three
  orders above the boundary here) and the N-artifacts-per-release packaging.
  *Amended 2026-08-29 (issue #110): now the primary mechanism for I/O, OS
  access and drivers; wasm stays primary for pure computation. The
  N-artifacts cost is accepted — Go cross-compiles the whole matrix without
  cgo — and the per-call cost is irrelevant for I/O-bound APIs.*
- **In-process dynamic libraries via purego** (RFC approach C): best raw
  performance, violates invariant 5 by construction. Rejected; not even
  behind a flag until someone brings a use case tier B cannot serve.
  *Amended 2026-08-29 (issue #110): invariant 5 is gone; the rejection now
  rests on (a) Go `-buildmode=c-shared` requiring cgo and a C toolchain per
  target, which forfeits the cross-compile advantage; (b) a permanently
  stable C-API over `Value`; (c) the gain mattering only for hot per-element
  calls, which wasm serves. Reopens on a concrete case needing the whole OS
  and hot calls that cannot be batched.*
- **Host-side handle table with accessor imports** (the "rich ABI"): rejected
  for v1, §3. Re-openable with evidence: a concrete extension whose profiled
  copy cost dominates and whose API cannot be made chunky.
- **Extism**: the same architecture (wazero underneath, copy-based ABI) with
  an existing SDK ecosystem. Rejected as a dependency — its envelope is
  JSON/bytes-oriented and its host-function story would still need a Noxy
  value encoding, so it saves less than it costs in control; but its design
  validates this one, and its guest SDKs are the reference for ours.
- **WASI / component model**: the component model would eventually replace
  NXB with a standard IDL, but wazero's support is not there and the spec is
  still moving; WASI preview1 grants ambient capabilities (clock, random, fs)
  wholesale, which §9 exists to avoid. Revisit when wazero ships stable
  component-model support.
- **JSON over the boundary instead of NXB**: loses `bytes` (base64 tax on
  exactly the payloads that matter), loses int64 fidelity, and the VM
  already owns fast paths for neither direction. NXB is ~200 lines each side.

## 13. Risks and the two-year failure mode

Named in the RFC, owned here:

- **Authoring friction is the existential risk.** If writing an extension
  requires fighting toolchains, nobody writes one and the mechanism is dead
  code. Mitigation is scoped into M1 (§14): ship two reference SDKs (Rust
  crate, TinyGo package) and one real published extension, and treat SDK
  ergonomics as part of this feature, not an afterthought.
- **Capability surface creep.** Extensions that need sockets/fs will lobby
  for `noxy:cap/net`, and each capability re-concentrates review burden.
  Mitigation: capabilities ship one at a time, each with its own spec, and
  tier B remains the documented answer for "I need the whole OS".
- **Guest-state loss on trap** (§6) will surprise someone. Mitigation: the
  authoring doc requires stateful extensions to document poisoning behavior,
  and the error message names the mechanism.
- **wazero as a load-bearing dependency.** It is CNCF-adjacent, widely used
  (Envoy/Istio ecosystem via proxy-wasm, Dapr, Trivy) and API-stable at
  v1.x. The exit path, should it falter: NXB and the ABI are runtime-neutral;
  any future pure-Go runtime can slot in behind the same loader.

## 14. Phasing

- **M1 — mechanism.** wazero embedding, loader + manifest, NXB, `nx_fail`/
  `nx_log`, stateless + single modes, poisoning, benchmarks of §11, `noxy
  --get` sha256 into `noxy.sum` (minimal form), Rust + TinyGo SDKs, one
  published reference extension (proposed: zstd — real demand, pure compute,
  exercises bytes-heavy calls). Capability set: empty.
- **M2 — capabilities.** `noxy:cap/random/v1` + `noxy:cap/clock/v1`, grant
  lines in `noxy.mod`, `--get` prompt. Reference extension exercising them
  (proposed: an argon2/password-hashing lib).
  *Suspended 2026-08-29 (issue #110): I/O and OS access are tier B's job;
  see `2026-08-29-extensibility-invariants-revision.md`.*
- **M3 — core diet, decision only.** Measure binary share of
  `modernc.org/sqlite` (+`libc`) with `go tool nm` size accounting; if it is
  the expected majority share, spec sqlite-as-extension (needs
  `noxy:cap/fs`) as its own RFC. The core criterion from issue #78 stands:
  core keeps what language semantics need and what the toolchain needs to
  bootstrap (net client, TLS, sha256, fs).

## 15. Open questions and follow-ups

- **`noxy.sum` format spec** — prerequisite, separate spec; must cover both
  source packages and binary artifacts, and `--get`'s trust-on-first-use
  story.
- **Guest→host callbacks.** Some real APIs (visitors, streaming parsers)
  want to call Noxy code per item. The task/`call_result` failure vocabulary
  would carry errors; the cost model says it wants batching anyway. Needs its
  own design; explicitly not v1.
- **Streaming/chunked convention** for payloads that exceed the size cap or
  want pipelining — a `nx_call`-level convention, additive to the ABI.
- **Soft deadlines per call** (manifest-declared), riding the existing
  context plumbing.
- **Instance prewarming** — M1 enables wazero's persistent compilation
  cache (user cache dir) so repeat runs skip recompilation, and measures
  cold vs. warm load; remaining instantiation cost at first `use` of a
  large module is still worth measuring per-extension.
- **Tier B formalization** — versioned handshake for `sys_load_plugin`,
  msgpack framing, platform-artifact selection in the package manager; its
  own spec, sharing the manifest and `noxy.sum` machinery where possible.
  *Now issue #80 (spec) with the deltas of issue #110: NXB framing (not
  msgpack), binaries as release assets per platform, portable `noxy.sum`,
  `noxy_terminal` and `noxy_dynamodb` as the reference extensions.*
- **REPL story** — loading extensions from the REPL session works via the
  same module path; poisoning semantics in a long-lived REPL deserve a test.
