# Noxy Package Manager 📦

Noxy includes a built-in package manager to manage dependencies and share
code. It follows a decentralized approach similar to Go, Cargo and uv:
packages are Git repositories, `noxy.mod` records the direct dependencies
you asked for, and `noxy.sum` is a lockfile that pins the exact transitive
closure so a project built from source is reproducible on any machine.

A project cloned with `noxy.mod` and `noxy.sum` becomes runnable with one
command:

```bash
git clone <project> && cd <project>
noxy --sync
noxy main.nx
```

`noxy_libs/` is not committed — it is entirely derived from `noxy.mod` and
`noxy.sum`. See [`noxy_libs`](#noxy_libs) below.

## Commands

### `noxy --get pkg[@version]`

Add a new dependency or upgrade an existing one, then sync.

```bash
noxy --get github.com/user/repo
noxy --get github.com/user/repo@v1.2.0
```

- Resolves the version: a tag is used as given (normalized with a leading
  `v`); no version resolves the newest semver tag (`git ls-remote --tags`);
  a commit SHA or branch name resolves to a
  [pseudo-version](#versions).
- Writes (or replaces) the `require` line for that module in the root
  `noxy.mod`.
- Runs the same sync as `noxy --sync` (below) to install the full closure.
- Does **not** touch the lock entry of a version it already has: if the
  resolved version matches what `noxy.sum` already pins, the existing hash
  still applies and a download that no longer matches it (e.g. a moved tag)
  is a fatal error, same as always.

`--get` with no version on an already-pinned package **upgrades** it to the
newest tag — it is `go get pkg@latest`. To just install what `noxy.mod`
already asks for, use `--sync`.

Example output:

```
Resolved 2 packages
github.com/estevaofon/quicksort v0.1.0        cached
github.com/estevaofon/noxy_dynamodb v0.3.0    installed (bin/noxy-plugin-dynamodb-linux-amd64)
Done.
```

To remove a dependency: delete its `require` line from `noxy.mod` and run
`--sync` — pruning does the rest.

### `noxy --sync`

Rebuilds `noxy_libs/` from `noxy.mod` and `noxy.sum` alone. This is what a
fresh clone runs, and what CI runs before executing any Noxy program.

1. Reads `noxy.mod` and `noxy.sum`, computes the transitive closure by
   [MVS](#versions) (re-reading every dependency's own `noxy.mod`).
2. For each package in the closure, in order: if it is already on disk,
   the stamp says it is at that version, and its tree hash matches the
   lock, it is `cached` — nothing is downloaded or touched. Otherwise it
   is cloned (or a process extension's release assets are downloaded),
   hashed, compared against the lock if the lock had an entry (a mismatch
   is a fatal error — the download does not match what was recorded), and
   installed.
3. Prunes anything the sync itself previously installed that is no longer
   in the closure (see [`noxy_libs`](#noxy_libs)), rewrites the stamp, and
   rewrites `noxy.sum` and, if a `HEAD` line got pinned, `noxy.mod`.
4. Prints one line per package, then `Done.`.

A `--sync` on an already-synced project touches no network and rewrites no
file whose content did not change.

```
Resolved 2 packages
github.com/estevaofon/noxy_dynamodb v0.3.0  cached
github.com/estevaofon/quicksort v0.1.0      cached
Done.
```

### `noxy --sync --locked`

Same as `--sync`, but refuses to write anything: `noxy.mod` pinning a
package to `HEAD`, or a computed closure that differs from `noxy.sum`
(module added, removed, at a different version, or missing a tree hash) is
a fatal error telling you to run `--sync` without `--locked` first. A
package that is in the lock but missing from disk is still installed
normally — `--locked` freezes the files, not the disk.

This is the command CI runs: it fails fast if someone forgot to commit an
updated `noxy.sum` after touching `noxy.mod`.

## `noxy.mod`

`noxy.mod` is **intent**: it lists only your project's direct
dependencies, the way you asked for them. It is not the lock — `noxy.sum`
is.

```text
module my_project

noxy v0.24.0

require github.com/estevaofon/noxy_dynamodb v0.3.0
require github.com/estevaofon/quicksort v0.1.0
```

- `module`: your project's module name.
- `noxy <version>`: the minimum Noxy version this project requires.
  `--sync` refuses to run with an older binary (`noxy.mod requires noxy
  vX.Y.Z; this is vA.B.C`); `--sync` never writes this line itself, but
  `--get` keeps overwriting it with the version of the binary you ran.
- `require <module> <version>`: a direct dependency. `<version>` is a
  semver tag, a pseudo-version, or `HEAD`. `HEAD` means "resolve on the
  next sync and rewrite this line with the result" — under `--locked` it
  is an error instead. Module paths are bare `host/user/repo`, no scheme:
  `https://github.com/x/y` or `git@github.com:x/y` are rejected.

`Save` always writes `require` lines sorted by module path, so diffs are
stable across machines.

## `noxy.sum`

The lock. One line per fact, sorted, terminated with `\n`:

```
<module> <version> sha256:<hex>                 # source tree hash
<module> <version> <file> sha256:<hex>          # extension artifact (manifest, .wasm, bin/<asset>)
```

Example, for this repository's `noxy.mod`:

```
github.com/estevaofon/noxy_dynamodb v0.3.0 bin/noxy-plugin-dynamodb-darwin-amd64 sha256:…
github.com/estevaofon/noxy_dynamodb v0.3.0 bin/noxy-plugin-dynamodb-linux-amd64 sha256:…
github.com/estevaofon/noxy_dynamodb v0.3.0 noxy_ext.toml sha256:…
github.com/estevaofon/noxy_dynamodb v0.3.0 sha256:…
github.com/estevaofon/quicksort v0.1.0 sha256:…
```

- **The key is the module path** from `noxy.mod` (`github.com/...`), not
  the local directory path (`github_com/...`).
- **One version per module.** The lock lists the module's full transitive
  closure — direct and indirect, unmarked — and MVS always produces
  exactly one version per module; `noxy.sum` refuses to record two.
- The tree hash line covers every regular file under the package
  directory (Go's `dirhash.Hash1` algorithm) and **excludes** `bin/` (the
  per-platform extension binary, already covered by its own artifact
  line) and any nested `noxy_libs/`. Because `bin/` is excluded, the same
  tree-hash line verifies the source on every platform.
- Artifact lines for a process extension cover **every** published
  platform (hashes come from the release's `checksums.txt`), so one
  committed `noxy.sum` verifies a teammate's macOS download and a
  Lambda's Linux download alike.

## `noxy_libs`

`noxy_libs/` is entirely **derived** — do not commit it. `noxy --sync`
reconstructs it from `noxy.mod` + `noxy.sum`; that is why the repository's
`.gitignore` has:

```
noxy_libs/*
!noxy_libs/math_lib/
```

(A hand-placed fixture library can still be committed by exempting its own
directory, like `math_lib` is here, until `replace <pkg> => ./path` exists
for local development — see the roadmap in the design spec.)

Every package the sync itself installs is recorded in
`noxy_libs/.noxy-sync`, one `<module> <version>` line per package. Pruning
uses this stamp: a directory the stamp lists but the new closure no longer
needs is removed (`os.RemoveAll`, with empty parent directories cleaned up
too); a directory outside the stamp is **never** touched, no matter what
`noxy.mod` says. This is why a hand-placed library like `math_lib`
survives every sync even though it lives under `noxy_libs`.

## Project root

`noxy.mod` is found by walking up from a starting directory to the nearest
ancestor that has one — this is the **only** definition of "project root",
used both by the CLI and by the VM:

- `--get`/`--sync` start from the current working directory. With no
  `noxy.mod` above it, `--sync` is an error (`no noxy.mod in <cwd> or any
  parent`); `--get` creates one in the cwd, as before.
- The VM starts from the script's directory. This means a script in a
  subdirectory (e.g. `noxy_examples/dynamodb_example.nx` run from the
  repository root) resolves `use` statements and verifies extension
  hashes against the `noxy.mod`/`noxy.sum` at the repository root, not
  against a `noxy_libs` next to the script.

A `module not found` error mentions the fix when it applies: if the
missing module matches a `require` in the project's `noxy.mod`, the error
is `module not found: <module> (required by noxy.mod) — run 'noxy
--sync'`.

## Versions

- **Newest tag.** `pkg` or `pkg@HEAD` at the project root resolves to the
  newest semver tag (`git ls-remote --tags`); with no semver tag, it falls
  back to a pseudo-version for the commit `HEAD` points at.
- **Pseudo-version.** For a commit with no tag (or requested by SHA or
  branch name), the version is `v0.0.0-<yyyymmddhhmmss>-<sha12>`; if a
  semver tag `vX.Y.Z` is an ancestor of the commit, it is instead
  `vX.Y.(Z+1)-0.<timestamp>-<sha12>` (Go's scheme), so a pseudo-version
  built on top of a tag still outranks that tag under MVS.
- **`HEAD` inside a dependency's own `noxy.mod`.** A dependency's
  `noxy.mod` is part of its tree hash and is never rewritten. If it
  `require`s something at `HEAD`, the sync uses the version already in
  the lock for that module instead of going to the network — this is what
  keeps an already-synced project's `--sync` offline. Outside the lock it
  resolves like at the root; under `--locked` it is an error.
- **MVS (minimal version selection).** When two dependencies require
  different versions of the same module, the sync always chooses the
  **greater** one, the same rule Go's module resolver uses. The chosen
  version is printed when it differs from what the root's `noxy.mod`
  asked for directly; the root's `require` line itself is never rewritten
  because of an indirect requirement.

## Migrating from v1

`noxy.sum` v1 (three fields, keyed by local path — `github_com/user/repo
<file> sha256:<hex>`, extension artifacts only) is read for one version:
the parser still accepts v1 lines, but treats them as "no tree hash
recorded" for that module. The next `--sync` reinstalls the module
(nothing to compare a hash against yet, so it is never reported as
`cached`) and rewrites all of its lines in v2 form. A version after that,
the parser rejects v1 lines outright.

If your project has `noxy_libs/` committed to git, migrate once:

```bash
noxy --sync
git rm -r --cached noxy_libs
```

then add `noxy_libs/*` (with any hand-placed fixture directory excepted,
as above) to `.gitignore` and commit the regenerated `noxy.sum`.
