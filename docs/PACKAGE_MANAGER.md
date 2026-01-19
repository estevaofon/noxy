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

Once installed, you can import packages in your Noxy code using their path relative to `noxy_libs` or the project root conform configured.

*(Note: Specific import syntax depends on the current Noxy language specification for imports, typically finding files in `noxy_libs` automatically or via relative paths).*

## Creating a Package

To create a shareable package:
1.  Create a standard Noxy project.
2.  Initialize a git repository.
3.  Push to a public host (e.g., GitHub).
4.  Users can now install it via `noxy --get`.
