# Extensibility Invariants — Revision (2026-08-29)

Amends `2026-08-23-wasm-extension-mechanism-design.md`. Decision record:
issue #110. Related: RFC #78 (closed), issue #80 (tier B spec), PR #79
(wasm M1, v0.18.0).

This document is the authoritative statement of the invariants that any
extensibility mechanism for Noxy must satisfy. Where the 2026-08-23 spec
cites "invariant N", read the revised table below; the spec keeps its
original text with amendment notes at the points it supersedes.

## Why a revision

RFC #78 listed five "non-negotiable" invariants. Reviewing the record on
2026-08-28 showed that they were written together with the answer, not
before it: the RFC was opened on 2026-08-23 at 21:43Z, the spec that answers
it was committed 47 minutes later (`5435900`), and PR #79 with the whole M1
implementation was merged about five hours after the RFC. No commit before
2026-08-23 mentions a single binary or cross-compilation; the first
`GOOS=darwin` in the repository is the spec's own commit. The spec's claim
that "CI enforces `CGO_ENABLED=0`" corresponds to an environment variable in
one workflow (`network-deadlines.yml`, since 2026-08-14) and to the Lambda
build scripts from January.

Invariants 1 and 2 have real roots — a static Linux binary built from a
Windows machine for the Lambda runtime since January 2026. Invariants 3, 4
and 5 were declared on the spot, and invariant 5 is the one that did the
decisive work against in-process dynamic libraries.

The RFC's own problem statement has two halves — "performance **or talking
to the operating system** … no path to bindings for … **database
drivers**" — and the wasm mechanism serves only the first. The two native
extensions that existed before the RFC, `estevaofon/noxy_dynamodb`
(2026-01) and `estevaofon/noxy_terminal` (2026-08), are both I/O; neither is
expressible as a wasm extension. The only wasm extension, `noxy_regex`, was
created the day after the RFC to validate the mechanism.

None of this invalidates the wasm design for what it does. It obliges the
project to record the invariants the author actually holds and to
re-evaluate the alternatives against them.

## The invariants (authoritative)

Stated by the author on 2026-08-28, in the author's words:

> R1. Não precisa ser binário único, mas é preferível.
> R2. Eu gostaria que seja cross compile e sem necessidade do usuário
> compilar, assim como o Python.
> R3. Concordo no sentido de ser cross-plataforma.
> R4. Concordo, sem compilação pelo usuário.
> R5. Não é necessário, muito restritivo.

| # | RFC #78 (2026-08-23) | Revised (2026-08-28) |
|---|---|---|
| 1 | Distribution as a single binary | **Preferred, not required.** |
| 2 | Cross-compilation without an external toolchain | **Kept**, and extended: whoever installs an extension never compiles — the Python model. |
| 3 | Platform parity (Linux, macOS, Windows) | **Kept** (cross-platform). |
| 4 | The end user does not compile | **Kept**, strongly. |
| 5 | The VM cannot be destabilized | **Dropped.** Too restrictive. An extension may crash the VM, as a C extension may crash Python. |

The decisive filter is 2 + 4: an author must be able to produce artifacts
for every supported platform cheaply, and a user must never need a
toolchain. Invariant 5 no longer constrains the choice of mechanism;
isolation, where a mechanism provides it, is a bonus.

## Decision

### 1. Process plugins (tier B) are the primary path for I/O

For I/O, OS access, drivers and SDK bindings — the second half of the RFC's
problem — the out-of-process plugin is the primary mechanism, not a "second
tier" and not a consciously accepted cost. It satisfies invariants 2, 3 and 4
at the lowest cost to authors because of a structural advantage Python never
had: Go cross-compiles to every target from any machine, without cgo.

```sh
for os in linux darwin windows; do for arch in amd64 arm64; do
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -o dist/noxy-plugin-pg-$os-$arch .
done; done
```

Six binaries, one command, no C toolchain. The pure-Go driver ecosystem
(`pgx`, `go-sql-driver/mysql`, `mongo-go`, `go-redis`, the AWS SDK,
`modernc.org/sqlite`) becomes available with `go get`. This is exactly what
`noxy_dynamodb` and `noxy_terminal` needed.

### 2. wasm (tier A) stays, for pure computation

The wasm mechanism is shipped, costs about 4 MiB and serves its niche:
compression, regex, hashing, parsers — buffer in, buffer out. What changes:
the bespoke capability plan (`noxy:cap/random`, `noxy:cap/clock`,
`noxy:cap/net`, `noxy:cap/fs`; spec §9, phase M2 in §14) is **suspended**.
I/O goes to tier B. A restricted WASI preview1 grant (clock, random,
stdout→`nx_log`; no filesystem mount, no sockets) is the candidate answer
to authoring friction — today standard Go cannot produce a production
extension, because it only targets `wasip1` and the loader refuses WASI
(`internal/ext/exttest/exttest.go`). That is its own issue if pursued.

### 3. In-process dynamic libraries (approach C) stay deferred — for the right reasons

Invariant 5 is gone, so the original rejection no longer applies. Approach C
stays out for three other reasons:

1. **It forfeits the advantage.** A Go plugin becomes a `.so`/`.dll` only
   through `-buildmode=c-shared`, which requires cgo, which cross-compiled
   requires a C toolchain per target (mingw for Windows, the Apple SDK for
   macOS). The author is back to the expensive CI matrix that pure-Go tier B
   avoids.
2. **It demands a permanently stable C-API over `Value`.** The layout cannot
   leak, so the surface would be a handle table with accessor functions,
   frozen forever because every published `.so` depends on it. NXB is a wire
   format; `Value` stays free to evolve.
3. **Its gain only appears where wasm already is.** 50 ns versus ~20 µs
   matters for hot per-element calls — and hot computation goes to wasm.
   For I/O, 20 µs is noise.

Reopen on a concrete library that needs the whole OS **and** hot calls that
cannot be batched. Even then, the first answer may be pointwise (that library
enters the core in Go) rather than a new mechanism.

## Mechanisms against the revised invariants

| | wasm (A) | process (B) | in-process dylib (C) |
|---|---|---|---|
| Cross-platform | one artifact serves all | one binary per OS/arch | one `.so/.dylib/.dll` per OS/arch |
| Author cross-compiles painlessly | yes | **yes, in Go** (no cgo) | no: Go `c-shared` needs cgo + a C toolchain per target; Rust/C need the Apple SDK for macOS |
| User never compiles | yes | yes (prebuilt assets) | yes, but costlier for the author to deliver |
| OS / network / drivers | no (until capabilities) | **full** | full |
| Per-call cost | ≈4 µs | ≈10–50 µs with NXB framing (50–500 µs with today's JSON) | ≈50 ns |
| Extension crashes | trap; instance poisoned | process dies; VM survives | VM dies |
| Coupling to `Value` | none (NXB) | none (NXB) | stable C-API of handles/accessors, forever |

Choose by workload: pure computation → wasm; I/O, OS, drivers, SDKs →
process plugin; whole OS plus a hot loop → no mechanism for now (core in Go,
or approach C when the case appears).

## "The user never compiles", end to end

1. **Author**: `main.go` (uses the plugin SDK; imports anything pure-Go),
   `noxy_ext.toml` with `kind = "process"` and typed `[[export]]` entries,
   and the `.nx` wrapper. Runs the build matrix above; publishes a tag and a
   release with the binaries and a `checksums.txt`.
2. **User**: `noxy --get github.com/you/noxy_pg@v1.2.0`. The package manager
   clones the repo (manifest + wrapper, as today), reads
   `kind = "process"`, detects `GOOS/GOARCH`, downloads only the matching
   asset (`noxy-plugin-<name>-<os>-<arch>[.exe]`) into
   `noxy_libs/<domain>/<user>/<repo>/bin/`, and records the hashes of
   **all** published assets in `noxy.sum`, so the committed lockfile is
   valid for a teammate on macOS and for a Lambda on Linux. No build for the
   platform is an error at `--get` time, never at runtime; there is no
   compile-from-source fallback, by design (invariant 4).
3. **Runtime**: on first call the VM verifies the hash against `noxy.sum`,
   spawns the process, performs the versioned handshake, and exchanges NXB
   frames over stdin/stdout.

An author writing in Rust, C or anything else is allowed — the mechanism only
requires an executable that speaks the protocol — but then the author carries
the per-platform CI matrix, as in Python. The user still never compiles.

## What this changes elsewhere

- **Wasm spec (2026-08-23)**: amendment notes in §1 (approach C), §9
  (capabilities suspended; tier B no longer bound to invariant 5), §12
  (approaches B and C), §14 (M2 suspended), §15 (tier B follow-up now
  #80 + #110).
- **Issue #80 (tier B spec)** keeps its technical design — one boundary, two
  transports; NXB over the pipe; `kind = "process"` in the manifest; a Go
  SDK. Issue #110 lists the deltas: primary-for-I/O framing; invariant 5
  as bonus; binaries as release assets, not committed; portable `noxy.sum`;
  reference extensions are `noxy_terminal` and `noxy_dynamodb`; migration of
  `sys_load_plugin` removes the compiler's `PluginNativeNames` special case;
  errors surface as Noxy errors with a per-call timeout; the SDK is a
  requirement of the first delivery.
- **User docs** (`docs/EXTENSIONS.md`, `AGENTS.md`): scope note — wasm for
  computation, process plugins for I/O.

## Related, out of scope here

- **TLS is absent from the core.** `http_client.nx` uses `"https"` only to
  choose port 443; there is no `crypto/tls` anywhere. The wasm spec §14
  itself says TLS belongs in the core. Its own issue, with priority above
  any wasm M2 work.
- Restricted WASI for wasm authoring; OS-level process sandboxing; a package
  registry; any change to the wasm ABI v1.
