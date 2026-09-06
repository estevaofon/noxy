# Noxy Showcase

Real projects written in Noxy. Each one is a working system, and every feature
it needs or rough edge it hits feeds back into the language.

---

## Zombie Apocalypse

<img width="800" alt="Zombie Apocalypse screenshot" src="https://github.com/user-attachments/assets/cd265feb-6f87-462c-ad92-ca4a0789d9dd" />

**Repository:** <https://github.com/estevaofon/zombie_apocalypse>
**Written in:** Noxy, on top of [noxy_game_engine](https://github.com/estevaofon/noxy_game_engine)

A top-down wave shooter in an abandoned rail yard: twenty waves, three enemy
types, upgrades between waves, animated sprite sheets, data-driven collision
with a built-in editor, and a headless test suite over the whole simulation.

| Exercises | |
|---|---|
| [Package manager](PACKAGE_MANAGER.md) | `noxy.mod` + `noxy --sync` pull the engine |
| Modules, structs, typed arrays | Eleven engine-agnostic modules over a `World` struct |
| [Explicit `ref`](REF_SEMANTICS.md) | One world mutated in place, sixty times a second |
| File I/O and parsing | Obstacle rectangles loaded and validated from a text file |

---

## NoxyDB

**Repository:** <https://github.com/estevaofon/NoxyDB>
**Written in:** Noxy (core, storage engine, and server)

A lightweight, persistent document key-value database: string keys, JSON
documents, an append-only log, and a choice between embedded use or a local
HTTP server with a Python client.

| Exercises | |
|---|---|
| Maps and `any` | Documents are `map[string, any]` |
| [Value semantics (CoW)](REF_SEMANTICS.md) | Returned documents are independent copies for free |
| [Native JSON](JSON_SUPPORT.md) | `json_dumps` / `json_parse` at the log boundary |
| [HTTP server](HTTP_SERVER.md) + [concurrency](concurrency.md) | Remote transport with a worker owning the database cache |
| File I/O | Hex-encoded records with strict replay on open |

---

## Adding a project

New entries go above this section: name, repository, what part is Noxy, one
paragraph, and a short table of what it exercises. Link to the project's own
documentation for depth.
