# Standalone executables (`noxy build`)

`noxy build` turns a Noxy program into a single executable that runs on a
machine without `noxy`, `noxy_libs` or the source tree. Design:
`docs/superpowers/specs/2026-09-26-noxy-build-design.md`.

```bash
noxy build editor.nx -o dist/noxy-editor
dist/noxy-editor ../some-folder          # exactly like `noxy editor.nx ../some-folder`
```

## What goes in

The executable is the `noxy` that ran the build, followed by a zip payload
and a 64-byte trailer (`NOXYAPP1`). The payload holds:

- the entry file and every module reachable through `use`, transitively,
  including packages under `noxy_libs/` (the embedded stdlib is already in
  the runtime and is not copied; a local `.nx` that shadows a stdlib module
  is);
- for each extension package, `noxy_ext.toml` and **this platform's**
  binary from `bin/` (or the `.wasm`);
- `noxy.mod` and `noxy.sum`, when the project has them;
- the assets you declare (below).

**The source is readable by anyone with `unzip`** — `unzip -l dist/noxy-editor`
lists it, `unzip` extracts it. `noxy build` is a packaging tool, not an
obfuscator, and the bytecode payload planned for a later version will not
protect code either (bytecode disassembles with `--disassembly`).

The project root is the directory of the nearest `noxy.mod` above the entry
file, or the entry's directory when there is none. Every module must live
under it: a module found through `NOXY_PATH` outside the root is a build
error. The build resolves `use` exactly like `noxy entry.nx`, and then checks
that the executable will find each module the same way: a built executable
only searches `noxy_libs/` (of the project and of the entry's directory) and
paths relative to the entry. A module that `noxy entry.nx` only finds through
the current directory or `NOXY_PATH` — even inside the project — is a build
error (`module <name> resolves through the current directory or NOXY_PATH`);
move it under `noxy_libs/` or next to the entry.

## Assets

Files your program reads at runtime are declared either in `noxy.mod`:

```text
include web
```

or on the command line, `--include web --include docs/help.txt` (repeatable).
Paths are relative to the project root, use `/`, and cannot escape it. Both
sources are merged. A directory is embedded recursively. An include that is
a symlink is followed and embedded under its own name; inside a directory,
symlinks to files are embedded (their content), symlinks to directories are
not followed. An include that yields no files is a build error.

## `--list`

`noxy build --list editor.nx` runs the whole plan — module graph, compile
check, platform binaries, includes — and prints what would be embedded
without writing anything. Use it to spot a missing asset or plugin binary.

## Compile errors come out of the build

Every module is compiled during the build (nothing is executed), so a type
error in `src/x.nx` fails `noxy build` with `src/x.nx: [line N] ...` instead
of failing the executable on the user's machine.

## How the executable runs

1. On start it reads its own trailer. Without one it is a plain `noxy`.
2. The payload is extracted **once** into the user cache, in a directory
   named after the first 16 hex characters of the payload's sha256:
   `~/.cache/noxy/apps/<hash>/` on Linux,
   `%LocalAppData%\noxy\apps\<hash>\` on Windows,
   `~/Library/Caches/noxy/apps/<hash>/` on macOS. The hash is verified at
   extraction; later starts only check the completion marker
   `.noxy-app-ok`. Extraction is atomic (temporary directory + rename), so
   two instances starting at once agree on one directory. A directory
   without the marker is a leftover of an interrupted extraction and is
   replaced.
3. The program runs with `argv = [<the executable>, <cache>/<entry>, args...]`
   and the **current directory unchanged** — so `dirname(argv[1]) + "/web"`
   finds the embedded `web/`, and `noxy-editor .` still opens the folder
   you are in. Module resolution is sealed to the extracted tree:
   `NOXY_PATH` and the current directory are ignored, and the app never
   looks above its own directory (a `noxy.mod` or `noxy_libs/` above the
   cache is not its project).
4. Extension binaries run from the extracted `noxy_libs/.../bin/`, verified
   against the extracted `noxy.sum` like in a synced project.

A new build has a new hash and a new directory; old ones are never removed.
Deleting the `apps` directory is always safe.

| Variable | Effect |
|---|---|
| `NOXY_INTERPRETER=1` | The executable ignores its payload and behaves as the plain `noxy` (REPL, `--sync`, `noxy file.nx`, even `noxy build`). **Inherited** by child processes. |
| `NOXY_APP_CACHE=<dir>` | Use `<dir>` instead of `<user cache>/noxy/apps`. Point it at a directory only you can write: the app directory name is predictable, and another local user who can write there could plant one with the marker and have your executable run their code. |
| `NOXY_PATH` | Ignored by a built executable. |

## Running other Noxy files from a built program

A built program has no `noxy` on the machine, but it *is* one. To run
another file with the same interpreter, spawn `sys.executable()` with
`NOXY_INTERPRETER=1` **in the child's environment only**:

```noxy
use sys
// sh:  NOXY_INTERPRETER=1 exec '<exe>' 'file.nx'
// cmd: set NOXY_INTERPRETER=1&& "<exe>" "file.nx"
let exe: string = sys.executable()
```

Do not set the variable in the program's own environment: it is inherited,
and a program started that way which calls `sys.executable()` and runs it
gets the interpreter, not the app. That is what the Noxy Editor's F5 does.

## Platforms

- **Linux and Windows** are the supported targets. The build runs on the
  target platform (no cross-compiling yet: it needs published `noxy`
  binaries, see below). On Windows the output gets `.exe`; extracted
  plugins are `.exe` files under the cache, which antivirus software may
  inspect on first run, as with `noxy.exe` itself.
- **macOS is experimental.** Go signs darwin binaries ad hoc; the appended
  payload sits outside the signed region, so local execution is expected to
  work, but `codesign --verify --strict`, Gatekeeper and notarization reject
  the file. The build prints a warning on macOS. If Apple Silicon kills the
  process, the plan B is to carry the payload in a Mach-O segment (what Node
  SEA/`postject` and Deno's `sui` do) and re-sign ad hoc.

Size: the runtime (~24 MB unstripped on Linux) plus the deflated payload —
the Noxy Editor comes out around 26 MB.

## Not yet

- **Cross-compiling** (`--target windows/amd64` from Linux): arrives when
  the noxy release publishes `noxy-<goos>-<goarch>` binaries with
  `checksums.txt`; the plugin binaries of other platforms are already
  hash-pinned in `noxy.sum`.
- **Bytecode payloads** and reading modules straight from the zip.
- `sys_load_plugin` (deprecated, removed in v0.27.0) is refused by the build.
- Cleaning old cache directories.
