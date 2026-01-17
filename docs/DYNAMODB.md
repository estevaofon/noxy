# DynamoDB Plugin for Noxy

This module allows Noxy programs to interact with AWS DynamoDB. It uses a **Plugin Architecture** where the database logic runs in a separate process (`noxy-plugin-dynamodb`), keeping the core Noxy VM lightweight and dependency-free.

## 1. Installation & Build

Because the plugin is a separate binary, you must build both the Noxy VM and the Plugin.

### Prerequisites
- Go 1.24+
- AWS Credentials configured (Environment variables or `~/.aws/credentials`)

### Build Commands (Windows/Linux/Mac)

Run these commands from the project root:

# 1. Build the generic Noxy VM
go build -o noxy.exe ./cmd/noxy

# 2. Organize into noxy_libs Structure (Required)
mkdir -p noxy_libs/dynamodb

# 3. Build the DynamoDB Plugin
# We use -C to build inside the plugin directory without changing folders
go -C noxy_libs/dynamodb/plugin build -o ../noxy-plugin-dynamodb.exe

# The result file structure should be:
```
noxy_libs/dynamodb/
├── dynamodb.nx
└── noxy-plugin-dynamodb.exe
```

> **Note**: For Linux/Mac, remove the `.exe` extension.

## 2. Configuration

The plugin uses the standard AWS SDK v2 credential chain. It will automatically look for credentials in:
1.  **Environment Variables**: `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`.
2.  **Shared Config**: `~/.aws/credentials` and `~/.aws/config`.

## 3. Usage

Import the `dynamodb` module in your Noxy script.

```noxy
use dynamodb
```

### API Reference

#### `connect(options: map[string, any]) -> Client`
Initializes the plugin connection.
- `options`: Map containing configuration (e.g., `{"region": "us-east-1"}`).

#### `put_item(client: Client, table: string, item: map[string, any]) -> bool`
Creates or replaces an item.
- `item`: A map where keys are strings and values can be any type (int, string, bool, etc.).

#### `get_item(client: Client, table: string, key: map[string, any]) -> map[string, any]`
Retrieves an item by its primary key. Returns `null` if not found.

#### `update_item(client: Client, table: string, key: map[string, any], updateExpr: string, exprVals: map[string, any]) -> bool`
Updates an existing item using a DynamoDB Update Expression.
- `updateExpr`: e.g., `"SET age = :newAge"`
- `exprVals`: Map of placeholders to values, e.g., `{":newAge": 30}`

#### `delete_item(client: Client, table: string, key: map[string, any]) -> bool`
Deletes an item by its primary key.

#### `scan(client: Client, table: string) -> []map[string, any]`
Retrieves all items from the table. Returns a list of maps.

#### `query(client: Client, table: string, keyCond: string, exprVals: map[string, any]) -> []map[string, any]`
Finds items based on key conditions.
- `keyCond`: Key Condition Expression, e.g., `"id = :id"`
- `exprVals`: Value map for placeholders, e.g., `{":id": "123"}`

### Query Syntax Guide

The `keyCond` parameter supports standard DynamoDB Key Condition Expressions. 
**Important**: You can only query on Key attributes (Partition Key and optional Sort Key).

| Operation | Syntax Example | Description |
| :--- | :--- | :--- |
| **Equality** | `id = :id` | Strict match on Partition Key. |
| **AND Sort Key** | `id = :id AND sk > :min` | Match PK and restrict Sort Key range. |
| **Begins With** | `id = :id AND begins_with(sk, :p)`| Match items where Sort Key starts with prefix. |
| **Between** | `id = :id AND sk BETWEEN :a AND :b` | Match Sort Key in inclusive range. |

### Examples

**Complete CRUD Example:**
```noxy
use dynamodb

// 1. Connect
let client: dynamodb.Client = dynamodb.connect({"region": "us-east-1"})

// 2. Put
let user: map[string, any] = {
    "id": "u-101",
    "name": "Alice",
    "active": true
}
dynamodb.put_item(client, "Users", user)

// 3. Get
let fetched: map[string, any] = dynamodb.get_item(client, "Users", {"id": "u-101"})
print(fetched["name"]) // "Alice"

// 4. Update
dynamodb.update_item(client, "Users", {"id": "u-101"}, "SET active = :a", {":a": false})

// 5. Delete
dynamodb.delete_item(client, "Users", {"id": "u-101"})
```

## 4. AWS Lambda Deployment

To run on AWS Lambda, use the provided build script which compiles both binaries for Linux and packages them with the runtime.

```bash
./build_lambda_with_dynamo.sh
```

This generates `deploy_dynamo.zip` ready for upload.
