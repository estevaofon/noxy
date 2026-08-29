# Noxy Package Manager 📦

Noxy includes a built-in package manager to easily manage dependencies and share code. It follows a decentralized approach similar to Go, allowing you to import packages directly from Git repositories (like GitHub).

## Commands

### Get a Package
To download and install a package, use the `--get` flag with the Noxy CLI:

```bash
noxy --get github.com/username/repository
# or with a specific version/tag/branch
noxy --get github.com/username/repository@v1.0.0
```

This command will:
1.  Clone the repository into `noxy_libs/`.
2.  Checkout the specified version (or HEAD).
3.  Update your `noxy.mod` file.
4.  Remove the `.git` directory from the downloaded package to avoid nested repositories.

`--get` always installs a fresh copy: the package directory under
`noxy_libs/` is replaced on every run (there is no `.git` left to pull).
For a process extension (`kind = "process"` in `noxy_ext.toml`) the
version is a release tag — omitted, `--get` resolves the newest semver
tag and prints it — and `--get` also downloads `checksums.txt` plus the
binary for your OS/arch from
`https://<host>/<user>/<repo>/releases/download/<tag>/` into
`noxy_libs/<pkg>/bin/`, verifying its sha256. A release without a binary
for your platform is an error here, never at runtime.

## Configuration (`noxy.mod`)

The `noxy.mod` file tracks your project's module name and dependencies. It is automatically updated when you run `noxy --get`.

### Example `noxy.mod`

```text
module my_project

require github.com/estevaofon/noxy_dynamodb v1.0.0
require github.com/estevaofon/math_lib HEAD
```

-   **module**: Defines the name of your module.
-   **require**: Lists dependencies and their versions.

## Directory Structure

Packages are installed in the `noxy_libs` directory in your project root. The structure mirrors the repository URL to avoid conflicts.

Example structure:
```
my_project/
├── noxy.mod
├── main.nx
└── noxy_libs/
    └── github_com/
        └── estevaofon/
            └── noxy_dynamodb/
                └── ... (package source code)
```

## Using Packages

Once installed, import packages using the module path. For example, the external terminal package is imported as:

```noxy
use github_com.estevaofon.noxy_terminal.terminal as terminal
```

The current package copy is provisional inside `noxy_libs` and may move to its own repository later.

## Creating a Package

To create a shareable package:
1.  Create a standard Noxy project.
2.  Initialize a git repository.
3.  Push to a public host (e.g., GitHub).
4.  Users can now install it via `noxy --get`.

## Integrity (`noxy.sum`)

When a downloaded package contains an extension (`noxy_ext.toml`),
`noxy --get` records sha256 lines in `noxy.sum` next to your `noxy.mod`:
the manifest plus the `.wasm` artifact for wasm extensions, or the
manifest plus **every** published binary (`bin/<asset>`) for process
extensions — hashes of the assets your machine did not download come from
the release's `checksums.txt`, so one committed `noxy.sum` verifies a
teammate's macOS download and a Lambda's Linux download alike. At load
time the VM checks the manifest first and then the artifact it is about
to run. Verification only applies to
packages installed under `noxy_libs`; when the package has a matching
`noxy.sum` entry, the VM verifies the manifest hash first (an attacker who
could repoint the manifest at an unregistered `.wasm` file would otherwise
bypass the check by renaming the artifact) and then the wasm hash — either
mismatch refuses the load. A package with no matching entry, or an
extension loaded from outside `noxy_libs` (a development layout), runs
unverified: trust on first use. The full integrity design (including
hashing packages without extensions) is tracked separately.
