# DynamoDB for Noxy

[`noxy_dynamodb`](https://github.com/estevaofon/noxy_dynamodb) is a DynamoDB
client packaged as a **process extension** (`kind = "process"`, see
[docs/EXTENSIONS.md](EXTENSIONS.md)): `noxy --get` downloads a prebuilt
binary for your platform, verifies it, and records its hash in `noxy.sum`.
No Go toolchain, no build step. Requires Noxy **0.23.0** or newer.

## Installation

```bash
noxy --get github.com/estevaofon/noxy_dynamodb@v0.3.0
```

Without `@version`, `--get` resolves the newest release tag. The package
lands in `noxy_libs/github_com/estevaofon/noxy_dynamodb/`:

```
noxy_libs/github_com/estevaofon/noxy_dynamodb/
├── noxy_ext.toml                                  # kind = "process"
├── noxy_dynamodb.nx                               # typed wrapper
└── bin/noxy-plugin-dynamodb-windows-amd64.exe     # this platform's binary
```

`noxy.sum` gets one line for the manifest plus one per published binary
(linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64,
windows/arm64) — commit it: the same lockfile verifies a teammate's macOS
download and a Lambda's Linux binary.

## Usage example

```noxy
use github_com.estevaofon.noxy_dynamodb as dynamodb

func main() -> void
    // 1. Connect — credentials and region come from the environment
    let client: dynamodb.Client = dynamodb.connect({"region": "us-east-1"})

    // 2. Put item
    let item: map[string, any] = {
        "id": "user_123",
        "name": "Estevao",
        "email": "estevao@example.com"
    }
    if dynamodb.put_item(client, "Users", item) then
        print("Item saved successfully!")
    end

    // 3. Get item — null when the key does not exist
    let found: map[string, any]? = dynamodb.get_item(client, "Users", {"id": "user_123"})
    if found != null then
        print(f"Found user: {found['name']}")
    end

    // 4. Scan and query take a limit; page through big tables with scan_page
    let users: map[string, any][] = dynamodb.scan(client, "Users", 100)
    print(f"{length(users)} user(s)")

    dynamodb.close(client)
end

main()
```

## API

| Function | Returns | On failure |
|---|---|---|
| `connect(options: map[string, any])` | `Client` | raises |
| `close(client: Client)` | `void` | raises |
| `put_item(client, table, item)` | `bool` | `false` |
| `get_item(client, table, key)` | `map[string, any]?` (`null` when missing) | raises |
| `update_item(client, table, key, update_expression, expression_values)` | `bool` | `false` |
| `delete_item(client, table, key)` | `bool` | `false` |
| `scan(client, table, limit)` | `map[string, any][]` — up to `limit` items | raises |
| `scan_page(client, table, limit, start_key)` | `Page{items, last_key: map[string, any]?}` — one request | raises |
| `query(client, table, key_condition, expression_values, limit)` | `map[string, any][]` — up to `limit` items | raises |
| `query_page(client, table, key_condition, expression_values, limit, start_key)` | `Page` — one request | raises |

`scan` and `query` follow pagination only as far as `limit` requires and ask
DynamoDB for no more than the remainder per request; `limit = 0` means every
item (opt-in — on a large table that runs until the 60 s call deadline). To
walk a big table in pieces, loop on `scan_page` / `query_page`, passing
`page.last_key` back as `start_key` until it is `null`.

Failures are the extension's runtime errors — `extension 'dynamodb' failed:
<message from AWS>` — capturable with `call_result` (`use errors select *`).
A dead plugin process surfaces as `extension 'dynamodb' trapped: ...` and
poisons the extension for the rest of the program. Each call has a 60 s
deadline. Numbers come back as `float`; structs go in as maps of their
fields. Connect options: `"region"` (else `AWS_REGION` / shared config, else
`us-east-1`), `"endpoint"` (DynamoDB Local) and `"profile"`. Full reference,
type mapping and the migration notes from v0.1.x: the
[repository README](https://github.com/estevaofon/noxy_dynamodb#readme).

## AWS Lambda

Deploy the installed package directory — with the Linux binary in `bin/`
and the execute bit — next to your `noxy.mod` and `noxy.sum`. The VM
verifies the binary against `noxy.sum` before starting it; nothing goes on
`PATH`. See [docs/AWS_LAMBDA_LAYER.md](AWS_LAMBDA_LAYER.md).
