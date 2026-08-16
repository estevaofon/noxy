# Standard Library Reliability Design

## Goal and Scope

Seven defects share one shape: an API that fails without saying so. A
conversion that cannot succeed returns a plausible number. An index function
returns an offset measured in a unit the functions that consume it do not use.
A text function silently accepts a value it cannot interpret and operates on
its debug representation. Diagnostic output ships to end users. Source comments
carry characters destroyed by a lossy encoding pass. Two natives are registered
twice, so one definition silently wins. A character-code function reads a byte.

This subproject fixes all seven. It is not a feature: every change either makes
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

The pair is required, not a convenience, for two independent reasons.

**A correct guard cannot be written in Noxy today.** A caller who wants to
avoid the strict form must validate first, and the language offers no sound way
to do it. `is_digit` covers non-negative integers but not range:

```noxy
is_digit("99999999999999999999")   // true
to_int("99999999999999999999")     // ParseInt overflows
```

Checking the range without converting requires comparing against
`9223372036854775807`, which requires converting. There is no `is_float` or
`is_number` at all, so `to_float` has no guard even in principle. A validate
first strategy is therefore unsound for integers and unavailable for floats.

**Handling the raised error is possible but disproportionate.** Noxy has no
`try`, `catch`, `recover`, or `rescue` keyword. The one mechanism that survives
a runtime error is `spawn_task` with `task_await`, which recovers both runtime
errors and Go panics into a status envelope. It works, but it runs the call in
a new VM on its own goroutine under shallow-copy parameter rules, returns the
value untyped, and reports the cause only as a message string that a caller
would have to pattern-match. That is a concurrency primitive, not an error
handler, and using it to convert a string to a number changes the execution
model of the expression.

The `_result` suffix is the convention the standard library already uses for
exactly this split in `io.close` / `io.close_result` and `io.write` /
`io.write_result`. Noxy functions have a single return type, so a result struct
takes the place of Go's `value, err` pair; `NetResult`, `IOResult`,
`IOWriteResult`, and `SplitResult` already establish that shape. `to_int` is
one of the few places that returns a sentinel instead.

Note the naming is the inverse of Go's, where `Atoi` returns an error and
`MustCompile` panics. Noxy's bare name raises and its `_result` form returns,
because that is the convention the standard library already created. Internal
consistency is preferred over mirroring the host language.

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

Twenty-three comment lines across three embedded standard library sources lost
their accented characters to a lossy encoding conversion, leaving a literal
`?` in place of each one: `M?dulo`, `usu?rio`, `Verifica??o`, `Convers?o`,
`Divis?o`, `s?bado`, `Diferen?a`, `Aritm?tica`, `padr?o`, `m?s`, and others.

| File | Affected lines |
|---|---|
| `internal/stdlib/http.nx` | 2 |
| `internal/stdlib/strings.nx` | 6 |
| `internal/stdlib/time.nx` | 15 |

### The fix

Each comment is restored to correct UTF-8 Portuguese. Only comments change; no
identifier, string literal, or behavior is touched.

A guard test asserts that every source embedded through `internal/stdlib/embed.go`
is valid UTF-8 and contains no U+FFFD replacement character. That guard cannot
detect the historical `?` substitution, which is indistinguishable from an
intentional question mark, but it does prevent the next lossy conversion from
landing silently.

## 6. One Registration per Native

### The defect

Two natives are registered twice in the same function:

| Native | Live registration | Dead registration |
|---|---|---|
| `strings_contains` | `builtins_strings.go:11` | `builtins_strings.go:159` |
| `strings_replace` | `builtins_strings.go:77` | `builtins_strings.go:167` |

`DefineNative` resolves to `DefineLocalIfAbsent`, so the **first** registration
wins and every later one is a silent no-op. The bodies are currently
equivalent, so nothing misbehaves today. The hazard is maintenance: a
correction applied to the second copy is silently discarded, and nothing in the
source signals that the code being edited is unreachable.

The two registration paths disagree about which duplicate wins, which makes the
hazard worse rather than merely redundant:

| Entry point | Underlying call | Winner on conflict |
|---|---|---|
| `DefineNative`, `DefineNativeWithSignature` | `DefineLocalIfAbsent` | first |
| `DefineContextualNative` | `SetGlobal` → `SetLocal` | last |

So the answer to "which definition is live?" depends on which registration
helper was used, and neither is visible at the call site.

### The fix

The dead second registration of each native is deleted, at
`builtins_strings.go:159` and `:167`. The surviving definition is the earlier
one, which is already the live one, so behavior is unchanged by construction.

A guard test asserts that no native name is registered more than once. It
parses every non-test Go source under `internal/vm/` with `go/ast` and collects
the string literal argument of each `DefineNative`,
`DefineNativeWithSignature`, `DefineContextualNative`, and
`DefineContextualNativeWithSignature` call, so duplicates spanning different
builtin files are caught too. `internal/vm/architecture_test.go` already parses
sources this way, so the mechanism is established rather than new.

## 7. `ord` Reads a Character, Not a Byte

### The defect

`ord` returns `int64(s[0])` — the first **byte** of the UTF-8 encoding. Its
inverse, `from_char_code`, is code-point based (`string(rune(code))`). The two
do not round-trip:

```noxy
from_char_code(233)   // "é"
ord("é")              // 195, the first UTF-8 byte, not 233
```

This is the defect of item 2 in the opposite direction, in a function whose
entire purpose is to name a character.

### The fix

`ord(s)` returns the Unicode code point of a single-character string. It
requires exactly one character and raises a synchronous runtime error for an
empty string or a longer one, matching Python's contract and the strictness
established in item 1. `ord` also rejects `bytes` under the item 3 rule; an
octet is read through element access on the `bytes` value itself.

`ord` is currently a global native that `strings.nx` does not export and that
no `.nx` source in the repository calls, so the change cannot break existing
code. To complete the pair, `strings.nx` exports it as `char_code(s)` alongside
the existing `from_char_code(code)`, and the two are documented as inverses:
`char_code(from_char_code(n)) == n` for every valid code point.

## Migration

Item 1 is a breaking change. It is the point of the change: an additive
`to_int_result` alone would leave every existing `to_int` call carrying the
original defect, and almost none of them would ever be revisited.

The blast radius is small and enumerable — 20 call sites in Noxy sources, 12
of `to_int` and 8 of `to_float`, of which 2 are in the standard library:

- `http_parser.nx:132`, `parse_url`, parses a URL port. A port that is absent,
  empty, or non-numeric currently becomes `0`. It moves to `to_int_result` and
  a failed parse makes the URL invalid: `valid` stays `false`.
- `http_parser.nx:233`, `parse_response`, parses a response status code. A
  malformed status line currently becomes `0` through the silent fallback. It
  moves to `to_int_result` and assigns `0` explicitly when the parse fails,
  which preserves the observable result exactly while making it a decision
  rather than an accident. `0` is chosen over leaving the constructor default
  of `200` because a malformed response must not read as a success.

The remaining 18 call sites live in `noxy_examples/`. Each is audited
individually: a call on a value that is always numeric keeps `to_int`, and a
call on parsed or external input moves to `to_int_result` with an explicit
branch. Four are known to read untrusted input and are expected to move:
`form_app.nx:123` and `todo_app.nx:196` parse a port from the environment,
`password_manager/server.nx:203` parses an identifier from a request path, and
`web_app.nx:148` parses an age from decoded JSON. The 160-example suite is the
regression gate for the migration.

`CHANGELOG.md` records item 1 as breaking and states the migration recipe:
a `to_int` or `to_float` call that may receive non-numeric input becomes the
`_result` form with an explicit failure branch.

This change also repairs a defect in the queued `feat/http-streaming-server`
plan. Its `resolve_body_length` gated every `Content-Length` conversion behind
`is_digit(raw) && length(raw) <= 19`, purely because `to_int` could not report
failure. That guard is wrong: `int64` holds up to `9223372036854775807`, which
is 19 digits, so `"9999999999999999999"` passes the guard and overflows. With
today's `to_int` it becomes `0`, the server frames the request as bodyless, and
the client's remaining bytes are read as the start of another request — a
framing desynchronization in exactly the code path that subproject exists to
harden.

With `to_int_result` the gate becomes a direct parse that reports overflow
honestly, and `is_digit` remains only where the HTTP specification requires a
rule stricter than "a valid integer": no sign and no leading `+`. The HTTP plan
is updated accordingly before it is executed.

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
- Deleting the dead duplicate registrations changes nothing observable; the
  surviving definition is the first one, which already won under
  `DefineLocalIfAbsent`.
- `ord` changes behavior for non-ASCII input and now raises for an empty or
  multi-character argument. It has no caller in the repository and is not
  exported by any module, so no existing code is affected.
- `char_code` is new and requires `use strings`.
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
  output beyond what the caller printed;
- no native name is registered more than once across the whole builtin
  surface, and `strings_contains` and `strings_replace` keep their current
  behavior after the duplicates are removed;
- `ord` returns the code point of a single-character string, including
  multi-byte characters, and round-trips with `from_char_code` across the ASCII
  range, the Latin-1 supplement, and an emoji;
- `ord` raises for an empty string, for a multi-character string, and for a
  `bytes` argument;
- `char_code` is exported by `strings` and agrees with `ord`.

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

Two language-level subprojects are candidates that this design deliberately
does not require:

- **A structured error-catching construct.** The unwind machine from
  `feat/runtime-defer-unwind` already exists, so catching is largely a matter
  of stopping the unwind at a marker and defining how that interacts with
  deferred-failure aggregation. It would make the strict forms usable directly
  for untrusted input. Even then the `_result` family stays valuable, because
  an expected data failure and an unexpected program bug deserve different
  handling — the same reason Go carries both `error` and `panic`.
- **Multiple return values.** Noxy's single `ReturnType` is why the standard
  library encodes fallible operations as result structs. Tuple returns would
  allow Go's `value, err` shape directly. This is a parser, compiler, and VM
  change, and the result-struct convention works without it.
