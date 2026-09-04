# Noxy Lambda Layer Deployment Guide

This walkthrough explains how to deploy Noxy as an AWS Lambda Layer and deploy your function separately.

## 1. Build the Layer

Run the layer build script to create `noxy_layer.zip`. This archive contains the Noxy runtime (built for Linux, `GOOS=linux GOARCH=amd64` — or `arm64` for Graviton) and the `bootstrap` executable.

```bash
./build_layer.sh
```

**Step 1.1: Create the Layer in AWS**
1.  Open the AWS Lambda Console.
2.  On the left sidebar, chose **Layers** (This is separate from "Functions").
3.  Click **Create layer**.
4.  **Name**: `noxy-runtime`.
5.  **Upload**: Select the `noxy_layer.zip` file you just built.
6.  **Architectures**: Select `x86_64` (since we compiled for AMD64).
7.  **Runtimes**: Select `Custom runtime on Amazon Linux 2`.
8.  Click **Create**.

## 2. Build the Function

Run the function build script to create `function.zip`. This archive contains your function code (`function.nx`) and, when the function uses packages, its `noxy_libs/` and `noxy.sum` (see [Packages and extensions](#packages-and-extensions-dynamodb) below).

```bash
./build_function.sh
```

**Step 2.1: Setup the Function**
1.  Go to your Lambda **Function** in the console.
2.  Scroll down to the **Layers** section (usually at the bottom of the "Code" tab).
3.  Click **Add a layer**.
4.  Choose **Custom layers**.
5.  Select the `noxy-runtime` layer you created in Step 1.
6.  Click **Add**.

**Step 2.2: Upload Code**
1.  In the **Code source** section, click **Upload from** -> **.zip file**.
2.  Upload `function.zip`.
3.  In **Runtime settings**, ensure "Runtime" is set to `Custom runtime on Amazon Linux 2`.
4.  Ensure **Handler** is `function.handler` (or your entry point).

## How it Works

-   **Layer (`/opt`)**:
    -   `/opt/bin`: the Noxy interpreter (`noxy`).
    -   `/opt/bootstrap`: The entry point script.
    -   `/opt/runtime`: The Noxy runtime loop scripts (`exec_runtime.nx`, `runtime.nx`).
-   **Function (`/var/task`)**:
    -   Your `function.nx`, plus `noxy_libs/` and `noxy.sum` when it uses packages.

The `bootstrap` script sets `NOXY_PATH` so the runtime module can be found. Nothing else goes on `PATH`: extensions are not looked up there (see below).

## Packages and extensions (DynamoDB)

Since v0.23.0, [`noxy_dynamodb`](DYNAMODB.md) is a process extension: a package directory holding the manifest, the wrapper and a per-platform binary under `bin/`, which the VM starts itself on the first call — it does **not** search `PATH` for plugins. The package is found by the module resolver like any other package, and its binary is verified against `noxy.sum` when the package lives under the VM root's `noxy_libs/`. The VM root is the directory of the script `noxy` was started with (`/var/task` when `bootstrap` runs the function script directly; `/opt/runtime` when it runs the runtime loop script, which then imports the function).

Two layouts work:

1. **Verified — under the root's `noxy_libs/`.** Ship the package as `noxy --get` installs it, next to the root's `noxy.sum`:

   ```
   <root>/noxy.sum
   <root>/noxy_libs/github_com/estevaofon/noxy_dynamodb/
   ├── noxy_ext.toml
   ├── noxy_dynamodb.nx
   └── bin/noxy-plugin-dynamodb-linux-amd64        # executable; -linux-arm64 on Graviton
   ```

   `noxy --get github.com/estevaofon/noxy_dynamodb@v0.3.0` on any workstation records the hashes of **all** published binaries in `noxy.sum`, so a `noxy.sum` generated on Windows or macOS verifies the Linux binary the Lambda runs. `--get` only downloads the workstation's own binary, though: fetch the Lambda's from the release page into `bin/` before zipping:

   ```bash
   curl -L -o noxy_libs/github_com/estevaofon/noxy_dynamodb/bin/noxy-plugin-dynamodb-linux-amd64 \
     https://github.com/estevaofon/noxy_dynamodb/releases/download/v0.3.0/noxy-plugin-dynamodb-linux-amd64
   chmod +x noxy_libs/github_com/estevaofon/noxy_dynamodb/bin/noxy-plugin-dynamodb-linux-amd64
   ```

   The execute bit must survive the zip (build the archive on Linux, or in a Linux step of your pipeline), and the binary's architecture must match the function's (`x86_64` → `linux-amd64`, `arm64` → `linux-arm64`).

2. **Unverified — in a `NOXY_PATH` root.** Put the same package directory under a root listed in `NOXY_PATH` (for example `/opt/noxy_libs/github_com/estevaofon/noxy_dynamodb/` in the layer, with `NOXY_PATH=/opt/noxy_libs:/opt/runtime` in `bootstrap`). The resolver finds it, but `noxy.sum` is not consulted outside the root's `noxy_libs/` — a development layout, convenient for a shared layer, without the integrity check.

Credentials come from the function's execution role through the default AWS chain; `capabilities = ["net", "env"]` in the manifest is exactly that. Give the role `dynamodb:*` permissions on the tables the function touches.
