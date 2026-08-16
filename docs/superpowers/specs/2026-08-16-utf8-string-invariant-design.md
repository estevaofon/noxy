# UTF-8 String Invariant — Design

**Date:** 2026-08-16
**Status:** Proposal
**Branch:** continues the philosophy of `fix/stdlib-reliability` (`55b7d`/`55767a0`:
conversions raise instead of returning sentinels; `3a0c199`: text functions
reject bytes instead of silently reading them)

## The defect

A Noxy `string` is stored as a Go `string` — an arbitrary byte sequence. UTF-8
validity is **assumed at every character-based operation but guaranteed
nowhere**. No constructor validates:

- `to_str(bytes)` retags the payload without inspection
  (`internal/vm/builtins_core.go:34`).
- `io_read` labels raw file content as `string`
  (`internal/vm/builtins_io.go:212`).
- `sqlite` returns TEXT columns as `string` straight from the driver
  (`internal/vm/builtins_sqlite.go:447`); SQLite does not enforce UTF-8.

When the assumption is false, corruption is silent and irreversible.
Reproduced with `h` + `0xFF` + `i` (0xFF is never valid in UTF-8):

```noxy
let bad: bytes = to_bytes([104, 255, 105])
let s: string = to_str(bad)          // no error

length(s)                            // 3 — the invalid byte counts as 1 char
strings.char_code(s[1])              // 65533 (U+FFFD) — the 0xFF is gone
to_bytes(s)[1]                       // 255 — plain retag still preserves it
to_bytes(strings.substring(s, 0, 3)) // 5 bytes — U+FFFD materialized as EF BF BD
```

Any rune-based operation (`s[i]`, `substring`, `reverse`, `char_at`) decodes
the invalid byte as U+FFFD and re-encoding writes 3 replacement bytes where 1
original byte was. No error, no warning. A program that reads a file, slices a
string, and writes it back has corrupted data it never touched.

The only validation in the runtime today is in JSON
(`builtins_json.go:88`, `json_strict.go:47,99`).

## The fix

**Invariant: every Noxy `string` holds valid UTF-8.** Validation happens once,
at the boundaries where bytes become text. Inside the invariant, every
character-based operation is correct by construction — the U+FFFD scenario
above becomes unreachable.

This is the Python 3 model, which the language spec already claims for its
indexing semantics: `bytes.decode()` raises on invalid input; it never
substitutes silently.

### Boundary inventory (verified)

| Boundary | Location | Today | Change |
|---|---|---|---|
| `to_str(bytes)` | `builtins_core.go:34` | retag, no check | **validate, raise** |
| `io_read` (text) | `builtins_io.go:188-212` | raw bytes as string | **validate, `ok=false` + `error` in `IOReadResult`** |
| `io_read_lines` | `builtins_io.go:266` | raw bytes as strings | same contract as `io_read` |
| `io_read_bytes` | `builtins_io.go:220` | returns `bytes` | **unchanged** — this is the raw escape hatch |
| `net_recv` | `builtins_net.go:334-341` | already returns `bytes` | **unchanged** — network is already clean; text arrives via `to_str` |
| sqlite TEXT column | `builtins_sqlite.go:447` | driver string, no check | validate; surface through the row's existing `error` field |
| Module/source loading | `vm/modules.go:134`, `compiler/module_exports.go:185` | read raw | validate whole file; report as a load/syntax error (embedded stdlib already has this as a test, `stdlib_hygiene_test.go:130`) |
| `sys.run` output, `sys.env` | `builtins_sys.go:115,158,278` | raw as string | validate; use each function's existing error channel |
| String literals, f-strings, JSON | lexer / `json_strict.go` | covered by source validation / already validated | unchanged |

Key structural point: because `net_recv` already returns `bytes` and
`io_read_bytes` exists, **`to_str` is the single choke point** for
program-driven decoding. The other rows are passive inputs (files, DB, env)
that each already own an error-reporting channel.

### Error contract

- `to_str` raises a synchronous runtime error, catchable like the `to_int` /
  `to_float` errors introduced in `55767a0`:

  ```
  to_str: bytes are not valid UTF-8 at byte offset N
  ```

  Include the first invalid offset; bound any echoed payload using the
  existing `truncateConversionInput` convention
  (`builtins_convert.go:14-28`).

- Functions with a result-struct contract (`io_read`, sqlite) do **not**
  raise; they report through their existing `ok`/`error` fields. Raising is
  reserved for pure conversions, matching the branch's established split.

### New builtin: `strings.is_valid_utf8`

```noxy
func is_valid_utf8(b: bytes) -> bool
```

Native `strings_is_valid_utf8` wrapping `utf8.ValidString`; exported from
`stdlib/strings.nx`. This is the check-before-decode path for programs that
handle dirty data deliberately.

**The parameter is strictly `bytes`; a `string` argument is a type error.**
The question "is this valid UTF-8?" only makes sense before decoding — once a
value is a `string`, the invariant already answers it, and a program asking
anyway has placed its gate after the door (`to_str` on the previous line
would have raised first). Returning a trivial `true` would silence that logic
error. This mirrors the `3a0c199` rule in the opposite direction (text
functions reject `bytes`; this bytes function rejects `string`) and Python's
model (`str` has no `.decode`).

The static `b: bytes` annotation in the `strings.nx` wrapper is **not** enough
on its own. It is enforced only for a direct call inside the same compiled
unit; `use strings select *` — the path every real caller takes — erases it,
and the argument reaches the native unchecked. So `strings_is_valid_utf8`
carries its own runtime guard, `requireBytesArgument`
(`internal/vm/builtins_strings.go`), the mirror image of `requireTextArgument`
in the opposite direction: text functions reject `bytes`, this bytes function
rejects `string`. Keep both — the annotation documents the contract, the guard
enforces it. Do **not** widen the signature to `any`, and do not delete the
guard as redundant. The message the guard raises is:

```
strings.is_valid_utf8: expected bytes, got string; use to_bytes(value) to convert explicitly
```

Deferred (add only on demand): `to_str_lossy(bytes)` with explicit U+FFFD
substitution — Python's `errors="replace"`. Not needed for the invariant; a
user can branch on `is_valid_utf8` today.

## Implementation notes

- `to_str` is registered with `DefineNative`, whose signature
  (`func(args) value.Value`) cannot return an error. Re-register it as a
  contextual native (`DefineContextualNative`), the same migration `to_int`
  and `to_float` went through.
- Add one helper, e.g. in `builtins_strings.go` next to
  `requireTextArgument`:

  ```go
  // requireValidUTF8 guards the bytes→string boundary. Returns the first
  // invalid byte offset for the error message.
  func requireValidUTF8(function, data string) error
  ```

  Find the offset with a single `utf8.DecodeRuneInString` walk or
  `utf8.ValidString` + a locate pass on failure (failure path only, so cost
  is irrelevant).
- Validation cost is one O(n) scan at creation, only at boundaries. Interior
  operations (`[]rune`, `RuneCountInString`) are already O(n) per call; no
  hot-path regression.
- Plain retags between valid `string` and `bytes` stay allocation-free.

## What does not change

- The code-point indexing model (Python 3 style) and its documented
  grapheme-cluster imprecision.
- `bytes` semantics: byte-indexed, byte-length, arbitrary content.
- No Unicode normalization (NFC/NFD). Comparison stays byte-exact, as in
  Python. Out of scope; document the limitation in the spec instead.
- `is_alpha`/`is_digit` ASCII-vs-Unicode classification — separate decision,
  separate change.
- `pad_left` byte-length bug and `\uXXXX` lexer escapes — related fixes, but
  independent of this design.

## Breaking change and migration

This is a `fix!`. Programs that today read invalid UTF-8 through `io_read` or
`to_str` and "work" are silently corrupting data; after this change they get
a diagnosable error at the boundary instead of U+FFFD damage far from the
cause. Migration path in the CHANGELOG: use `io_read_bytes` /
keep values as `bytes`, or gate with `strings.is_valid_utf8`.

## Test plan (TDD, per repo practice)

- [ ] `to_str` on valid UTF-8 bytes round-trips byte-for-byte (existing behavior).
- [ ] `to_str(b"\xff")` raises; message names the function, says UTF-8, includes offset 0.
- [ ] `to_str` on truncated multi-byte sequence (e.g. first byte of `é` only) raises with the correct offset.
- [ ] Error message is bounded for large payloads (mirror `builtins_convert_test.go:179`).
- [ ] `io_read` on a file containing `0xFF`: `ok == false`, `error` mentions UTF-8; `io_read_bytes` on the same file succeeds.
- [ ] `io_read_lines` same contract.
- [ ] `strings.is_valid_utf8` — true/false/empty-bytes cases.
- [ ] `strings.is_valid_utf8("text")` fails with `function 'is_valid_utf8' argument 1: expected bytes, got string` (falls out of the `b: bytes` signature; the test pins the signature against widening to `any`).
- [ ] Loading a `.nx` source file with invalid UTF-8 fails with a load error naming the file.
- [ ] sqlite TEXT column with invalid bytes surfaces `error` instead of a corrupt string (fixture DB or direct insert via bytes).
- [ ] JSON behavior unchanged (already validated).
- [ ] Executor characterization: after the invariant, no `string` reaching `OP_INDEX`/`OP_LEN` contains invalid UTF-8 in any stdlib path.
