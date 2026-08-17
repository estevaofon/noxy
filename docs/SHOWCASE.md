# Noxy Showcase

Real projects written in Noxy. Each entry is a working system — not a snippet —
that exercises the language, its VM, and its standard library under load.

These projects are how Noxy gets tested outside its own test suite. Every
feature they need, every rough edge they hit, and every performance cliff they
expose feeds directly back into the language: the copy-on-write value
semantics, the HTTP server module, and the native JSON functions all matured
this way.

---

## NoxyDB

**Repository:** <https://github.com/estevaofon/NoxyDB>
**Written in:** Noxy (core, storage engine, and server)
**Status:** v0.2

A lightweight, persistent **document key-value database** written entirely in
Noxy. A `string` key identifies a JSON document — strings, numbers, booleans,
`null`, arrays, and nested objects — persisted through an append-only storage
engine and retrieved, replaced, or removed by key.

### What it does

- **Append-only storage engine.** Every write is a `P` record (put) or `D`
  record (tombstone) appended to a log of hex-encoded key/payload pairs.
  Opening a database replays the log, validating record termination, arity,
  operation, hex data, and byte count before the file is opened for append.
- **Documents as `map[string, any]`.** The public API is `open_database`,
  `put`, `get`, `remove`, `exists`, and `close_database`. `put` replaces the
  whole document; `get` deserializes a fresh map on every lookup, so mutating a
  returned document can never reach into database state.
- **Two access modes.** Embedded, by importing `noxydb` from a Noxy program; or
  remote, through an HTTP server (`server/noxydb_server.nx`) with a Python
  client on the other end. One server manages many logical databases, one
  isolated `.db` file per name.
- **Explicit failure states.** A database is open, normally closed, or failed.
  Writes reach the log before the in-memory map changes, and write or
  physical-close failures surface as errors rather than silent corruption.

### Embedded usage

```noxy
use noxydb

let db: noxydb.Database = noxydb.open_database("database.db")
let user: map[string, any] = {
    "name": "Estevao",
    "age": 30,
    "active": true,
    "languages": ["Python", "Noxy"]
}

let stored: noxydb.PutResult = noxydb.put(db, "user:1", user)
if stored.success then
    let result: noxydb.LookupResult = noxydb.get(db, "user:1")
    if result.found then
        print(result.value["name"])
    end
else
    print(stored.error)
end
noxydb.close_database(db)
```

### Server usage

```powershell
noxy.exe server\noxydb_server.nx --data-dir .\data --port 8765
```

```python
from noxydb import NoxyDBClient

client = NoxyDBClient("http://127.0.0.1:8765")
db = client.open_database("usuarios")
db.put("user:1", {"name": "Estevão", "active": True})

result = db.get("user:1")
if result.found:
    print(result.value["name"])
```

The server binds to `127.0.0.1` only and has no authentication because it is
local-only. Each connection carries a read-idle deadline and a finite absolute
deadline, so a slow client cannot hold a handler open. Requests are logged with
timestamp, method, route, database, key, status, and duration — document
contents are never logged.

### What it exercises in Noxy

| Language / stdlib area | How NoxyDB uses it |
|---|---|
| Maps and `any` | Documents are `map[string, any]`; the JSON domain check walks them recursively |
| [Value semantics (CoW)](REF_SEMANTICS.md) | Caller documents and returned documents are independent copies — isolation comes from the language, not from defensive cloning |
| [Native JSON](JSON_SUPPORT.md) | `json_dumps` / `json_parse` are the serialization boundary between documents and the log |
| [HTTP server](HTTP_SERVER.md) | The remote transport: framing, deadlines, and bounded request limits |
| [Concurrency](concurrency.md) | A worker owns the database cache and receives commands over a channel |
| Structs and typed returns | `Database`, `PutResult`, `LookupResult` model outcomes without exceptions |
| File I/O and byte handling | Hex-encoded records, strict replay, explicit descriptor lifecycle |

### Deliberately out of scope

Queries, JSON Path, partial updates, indexes, schemas, collections, filters,
compaction, TTL, transactions, replication, and sharding. Persistence is
guaranteed after a successful embedded `close_database`; `fsync` and crash
durability are not provided.

---

## Adding a project

New showcase entries follow the same shape, appended above this section:

```markdown
## <Project name>

**Repository:** <url>
**Written in:** <what part of it is Noxy>
**Status:** <version or maturity>

<One-paragraph description: what it is and who it is for.>

### What it does
<3–5 bullets, concrete behavior.>

### Usage
<Smallest runnable example.>

### What it exercises in Noxy
<Table mapping language/stdlib areas to the way the project uses them.>
```

Keep entries short and factual — link to the project's own documentation for
depth instead of duplicating it here.
