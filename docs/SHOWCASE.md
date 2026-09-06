# Noxy Showcase

Real projects written in Noxy. Each entry is a working system — not a snippet —
and every feature it needs or rough edge it hits feeds back into the language.

---

## Zombie Apocalypse

**Repository:** <https://github.com/estevaofon/zombie_apocalypse>
**Written in:** Noxy (simulation, rendering, collision editor, and tests) on top of [noxy_game_engine](https://github.com/estevaofon/noxy_game_engine)
**Status:** playable, 20 waves

<img width="800" alt="Zombie Apocalypse screenshot" src="https://github.com/user-attachments/assets/cd265feb-6f87-462c-ad92-ca4a0789d9dd" />

A top-down wave shooter set in an abandoned rail yard. Twenty waves of three
enemy types pour in from the edges of a 2816×1536 arena; between waves you
pick one of three random upgrades (fire rate, damage, speed, armor, triple
shot, piercing). Sprites are animated sheets, the map is a drawn image with
collision rectangles kept as data, and a built-in editor lets you adjust them.

### What it does

- **Engine-free simulation.** Vectors, RNG, world state, combat, waves,
  upgrades, and screen flow are pure arithmetic over a `World` struct. Only
  `src/render` draws; the entry point just turns input into vectors.
- **Data-driven collision.** Obstacles live in `data/obstacles.txt`; entities
  are pushed out of rectangles after moving, which yields wall sliding.
- **Collision editor.** `editor.nx` draws, moves, resizes, duplicates, undoes,
  and saves the rectangles over the map.
- **Headless tests.** `tests/run.nx` runs 294 asserts over simulation, map,
  animation, and editor without opening a window; `tests/smoke.nx` walks the
  four screens with a window open.

### Usage

```bash
noxy --sync                  # fetch noxy_game_engine
noxy zombie_apocalypse.nx    # play
noxy editor.nx               # edit collision rectangles
noxy tests/run.nx            # headless tests
```

### What it exercises in Noxy

| Language / stdlib area | How the game uses it |
|---|---|
| [Package manager](PACKAGE_MANAGER.md) | `noxy.mod` requires `noxy_game_engine`; `noxy --sync` resolves it |
| Modules and aliases | `use src.vec as vec` splits the game into eleven engine-agnostic modules |
| Structs and typed arrays | `Player`, `Enemy`, `Bullet`, `Particle`, `World` with `Enemy[]` and friends |
| [Explicit `ref`](REF_SEMANTICS.md) | `flow.advance(w: ref World, ...)` mutates one world in place, sixty times a second |
| Float arithmetic | Every frame is vector math: chase, separation, push-out, bullet lifetimes |
| File I/O and parsing | Obstacle file loaded and validated line by line; malformed input aborts with the line |
| Extensions | Drawing, input, and the window come from a Noxy package, not from the core |

---

## NoxyDB

**Repository:** <https://github.com/estevaofon/NoxyDB>
**Written in:** Noxy (core, storage engine, and server)
**Status:** v0.2

A lightweight, persistent **document key-value database**. A `string` key
maps to a JSON document persisted through an append-only log, and the whole
thing can run embedded or behind a local HTTP server.

### What it does

- **Append-only storage engine.** Puts and tombstones are hex-encoded records;
  opening a database replays and validates the log before appending.
- **Documents as `map[string, any]`.** `open_database`, `put`, `get`,
  `remove`, `exists`, `close_database`. Returned documents are fresh copies.
- **Two access modes.** Embedded via `use noxydb`, or remote through
  `server/noxydb_server.nx` with a Python client; one `.db` file per database.
- **Explicit failure states.** Writes hit the log before memory, and failures
  surface as errors rather than silent corruption.

### Usage

```noxy
use noxydb

let db: noxydb.Database = noxydb.open_database("database.db")
let stored: noxydb.PutResult = noxydb.put(db, "user:1", {"name": "Estevao", "active": true})
if stored.success then
    let result: noxydb.LookupResult = noxydb.get(db, "user:1")
    if result.found then
        print(result.value["name"])
    end
end
noxydb.close_database(db)
```

```powershell
noxy.exe server\noxydb_server.nx --data-dir .\data --port 8765
```

The server binds to `127.0.0.1` only, has no authentication, and enforces
per-connection deadlines. Document contents are never logged.

### What it exercises in Noxy

| Language / stdlib area | How NoxyDB uses it |
|---|---|
| Maps and `any` | Documents are `map[string, any]`, checked recursively for the JSON domain |
| [Value semantics (CoW)](REF_SEMANTICS.md) | Caller and returned documents are independent copies without defensive cloning |
| [Native JSON](JSON_SUPPORT.md) | `json_dumps` / `json_parse` at the log boundary |
| [HTTP server](HTTP_SERVER.md) | Remote transport with framing, deadlines, and bounded request limits |
| [Concurrency](concurrency.md) | A worker owns the database cache and receives commands over a channel |
| Structs and typed returns | `Database`, `PutResult`, `LookupResult` model outcomes without exceptions |
| File I/O and byte handling | Hex-encoded records, strict replay, explicit descriptor lifecycle |

Out of scope: queries, indexes, schemas, transactions, replication, `fsync`.

---

## Adding a project

New entries go above this section and follow the same shape:

```markdown
## <Project name>

**Repository:** <url>
**Written in:** <what part of it is Noxy>
**Status:** <version or maturity>

<One paragraph: what it is and who it is for.>

### What it does
<3–4 bullets, concrete behavior.>

### Usage
<Smallest runnable example.>

### What it exercises in Noxy
<Table mapping language/stdlib areas to the way the project uses them.>
```

Keep entries short — link to the project's own documentation for depth.
