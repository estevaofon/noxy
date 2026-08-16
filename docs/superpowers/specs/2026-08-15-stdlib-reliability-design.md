# Standard Library Reliability Design

## Goal and Scope

Five defects share one shape: an API that fails without saying so. A
conversion that cannot succeed returns a plausible number. An index function
returns an offset measured in a unit the functions that consume it do not use.
A text function silently accepts a value it cannot interpret and operates on
its debug representation. Diagnostic output ships to end users. Source comments
carry characters destroyed by a lossy encoding pass.

This subproject fixes all five. It is not a feature: every change either makes
a silent failure loud or removes output that should never have shipped.

Excluded by decision: the `net_settimeout` error style, which the network
deadlines design settles deliberately; `continue`, which is a language feature
with its own branch; the `\u` string escape; and the cost of character
indexing, recorded as follow-up work at the end of this document.

## 1. Strict Numeric Conversion with a Recoverable Companion

### The defect

`to_int("abc")` returns `0`, and `to_int("0")` returns `0`. No caller can tell
the two apart. `to_float` behaves the same way with `0.0`. Every call site that
parses configuration, user input, or a wire format silently substitutes a
plausible value for a failure.

### The fix

`to_int` and `to_float` become strict and raise a synchronous runtime error
when the conversion cannot succeed. A companion pair returns a value the caller
can branch on:

| Function | On failure |
|---|---|
| `to_int(v)` | synchronous runtime error |
| `to_float(v)` | synchronous runtime error |
| `to_int_result(v)` | `IntResult` with `ok = false` |
| `to_float_result(v)` | `FloatResult` with `ok = false` |

The pair is required, not a convenience. Noxy has `defer` for cleanup but no
construct that catches a runtime error, so a strict conversion alone would
leave code that parses untrusted input with no way to recover. The `_result`
suffix is the convention the standard library already uses for exactly this
split in `io.close` / `io.close_result` and `io.write` / `io.write_result`.

### Conversion rules

The rules follow Python, where converting a *number* is a defined narrowing and
parsing a *string* either succeeds or fails.

| Input | `to_int` | `to_float` |
|---|---|---|
| `int` | the value | the value as `float` |
| `float`, finite | truncated toward zero | the value |
| `float`, `NaN` or `±Inf` | error | the value |
| `string` parsable by Go `ParseInt` base 10, 64-bit | the value | — |
| `string` parsable by Go `ParseFloat` 64-bit | — | the value |
| `string` otherwise, including `"5.5"`, `""`, `" 5"`, `"0x10"`, and overflow | error | error |
| `bool` | error | error |
| `null` | error | error |
| any composite, function, channel, or handle | error | error |

`to_int(5.9)` is `5` while `to_int("5.5")` is an error. This mirrors Python's
`int(5.9) == 5` and `int("5.5")` raising `ValueError`.

Noxy diverges from Python for `bool` deliberately. Python defines
`int(True) == 1` because its `bool` subclasses `int`. Noxy's `bool` is an
independent type with no numeric heritage, and implicit boolean-to-number
coercion hides bugs, so it is an error.

`to_float` accepts the strings `NaN`, `Inf`, `+Inf`, and `-Inf` because Go's
`ParseFloat` accepts them and they name representable `float64` values.
`to_int` rejects the corresponding float values because they have no integer
representation.

### Error message

A failed strict conversion raises a message that names the function, the
rejected type, the rejected value, and the recoverable alternative:

```text
to_int: cannot convert string "abc" to int; use to_int_result to handle failure
```

The rendered value is truncated to 64 characters followed by `...` so a large
input cannot flood the error. A rejected non-string value is rendered by its
type name and its ordinary string form.

Calling `to_int` or `to_float` with anything other than exactly one argument
also raises, instead of returning `0` as it does today. The `_result` forms
report the same arity violation through `ok = false` rather than raising, so
the guarantee that a `_result` call never raises holds for every input.

### Public API

A new module `internal/stdlib/convert.nx` declares the result types and wraps
two new natives, following the pattern where a native returns a map and the
module declares the typed struct that receives it:

```noxy
struct IntResult
    ok: bool
    value: int
    error: string
end

struct FloatResult
    ok: bool
    value: float
    error: string
end

func to_int_result(v: any) -> IntResult
func to_float_result(v: any) -> FloatResult
```

The natives are `convert_to_int_result` and `convert_to_float_result`, matching
the `module_function` naming used by `net_*`, `strings_*`, and `io_*`.

On success `ok` is true, `value` carries the converted number, and `error` is
empty. On failure `ok` is false, `value` is `0` or `0.0`, and `error` carries
the same text the strict form would have raised, without the trailing
recommendation clause.

## 2. Character-Based `index_of`

### The defect

`strings_index_of` forwards Go's `strings.Index`, which returns a byte offset.
Every function that consumes an index in Noxy — `substring`, `char_at`,
`length`, `slice` on a `string` — measures in characters. The two do not
compose:

```noxy
let name: string = substring(line, 0, index_of(line, ":"))
```

This reads correctly, is correct for ASCII, and silently cuts in the wrong
place for anything else.

It is not theoretical. `http_parser.nx` composes exactly these two functions in
`parse_url`, so `http://münchen.de/path` currently yields host `münchen.de/`
and path `path`.

### The fix

`strings_index_of` returns the index measured in characters, so every index in
the string API shares one unit. It returns `-1` when the substring is absent
and `0` for an empty substring, matching current behavior.

The implementation locates the byte offset with `strings.Index` and converts it
with `utf8.RuneCountInString(s[:byteOffset])`, so a search still runs at Go's
speed and only the returned offset is translated.

`parse_url` is thereby fixed without a change to its own code.

### Terminology

Noxy indexes strings by Unicode code point, which is the Python 3 model. User
facing material — the language spec, module documentation, and runtime error
messages — says **character**. `rune` appears only in Go implementation code,
where it is the host language's word for the same thing. Noxy does not borrow
its implementation language's vocabulary.

Documentation states the known imprecision: a code point is not always a
user-perceived character. `é` may be one code point or two, and an emoji with
modifiers is several. Only a grapheme-cluster model is exact, and it cannot
offer an integer index in constant time. Noxy accepts the code-point
approximation, as Python does, and says so rather than implying more precision
than it delivers.

## 3. Text Functions Reject `bytes`

### The defect

Every `strings_*` native reads its text parameter with `Value.String()`. For a
`VAL_BYTES` value that method returns the *display* form:

```go
case VAL_BYTES:
    return fmt.Sprintf("b\"%s\"", v.Obj.(string))
```

So `contains(payload, "x")` searches a string that begins with `b"` and ends
with `"`, and `index_of(payload, "/")` returns an offset shifted by the two
character prefix. The function reports a confident answer about text the caller
never wrote.

### The fix

A `strings_*` native parameter documented as `string` raises a synchronous
runtime error when it receives `VAL_BYTES`:

```text
strings.index_of: expected string, got bytes; use to_str(value) to convert explicitly
```

This establishes the boundary the rest of this document depends on:

- `string` is text, indexed by character;
- `bytes` is octets, indexed by octet, through `length`, `slice`, and element
  access;
- `to_str` and `to_bytes` are the explicit bridge between them.

The rule applies per parameter, not per native: `strings_split` rejects `bytes`
in its subject and separator but not in its struct-definition argument, and
`strings_from_char_code` is unaffected because it takes an `int`.

Only `VAL_BYTES` is rejected. Other types continue to be rendered by
`Value.String()` as they are today; an integer rendering as `"5"` is a defined
stringification, while a `bytes` rendering as `b"..."` is not text at all.

Nothing observable regresses, because the current result for a `bytes` argument
is meaningless in every case.

## 4. No Shipped Diagnostic Output

### The defect

Two debug statements reach end users:

- `internal/vm/builtins_net.go:556` prints
  `DEBUG: net_send args[0] not map: %T %v` to standard output;
- `internal/stdlib/http_client.nx:87` prints `Debug: request start <url>` on
  **every** HTTP client request.

The second corrupts the output of any program that uses the HTTP client, and
both would corrupt a program whose standard output is structured data.

### The fix

Both statements are removed. The `net_send` case that printed already returns
`value.NewNull()` for a malformed argument and keeps doing so.

An architecture guard test asserts that no non-test Go source under
`internal/` and no `.nx` source under `internal/stdlib/` contains the literal
markers `DEBUG:` or `Debug:`. The project already guards architecture
invariants this way in `internal/vm/architecture_test.go`, so the mechanism is
established rather than new.

## 5. Source Encoding Repair

### The defect

Twenty-one comment lines across three embedded standard library sources lost
their accented characters to a lossy encoding conversion, leaving a literal
`?` in place of each one: `M?dulo`, `usu?rio`, `Verifica??o`, `Convers?o`,
`Divis?o`, `s?bado`, `Diferen?a`, `Aritm?tica`, `padr?o`, `m?s`, and others.

| File | Affected lines |
|---|---|
| `internal/stdlib/http.nx` | 2 |
| `internal/stdlib/strings.nx` | 6 |
| `internal/stdlib/time.nx` | 13 |

### The fix

Each comment is restored to correct UTF-8 Portuguese. Only comments change; no
identifier, string literal, or behavior is touched.

A guard test asserts that every source embedded through `internal/stdlib/embed.go`
is valid UTF-8 and contains no U+FFFD replacement character. That guard cannot
detect the historical `?` substitution, which is indistinguishable from an
intentional question mark, but it does prevent the next lossy conversion from
landing silently.

## Migration

Item 1 is a breaking change. It is the point of the change: an additive
`to_int_result` alone would leave every existing `to_int` call carrying the
original defect, and almost none of them would ever be revisited.

The blast radius is small and enumerable — 27 call sites in Noxy sources, of
which 2 are in the standard library:

- `http_parser.nx:132`, `parse_url`, parses a URL port. A port that is absent,
  empty, or non-numeric currently becomes `0`. It moves to `to_int_result` and
  a failed parse makes the URL invalid: `valid` stays `false`.
- `http_parser.nx:233`, `parse_response`, parses a response status code. A
  malformed status line currently becomes `0` through the silent fallback. It
  moves to `to_int_result` and assigns `0` explicitly when the parse fails,
  which preserves the observable result exactly while making it a decision
  rather than an accident. `0` is chosen over leaving the constructor default
  of `200` because a malformed response must not read as a success.

The remaining 25 call sites live in `noxy_examples/` and `noxy_libs/`. Each is
audited individually: a call on a value that is always numeric keeps `to_int`,
and a call on parsed or external input moves to `to_int_result` with an
explicit branch. The 160-example suite is the regression gate for the
migration.

`CHANGELOG.md` records item 1 as breaking and states the migration recipe:
a `to_int` or `to_float` call that may receive non-numeric input becomes the
`_result` form with an explicit failure branch.

This change also improves the queued `feat/http-streaming-server` subproject.
Its `resolve_body_length` planned to gate every `Content-Length` conversion
behind `is_digit` purely because `to_int` could not report failure. With
`to_int_result` the gate becomes a direct, honest parse, and `is_digit` remains
only where the HTTP specification requires a rule stricter than "a valid
integer" — no sign and no leading zeros beyond a single `0`.

## Compatibility

- `to_int` and `to_float` change behavior for inputs they could not convert.
  Inputs they could already convert are unaffected.
- `to_int_result`, `to_float_result`, `IntResult`, and `FloatResult` are new
  and require `use convert`.
- `index_of` changes only for strings containing non-ASCII characters before
  the match. ASCII text is unaffected, and the sole standard library consumer
  is the call this change repairs.
- `strings_*` natives change only for `bytes` arguments, whose current result
  is meaningless.
- Removing the debug statements changes program output, which is the intent.
- Comment repair changes no behavior.
- No syntax, keyword, or type-system change.

## Testing

Go unit tests cover each native directly, and Noxy example tests cover the
public surface.

The matrix proves:

- `to_int` converts every accepted form and raises for every rejected form in
  the rules table, including `NaN`, `±Inf`, overflow, `bool`, `null`, and
  composites;
- `to_int(5.9)` truncates toward zero and `to_int(-5.9)` truncates toward zero,
  while `to_int("5.5")` raises;
- `to_float` accepts `NaN` and the infinities as strings and rejects malformed
  text;
- the raised message names the function, the rejected type, the rejected value
  truncated at 64 characters, and `to_int_result`;
- `to_int_result` and `to_float_result` return `ok = true` with the converted
  value for every accepted form, and `ok = false` with a zero value and a
  non-empty reason for every rejected form;
- the result form never raises for any input;
- `index_of` returns a character index for text containing multi-byte
  characters before the match, returns `-1` when absent, returns `0` for an
  empty needle, and agrees with the byte offset for pure ASCII;
- `index_of` composes correctly with `substring`, `char_at`, and `slice` on
  multi-byte text;
- `parse_url("http://münchen.de/path")` yields host `münchen.de` and path
  `/path`, as a direct regression for the repaired defect;
- every `strings_*` text parameter raises the documented error for a `bytes`
  argument, and continues to accept `string` unchanged;
- `strings_from_char_code` and the struct-definition parameter of
  `strings_split` are unaffected by the `bytes` rule;
- no non-test Go source under `internal/` and no `.nx` source under
  `internal/stdlib/` contains `DEBUG:` or `Debug:`;
- every source embedded by `internal/stdlib/embed.go` is valid UTF-8 with no
  U+FFFD;
- the HTTP client performs a request without writing anything to standard
  output beyond what the caller printed.

Project validation:

```text
go build ./...
go vet ./...
go test ./internal/...
go test -race ./internal/vm/
go run cmd/noxy/main.go noxy_examples/run_all_tests.nx
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

The example suites must report at least the pass count recorded before the
branch.

## Documentation

`docs/NOXY_LANGUAGE_SPEC.md` gains the strict conversion contract, the
`_result` companion convention, and a statement that strings are indexed by
character with the code-point approximation named explicitly. The `strings`
module documentation states that text functions reject `bytes` and that
`to_str` is the explicit bridge.

## Follow-Up Work

Character indexing over a UTF-8 backing store costs `O(n)` per access, so
`char_at` inside a loop is `O(n²)`. This is already true today and is the
reason the queued HTTP subproject bounds method and header-name lengths before
validating them character by character. Correcting it — by caching a decoded
form, exposing a character-iterator, or indexing lazily — is a separate
optimization subproject and changes no semantics defined here.
