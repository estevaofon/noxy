# Noxy Lambda Layer Deployment Guide

This walkthrough explains how to deploy Noxy as an AWS Lambda Layer and deploy your function separately.

## 1. Build the Layer

Run the layer build script to create `noxy_layer.zip`. This archive contains the Noxy runtime, the `bootstrap` executable, and shared libraries (like DynamoDB).

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

Run the function build script to create `function.zip`. This archive contains ONLY your function code (`function.nx`).

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
    -   `/opt/bin`: Noxy interpreter (`noxy`) and Plugins (`noxy-plugin-dynamodb`).
    -   `/opt/bootstrap`: The entry point script.
    -   `/opt/runtime`: The Noxy runtime loop scripts (`exec_runtime.nx`, `runtime.nx`).
-   **Function (`/var/task`)**:
    -   Contains only your `function.nx`.

The `bootstrap` script ensures `/opt/bin` is in the `PATH`, allowing Noxy to find plugins automatically. It also sets `NOXY_PATH` so the runtime module can be found.
