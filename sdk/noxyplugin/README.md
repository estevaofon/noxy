# noxyplugin

`noxyplugin` is the Go SDK for writing Noxy process extensions: executables
that speak `noxy-plugin/1` over their stdin/stdout. It is a nested Go module
(`github.com/estevaofon/noxy/sdk/noxyplugin`) with no dependency on
`noxy-vm` — it carries its own NXB codec over Go types, because the wire
format is the contract, not a shared package. Protocol design:
`docs/superpowers/specs/2026-08-29-process-extensions-design.md` in the
main repository; user-facing docs: `docs/EXTENSIONS.md`.

## Writing one in Go

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
`bool`, `string`, `[]byte`, slices, maps, `noxyplugin.Struct`); an untyped
`Handler` gets `Args` with accessors. Handler errors become `failed`;
panics are recovered into `failed: panic: ...`; the handler's
`context.Context` is cancelled on CANCEL and at shutdown. `Main` protects
stdout (a stray `fmt.Println` goes to stderr) and refuses to run in a
terminal.

## Type mapping

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
struct-shaped map either way.

## Behaviour the SDK guarantees

- Reads frames from stdin, one goroutine per CALL (the host enforces
  `single`), replies under a write mutex.
- Cancels the handler's `context.Context` on CANCEL and on stdin EOF;
  replies to cancelled calls with whatever the handler returns.
- Recovers handler panics into ERROR `panic: <value>`.
- **Protects stdout**: at start it duplicates fd 1 for the protocol and
  points `os.Stdout` at stderr, so a stray print cannot corrupt the
  stream.
- Exits 0 on stdin EOF after a bounded wait for handlers; exits 2 on a
  malformed frame or a write error (EPIPE means the host is gone).
- Run by hand (stdin is a terminal), prints `this program is a Noxy
  extension; install it with 'noxy --get'` and exits 2.
- Ignores SIGINT: the host owns Ctrl-C; the child leaves on EOF.

## Releasing an extension

Build the platform matrix and checksums with `release/build.sh` (run from
the extension's module root):

```sh
# sdk/noxyplugin/release/build.sh — run from the extension's module root
#!/usr/bin/env sh
set -eu
NAME="${1:?usage: build.sh <extension-name>}"
mkdir -p dist
for os in linux darwin windows; do for arch in amd64 arm64; do
  ext=""; [ "$os" = windows ] && ext=".exe"
  GOOS=$os GOARCH=$arch CGO_ENABLED=0 go build -trimpath -ldflags=-s \
    -o "dist/noxy-plugin-$NAME-$os-$arch$ext" .
done; done
(cd dist && sha256sum -- * > checksums.txt)
echo "dist/ is ready: gh release create <tag> dist/*"
```

`release/github-release.yml` automates the same thing on a tag push: copy
it to `.github/workflows/` in your extension repository and replace
`<name>` with your extension's name. It checks out the repo, sets up Go
from `go.mod`, runs `sh release/build.sh <name>`, and publishes `dist/*`
(including `checksums.txt`) as release assets — exactly the layout
`noxy --get` expects: `[binaries]` in `noxy_ext.toml` names these same
assets, and `checksums.txt` lets `noxy --get` record the hash of every
platform's binary in the consumer's `noxy.sum`, not just the one it
downloaded.

## Testing

```sh
go test ./...
```

This module's tests (frame codec, golden NXB vectors, a fake host over
`io.Pipe`) run independently of the root `noxy-vm` module's `go test
./...` — CI runs both (see `.github/workflows/network-deadlines.yml`,
job `network-semantics`).
