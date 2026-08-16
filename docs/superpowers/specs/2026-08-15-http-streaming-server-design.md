# HTTP Streaming Server Design

## Goal and Scope

Resolve point 13 from PR #17: the HTTP server reads exactly one socket chunk
and parses whatever happens to arrive. This subproject replaces that single
read with incremental framing that reads headers and body to completion,
enforces explicit limits, bounds every phase with a deadline, and answers
malformed requests with real HTTP responses.

The work depends on `feat/network-deadlines`, which supplies the per-operation
timeout the server needs to bound slow or stalled clients.

Scope covers the server framing path only. Keep-alive, request pipelining,
chunked `Transfer-Encoding`, `Expect: 100-continue`, HTTP/2, TLS, and the
`http_client` response loop are excluded. The server continues to serve one
request per connection and closes afterwards.

## Runtime Foundations Assumed

`develop` has since landed two invariants that change what a parser is allowed
to do with attacker-controlled bytes. Both are enforced by the runtime, not by
convention, so the framing path is designed around them rather than in spite of
them.

**Every Noxy string holds valid UTF-8.** `to_str` is the single choke point at
the bytes-to-text boundary and *raises a runtime error* when the payload is not
valid UTF-8. The inverse guard applies too: every text native
(`split`, `index_of`, `starts_with`, `contains`, `trim`, `to_lower`) rejects a
`bytes` argument outright. There is therefore no way to run a string operation
over raw request bytes — the only route from `bytes` to `string` is `to_str`,
and it validates.

This is load-bearing for a network parser. A client may send any byte it likes,
so `to_str(header_bytes)` on a header block containing `0xFF` raises inside the
spawned connection routine. A raised error there does not merely skip the
response: the routine dies, the client receives nothing, and the VM prints
`Thread Error: ...` to the server's stdout — a remotely triggerable log flood.
The framing path therefore gates the boundary explicitly with
`is_valid_utf8(header_bytes)`, from `strings`, and rejects a non-UTF-8 header
block with `400` before any conversion is attempted.

**Numeric conversion raises instead of guessing.** `to_int` now parses through
`strconv.ParseInt` and raises on anything it cannot represent. It accepts a
leading `+` or `-`, rejects `" 5"`, `"5.5"`, and `"0x10"`, and raises
`out of range` for a value above `int64`. The `_result` forms
(`convert_to_int_result`, exposed as `to_int_result` in `convert`) return an
`ok`/`value`/`error` record instead and never raise.

`Content-Length` is attacker-controlled, so the framing path never calls
`to_int` on it. `is_digit` gates the value first — it is ASCII-only and rejects
the empty string, which removes `+5`, `-5`, `5.5`, `0x10`, and `5, 5` — and the
conversion itself goes through `convert_to_int_result` so that a 19-digit value
above `int64` is rejected with `400` rather than raising. `parse_url` in
`http_parser.nx` already uses this native, so the pattern is established.

Two consequences follow for the rest of this design. The "byte-oriented versus
rune-oriented helper" rule is retained, but its justification changes: it is no
longer what protects framing from non-UTF-8 input — the boundary gate is — it
is what keeps byte offsets and rune offsets from being mixed inside text that
is already known to be valid. And the stdlib hygiene suite added alongside
these invariants asserts that every embedded `.nx` source is valid UTF-8, holds
no `U+FFFD`, and ships no debug marker, so the new stdlib code is checked
against those rules as part of ordinary validation.

## Implementation Layer

The whole HTTP stack is written in Noxy, and every primitive the framing loop
needs already exists: `socket_recv`, `socket_send`, `settimeout`,
`time_now_ms`, `slice`, and `defer`. The implementation therefore stays in
Noxy and introduces no new native builtin. This keeps `http_server.nx` and
`http_client.nx` at the same layer and keeps the VM surface unchanged.

Responsibilities split across three layers:

| Layer | File | Responsibility |
|---|---|---|
| Framing primitives | `http_parser.nx` | resumable terminator scan, request-line and header validation, `Content-Length` resolution |
| Connection I/O | `http_server.nx` | incremental read loop, deadlines, limits, complete writes |
| Policy | `http_server.nx` | one request per connection, `Connection: close`, error responses |

Framing primitives perform no I/O, so they are decided by their inputs alone.
The I/O loop never re-implements parsing.

## Public API

`HttpServer` gains six configuration fields:

```noxy
struct HttpServer
    host: string
    port: int
    listener: Socket
    running: bool
    max_header_bytes: int
    max_body_bytes: int
    header_timeout_ms: int
    body_timeout_ms: int
    write_timeout_ms: int
    read_chunk_bytes: int
end
```

`new_server(host, port)` keeps its two-argument form and installs defaults:

| Field | Default | Meaning |
|---|---|---|
| `max_header_bytes` | `16384` | maximum bytes of the header block, terminator included |
| `max_body_bytes` | `1048576` | maximum declared `Content-Length` |
| `header_timeout_ms` | `5000` | total wall-clock budget for the header block |
| `body_timeout_ms` | `15000` | total wall-clock budget for the body |
| `write_timeout_ms` | `15000` | total wall-clock budget for one response |
| `read_chunk_bytes` | `8192` | receive size per `socket_recv` call |

Configuration is field assignment on the server before `serve`:

```noxy
let server: HttpServer = new_server("127.0.0.1", 8080)
server.max_body_bytes = 8388608
serve(server, handler)
```

A new function separates binding from the accept loop:

```noxy
func bind_server(server: ref HttpServer) -> bool
```

`bind_server` creates the listener, stores it in `server.listener`, writes the
actual bound port back to `server.port`, sets `server.running = true`, and
returns whether binding succeeded. Writing the bound port back makes port `0`
usable: the caller reads the assigned ephemeral port after binding.

`serve(server: ref HttpServer, handler: func) -> void` keeps its signature. It
calls `bind_server` when `server.listener.open` is false and otherwise reuses
the existing listener, so both the old one-call form and the new
bind-then-serve form work.

Every other exported name — `stop_server`, `response_ok`, `response_text`,
`response_json`, `response_html`, `response_error`, `response_404`,
`response_500`, `serve_static` — keeps its current signature and behavior.

## Configuration Validation

`serve` normalizes configuration once, before accepting, and never mid-request:
a non-positive `max_header_bytes`, `max_body_bytes`, `header_timeout_ms`,
`body_timeout_ms`, `write_timeout_ms`, or `read_chunk_bytes` is replaced by its
default. `read_chunk_bytes` is additionally clamped to `max_header_bytes` when
it exceeds it. The normalized values are copied into an `HttpLimits` value that
is passed to each spawned connection routine, so a later mutation of the server
struct cannot change the rules applied to a request already in flight.

```noxy
struct HttpLimits
    max_header_bytes: int
    max_body_bytes: int
    header_timeout_ms: int
    body_timeout_ms: int
    write_timeout_ms: int
    read_chunk_bytes: int
end
```

## Framing Result

Framing returns one value that either carries a request or a rejection:

```noxy
struct HttpFrameResult
    ok: bool
    request: HttpRequest
    status: int
    message: string
    respond: bool
end
```

`ok` true means `request` is complete and `status` is `0`. `ok` false means
`status` holds the HTTP status to return and `message` holds the plain-text
reason used as both the response body and the log line. `respond` is false only
when no response can be meaningfully sent — the client closed the connection
before any request byte arrived, or the connection failed at the transport
level. In that case the server closes silently.

## Read Algorithm

### Phase 1: header block

The loop accumulates received bytes and searches for the `\r\n\r\n`
terminator after each receive.

```text
buffer = b""
total = 0
scanned = 0
deadline = time_now_ms() + header_timeout_ms

loop:
    terminator = find_header_end_from(buffer, total, scanned)
    if terminator >= 0: break
    if total >= max_header_bytes: reject 431
    remaining = deadline - time_now_ms()
    if remaining <= 0: reject 408
    settimeout(client, remaining)
    result = socket_recv(client, read_chunk_bytes)
    if not result.ok: reject 408 on timeout, otherwise close silently
    if result.count == 0:
        if total == 0: close silently
        reject 400
    scanned = max(0, total - 3)
    buffer = buffer + slice(result.data, 0, result.count)
    total = total + result.count
```

`find_header_end_from(data, len, start)` is a new `http_parser.nx` primitive
that resumes the scan at `start` instead of index `0`. Setting `scanned` to
`total - 3` before appending re-examines only the three bytes that could form a
split terminator, so total scanning cost is linear in the header size rather
than quadratic in the number of segments. `find_header_end(data, len)` remains
exported and is defined as `find_header_end_from(data, len, 0)`.

`remaining` is recomputed from an absolute deadline on every iteration and
checked before `settimeout`. This is required for two reasons: it is the
slowloris defense, because a client that trickles one byte at a time still hits
the total budget; and `net_settimeout` raises a synchronous runtime error for a
non-positive timeout, so a non-positive `remaining` must reject before the call
rather than through it.

After the terminator is found, `terminator + 4 > max_header_bytes` rejects
with 431. The header block is `slice(buffer, 0, terminator)` and the first body
bytes already held are `slice(buffer, terminator + 4, total)`.

A `socket_recv` failure is classified by its `error` field: the normalized
`operation timed out` string from `feat/network-deadlines` rejects with 408;
any other failure closes silently, because the transport is already unusable.

### Phase 2: request line and headers

`parse_request_head(header_bytes) -> HttpFrameResult` validates strictly.

Its first act is the UTF-8 gate. `is_valid_utf8(header_bytes)` is checked
before any conversion, and a header block that fails it is rejected with 400.
Without this check `to_str` raises and the connection routine dies with no
response, so the gate is what turns a hostile byte into an ordinary rejection.
It is also the point that makes every later rule safe: past it the header block
is known to be valid UTF-8, so rune-based validation of ASCII-constrained
fields cannot decode a byte the client did not write.

After the gate it splits the header block on `\r\n`; a lone `\n` is not
accepted as a line terminator.

Request line rules:

- exactly three space-separated tokens, so a target containing a raw space is
  rejected with 400;
- the method is a non-empty RFC 9110 token of at most 64 characters: every
  character is in the visible ASCII token set `!#$%&'*+-.^_`|~`, a digit, or a
  letter. A violation is 400;
- the target is non-empty and begins with `/`. Absolute-form and authority-form
  targets are rejected with 400, which matches an origin server that terminates
  its own connections;
- the target is at most 2048 characters, else 414;
- every target character is visible ASCII in `0x21`–`0x7E`, so a space or a
  control character is 400;
- the version is exactly `HTTP/1.0` or `HTTP/1.1`. Any other syntactically
  well-formed version is 505; a malformed version token is 400.

Header line rules:

- a line beginning with space or horizontal tab is obsolete line folding and is
  rejected with 400 rather than silently joined;
- a line without `:` is 400;
- the name is the text before the first `:`. An empty name, a name longer than
  256 characters, a name containing whitespace, or any whitespace between the
  name and the `:` is 400;
- the value is everything after the first `:`, with leading and trailing spaces
  and tabs removed. A value may contain `:`;
- no header line may carry a control character. Every character must be
  horizontal tab, or `0x20` and above excluding `0x7F`; `0x00`–`0x08`,
  `0x0A`–`0x1F`, and `0x7F` are rejected with 400. Characters above `0x7F` are
  permitted, matching RFC 9110 `obs-text`, and are already known to be
  well-formed because the UTF-8 gate ran first;
- more than 64 headers is 431. The current implementation silently drops
  headers past 64, which can drop `Content-Length` itself and desynchronize
  framing, so silent truncation is replaced by explicit rejection.

The control-character rule is the response-splitting defense, and it is the one
rule the UTF-8 invariant does not supply. `CR`, `LF`, and `NUL` are all valid
UTF-8, so the boundary gate passes them through unchanged. The header block is
split on `\r\n`, which means a lone `\n`, or a `\r` not followed by `\n`,
survives inside a header value. `build_response` assembles response headers by
string concatenation with no validation of its own, so a handler that echoes a
received value into a response header — a routine thing to write — would let a
client inject `\r\n` and forge an entire second response.

Validating on the way in fixes this once, at the point where the bytes are
already being inspected, rather than depending on every handler author to
sanitize on the way out. The check runs against the whole header line rather
than the extracted value: the name is already constrained to a token and `:` is
a visible character, so scanning the line is equivalent to scanning the value
and costs one pass instead of two.

The parsed `HttpRequest` keeps its existing shape: `headers` holds the raw
`Name: value` lines, `header_count` their number, and `query` the substring
after the first `?` in the target.

### Phase 3: body length

`resolve_body_length(headers, count, max_body_bytes) -> BodyLengthResult`
determines how many body bytes to read:

```noxy
struct BodyLengthResult
    ok: bool
    length: int
    status: int
    message: string
end
```

`ok` true means `length` is authoritative and `status` is `0`. `ok` false means
`status` and `message` describe the rejection and `length` is `0`.

- any `Transfer-Encoding` header rejects with 501, because chunked decoding is
  out of scope; the server must not guess a length it cannot compute;
- `Transfer-Encoding` together with `Content-Length` rejects with 400, since
  the combination is a request-smuggling vector;
- two or more `Content-Length` headers reject with 400, including headers whose
  values are identical;
- a `Content-Length` value must be 1 to 19 characters, every character an ASCII
  digit. `to_int` accepts a leading `+` or `-` and raises on `" 5"`, `"5.5"`,
  and `"0x10"`, so the digit check runs before any conversion and a value
  failing it is 400. A comma-separated list such as `5, 5` fails the same check;
- conversion goes through `convert_to_int_result`, not `to_int`. `is_digit`
  plus the 19-character bound still admits a value above `int64` —
  `9999999999999999999` is nineteen digits and overflows — and `to_int` would
  raise on it, killing the connection routine instead of answering. The
  `_result` form reports the failure as data, and a value it cannot convert is
  rejected with 400;
- a value greater than `max_body_bytes` rejects with 413;
- no `Content-Length` and no `Transfer-Encoding` means a body length of zero,
  for every method including `POST`. This follows RFC 9112: a request without
  either header has no body. The server does not return 411.

### Phase 4: body

The body loop reuses the bytes already buffered past the header terminator and
reads until it holds exactly `content_length` bytes, under an absolute deadline
of `body_timeout_ms` computed when the phase starts. Its per-iteration
`remaining` guard, `settimeout` call, and error classification match phase 1.

- EOF before the declared length rejects with 400;
- a deadline expiry rejects with 408;
- surplus bytes beyond `content_length` are discarded rather than treated as an
  error. They are pipelined data, and the connection closes after one response;
- `content_length == 0` performs no read at all.

## Write Path

`send_all(client, data, write_timeout_ms) -> bool` writes a complete response.

```text
offset = 0
deadline = time_now_ms() + write_timeout_ms
while offset < length(data):
    remaining = deadline - time_now_ms()
    if remaining <= 0: return false
    settimeout(client, remaining)
    result = socket_send(client, slice(data, offset, length(data)))
    offset = offset + result.count
    if not result.ok and result.count == 0: return false
return true
```

The offset advances by `result.count` even when `ok` is false, because
`net_send` returns the actual transferred count alongside a failure. An
iteration that transfers nothing and fails aborts; an iteration that transfers
part of the buffer retries from the correct offset until the total write
deadline expires. `length(data)` on a `bytes` value is a byte count, so the
offset arithmetic is byte-exact.

## Connection Lifecycle

```text
handle_client_connection(client, handler, limits):
    defer socket_close(client)
    frame = read_request(client, limits)
    if not frame.ok:
        if frame.respond: send_all(error_response(frame), limits.write_timeout_ms)
        return
    response = handler(frame.request)
    send_all(build_response(response), limits.write_timeout_ms)
```

`defer socket_close(client)` replaces the current explicit close. The existing
code closes only on the success path, so a runtime error raised by the user
handler terminates the routine and leaks the socket until process exit. With
`defer`, the socket closes on every exit path including a handler failure.

The accept loop is unchanged apart from binding: it accepts, and spawns one
routine per connection carrying the client socket, the handler, and the
normalized limits.

## Error Responses

A rejected request receives a real response instead of a bare disconnect. Every
error response carries `Content-Type: text/plain`, a byte-exact
`Content-Length`, `Connection: close`, and the reason as its body.

| Status | Condition |
|---|---|
| 400 Bad Request | header block that is not valid UTF-8, control character in a header line, malformed request line, malformed header, obsolete folding, whitespace before `:`, non-origin target, duplicate `Content-Length`, non-digit or unconvertible `Content-Length`, `Transfer-Encoding` with `Content-Length`, EOF mid-request |
| 408 Request Timeout | header or body deadline expired, including a stalled trickle |
| 413 Content Too Large | declared `Content-Length` above `max_body_bytes` |
| 414 URI Too Long | request target longer than 2048 characters |
| 431 Request Header Fields Too Large | header block at or above `max_header_bytes`, or more than 64 header lines |
| 501 Not Implemented | `Transfer-Encoding` present |
| 505 HTTP Version Not Supported | version other than `HTTP/1.0` or `HTTP/1.1` |

`get_status_text` gains 408, 413, 414, 431, 501, and 505.

`response_error` currently computes `Content-Length` as `length(msg)` where
`msg` is a string, which is a rune count, while the body is `to_bytes(msg)`,
a byte count. A non-ASCII message therefore declares a length shorter than the
bytes it sends and desynchronizes the client. The length is computed from the
encoded bytes instead. `response_ok` already measures `bytes` and is correct.

## Corrected Header Lookup

`get_header` splits the raw line on every `:` and returns `parts[1]`, so
`Host: example.com:8080` returns `example.com` and any value containing a colon
is truncated. Framing depends on reading `Content-Length` and
`Transfer-Encoding` exactly, so the lookup is rewritten to split at the first
`:` only, using `index_of`, and to trim only spaces and tabs from the value.
`get_header` keeps its signature and its empty-string result for a missing
header. A new `count_header(headers, count, name) -> int` returns how many
lines carry a given name, which is what duplicate detection needs.

Value trimming uses the existing `strings_trim` native, whose Go `TrimSpace`
semantics operate on the raw string. Structural scanning likewise uses the
byte-oriented `split`, `index_of`, and `starts_with` natives rather than the
rune-oriented `substring` and `char_at`.

That split matters, but not for the reason a pre-UTF-8-invariant design would
give. A header carrying non-UTF-8 bytes cannot reach this code at all: the
boundary gate in `parse_request_head` rejects it with 400, and every text
native refuses a `bytes` argument outright. What the rule prevents is the
subtler error of mixing offset spaces inside text that is already valid —
`index_of` returns a byte offset while `substring` indexes runes, so feeding
one to the other silently mis-slices any header value containing a multi-byte
character. Rune-based helpers are therefore confined to validating
ASCII-constrained fields, where any non-ASCII rune fails validation and the
request is rejected before an offset is ever derived from it.

## Testing

Two layers, matching the project's existing practice of Go unit tests plus a
Noxy integration script.

### Go tests driving the Noxy VM

`internal/vm/http_server_framing_test.go` runs the embedded Noxy stdlib inside
the VM and speaks raw TCP against it, which gives byte-level control over
timing and fragmentation.

The harness works as follows: the test defines natives on the VM; the Noxy
script calls `bind_server` on `127.0.0.1:0`, reports the bound port through a
test native, spawns `serve`, blocks on a second test native until the test
releases it, and then calls `stop_server`. The test dials the reported port
with `net.Dial` and controls exactly which bytes are written and when. Every
goroutine has a bounded harness wait.

The matrix proves:

- a request delivered one byte at a time is framed correctly;
- a request split inside the `\r\n\r\n` terminator is framed correctly;
- a body split across several segments is delivered whole to the handler;
- a body larger than one `read_chunk_bytes` is delivered whole;
- a request with no `Content-Length` yields an empty body, including `POST`;
- surplus bytes after `Content-Length` do not corrupt the response;
- a header block exceeding `max_header_bytes` returns 431;
- more than 64 header lines returns 431;
- a declared `Content-Length` above `max_body_bytes` returns 413;
- a client that stops mid-header returns 408 within the configured budget;
- a client that stops mid-body returns 408 within the configured budget;
- a connection opened and closed with no bytes produces no response and no
  leaked socket;
- EOF mid-header and EOF mid-body return 400;
- duplicate `Content-Length` headers return 400, including identical values;
- `Content-Length: +5`, `5.5`, `5, 5`, and an empty value return 400;
- `Content-Length: 9999999999999999999` — nineteen digits, above `int64` —
  returns 400 rather than raising out of the connection routine;
- a header block carrying an invalid UTF-8 byte returns 400, and the server
  survives to answer the next connection;
- a header value carrying a bare `LF`, a bare `CR`, a `NUL`, or `DEL` returns
  400, so a handler echoing that value cannot be made to split the response;
- a header value carrying a horizontal tab or a non-ASCII character is
  accepted, so the rule rejects control characters without rejecting
  legitimate text;
- `Transfer-Encoding: chunked` returns 501, and combined with `Content-Length`
  returns 400;
- `HTTP/0.9` and `HTTP/2.0` return 505, while `HTTP/1.0` and `HTTP/1.1` are
  accepted;
- obsolete line folding, whitespace before `:`, a header without `:`, a target
  containing a space, and a non-origin target return 400;
- a target longer than 2048 characters returns 414, and a method longer than 64
  characters or a header name longer than 256 characters returns 400;
- every error response carries a byte-exact `Content-Length` and
  `Connection: close`;
- a large response to a slow-reading client is written completely, exercising
  the partial-write loop;
- a handler that raises a runtime error closes the connection rather than
  leaking the socket;
- `get_header` returns `example.com:8080` for `Host: example.com:8080`;
- `response_error` with a non-ASCII message declares its byte length;
- `bind_server` on port `0` writes the real bound port back to `server.port`;
- `serve` on an already-bound server reuses the listener instead of rebinding;
- non-positive configuration fields fall back to their defaults.

Framing primitives that are pure functions — `find_header_end_from`,
`parse_request_head`, `resolve_body_length`, `count_header` — are additionally
exercised directly through the Noxy source harness, so a framing regression is
attributable without a socket.

### Noxy integration test

`noxy_examples/test_http_server.nx` binds an ephemeral port, spawns the server,
and drives it with `http_client`: a `GET` returning text and a `POST` with a
JSON body echoed back. It asserts the responses, stops the server, and exits.
It runs as part of the ordinary example suite rather than being excluded.

### Project validation

```text
go build ./...
go vet ./...
go test ./internal/...
go test -race ./internal/vm/...
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

`go test ./internal/...` covers the stdlib hygiene suite that landed with the
UTF-8 invariant — `TestEmbeddedStdlibSourcesAreValidUTF8`,
`TestNoShippedDebugOutput`, and `TestEveryNativeIsRegisteredExactlyOnce` — so
the new stdlib code is held to those rules without a separate step.

## Compatibility

- `new_server(host, port)` keeps its arity and return type; the new fields are
  populated with defaults.
- `serve(server, handler)` keeps its signature and still binds when needed, so
  every existing script runs unchanged.
- `stop_server` and all `response_*` helpers keep their signatures.
- `HttpRequest` and `HttpResponse` keep their shapes.
- `find_header_end`, `parse_request`, `parse_response`, `build_request`,
  `build_response`, and `get_header` remain exported with their current
  signatures. `parse_request` is retained for callers that already hold a
  complete buffer.
- Responses continue to carry `Connection: close`, and the server continues to
  serve one request per connection.
- Behavior changes deliberately for requests that the previous implementation
  mis-framed: a truncated request that used to reach the handler with partial
  data now receives 400 or 408, and a request that used to have headers past 64
  silently dropped now receives 431.
- `parse_request` is retained but gains the same UTF-8 gate, returning its
  default `HttpRequest` for a non-UTF-8 buffer instead of raising. Its
  signature is unchanged and the server no longer calls it.

One related defect is deliberately left out of scope. `parse_response` also
converts header bytes through `to_str`, so a peer that replies with a non-UTF-8
header block now raises inside `http_client` rather than returning a response.
That is inherited from the UTF-8 invariant landing on `develop`, it affects the
client rather than the server framing path this subproject covers, and fixing
it means deciding what a `ClientResponse` reports for an unparseable reply.
It is recorded as follow-up work rather than folded in here.

## Documentation

`docs/HTTP_SERVER.md` documents the framing contract, the configuration fields
and their defaults, the status codes and the condition each one signals, the
one-request-per-connection policy, and the explicit exclusions. `CHANGELOG.md`
records the fix under point 13.
