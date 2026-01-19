# Noxy DynamoDB Client

A DynamoDB client for the Noxy language, implemented as a native Go plugin.

## Installation

Download the package using the Noxy Package Manager:

```bash
noxy --get github.com/estevaofon/noxy_dynamodb
```

## Build Requirements

Because this library uses a native Go plugin for high performance and AWS SDK integration, you must build the plugin binary after downloading.

**Prerequisites:**
- [Go 1.22+](https://go.dev/dl/) installed.

**Build Steps:**

1. Navigate to the installed library directory:
   ```bash
   cd noxy_libs/github_com/estevaofon/noxy_dynamodb
   ```

2. Run the build script for your OS:

   **Windows (PowerShell):**
   ```powershell
   .\build_plugin.ps1
   ```

   **Linux / macOS (Bash):**
   ```bash
   chmod +x build_plugin.sh
   ./build_plugin.sh
   ```

   This will create a `noxy-plugin-dynamodb` binary in the directory.

## Usage

Import the library and use the `dynamodb` module.

```noxy
use github_com.estevaofon.noxy_dynamodb as dynamodb

func main() -> void
    // 1. Connect (Uses default AWS credentials)
    let client: dynamodb.Client = dynamodb.connect({"region": "us-east-1"})

    // 2. Put Item
    let item: map[string, any] = {
        "id": "user_123",
        "name": "Estevao",
        "email": "estevao@example.com"
    }
    
    if dynamodb.put_item(client, "Users", item) then
        print("Item saved successfully!")
    end

    // 3. Get Item
    let key: map[string, any] = {"id": "user_123"}
    let result: map[string, any] = dynamodb.get_item(client, "Users", key)
    
    if result != null then
        print(f"Found user: {result['name']}")
    end
end

main()
```
