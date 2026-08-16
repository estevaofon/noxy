# HTTP Streaming Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the HTTP server's single 64 KB socket read with incremental framing that reads headers and body to completion, enforces limits and deadlines, writes responses completely, and answers invalid requests with real HTTP responses.

**Architecture:** Three layers, all in Noxy. `internal/stdlib/http_parser.nx` gains pure framing primitives that perform no I/O. `internal/stdlib/http_server.nx` gains the incremental read loop, deadline handling, and a complete-write loop. Policy stays where it is: one request per connection, always `Connection: close`. No new Go native is introduced; the primitives needed (`socket_recv`, `socket_send`, `settimeout`, `time_now_ms`, `slice`, `defer`) already exist.

**Tech Stack:** Noxy stdlib (`.nx`, embedded via `internal/stdlib/embed.go`), Go 1.x test harness in `internal/vm` that runs Noxy source on the VM and speaks raw TCP against it.

**Spec:** `docs/superpowers/specs/2026-08-15-http-streaming-server-design.md`

## Global Constraints

- Branch is `feat/http-streaming-server`, already created off `develop`.
- No new Go native builtin. All behavior changes live in `internal/stdlib/*.nx`.
- Existing exported names keep their signatures: `new_server(host, port)`, `serve(server, handler)`, `stop_server`, `response_ok`, `response_text`, `response_json`, `response_html`, `response_error`, `response_404`, `response_500`, `serve_static`, `find_header_end`, `parse_request`, `parse_response`, `build_request`, `build_response`, `get_header`.
- Noxy has no `continue` keyword. Use `if`/`else` structure inside loops.
- Noxy has no `min`/`max` builtin. Clamp with explicit `if`.
- A top-level variable that a function reassigns must be declared with `global`, not `let`.
- Noxy string literals support only the escapes `\n`, `\r`, `\t`, `\"`, `\'`, and `\\`. There is no `\u` escape; build a non-ASCII character with `from_char_code(code)`.
- `sleep` is not a global native. A script that calls it needs `use time select *`.
- `repeat` comes from `strings`. A script that calls it needs `use strings select *`.
- `time_now_ms()` **is** a global native and needs no import.
- `length(s)` on a `string` is a rune count; on `bytes` it is a byte count. All offset arithmetic in the read and write loops operates on `bytes`.
- `split`, `index_of`, `starts_with`, `contains`, and `trim` are byte-based; `substring`, `char_at`, and `length(string)` are rune-based. Structural framing decisions use only the byte-based natives.
- `to_int` accepts `"+5"`, `"5.5"`, and `" 5"`. Never call it on a `Content-Length` value before `is_digit` has passed.
- `net_settimeout` raises a synchronous runtime error for a non-positive timeout. Always check `remaining <= 0` and reject before calling `settimeout`.
- Defaults: `max_header_bytes` 16384, `max_body_bytes` 1048576, `header_timeout_ms` 5000, `body_timeout_ms` 15000, `write_timeout_ms` 15000, `read_chunk_bytes` 8192.
- Fixed bounds: method at most 64 characters, header name at most 256 characters, request target at most 2048 characters, at most 64 header lines.
- Commit after every task. Commit messages end with:
  `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`

## File Structure

| File | Action | Responsibility |
|---|---|---|
| `internal/stdlib/http_parser.nx` | Modify | framing primitives: resumable terminator scan, token/ASCII validation, strict head parsing, `Content-Length` resolution, corrected header lookup, extended status text |
| `internal/stdlib/http_server.nx` | Modify | server configuration, binding, incremental read loop, complete-write loop, connection lifecycle |
| `internal/vm/http_parser_framing_test.go` | Create | Go tests exercising the pure framing primitives through the Noxy source harness |
| `internal/vm/http_server_framing_test.go` | Create | Go tests running the Noxy server on the VM and driving it over raw TCP |
| `noxy_examples/test_http_server.nx` | Create | Noxy end-to-end test using `http_client` against the server |
| `docs/HTTP_SERVER.md` | Create | user documentation of framing contract, limits, timeouts, status codes |
| `CHANGELOG.md` | Modify | record the point 13 fix |

---

### Task 1: Framing primitives in `http_parser.nx`

Resumable terminator scan, ASCII/token validation helpers, corrected `get_header`, new `count_header`, extended `get_status_text`, and the two result structs the later tasks return.

**Files:**
- Modify: `internal/stdlib/http_parser.nx`
- Test: `internal/vm/http_parser_framing_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `struct HttpFrameResult` with fields `ok: bool`, `request: HttpRequest`, `status: int`, `message: string`, `respond: bool`
  - `struct BodyLengthResult` with fields `ok: bool`, `length: int`, `status: int`, `message: string`
  - `func find_header_end_from(data: bytes, len: int, start: int) -> int`
  - `func find_header_end(data: bytes, len: int) -> int`
  - `func is_token(s: string) -> bool`
  - `func is_visible_ascii(s: string) -> bool`
  - `func count_header(headers: string[64], count: int, name: string) -> int`
  - `func get_header(headers: string[64], count: int, name: string) -> string` (corrected)
  - `func frame_error(status: int, message: string) -> HttpFrameResult`
  - `func frame_silent() -> HttpFrameResult`
  - `func get_status_text(code: int) -> string` (extended)

- [ ] **Step 1: Write the failing test**

Create `internal/vm/http_parser_framing_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func captureParserInt(t *testing.T, body string) int64 {
	t.Helper()
	captured := captureVMSource(t, "use http_parser select *\n"+body)
	if captured.Type != value.VAL_INT {
		t.Fatalf("test_report value = %#v, want int", captured)
	}
	return captured.AsInt
}

func captureParserString(t *testing.T, body string) string {
	t.Helper()
	captured := captureVMSource(t, "use http_parser select *\n"+body)
	if _, ok := captured.Obj.(string); !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	return captured.Obj.(string)
}

func captureParserBool(t *testing.T, body string) bool {
	t.Helper()
	captured := captureVMSource(t, "use http_parser select *\n"+body)
	if captured.Type != value.VAL_BOOL {
		t.Fatalf("test_report value = %#v, want bool", captured)
	}
	return captured.AsBool
}

func TestFindHeaderEndFromResumesScan(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   int64
	}{
		{
			name:   "terminator at start of resumed window",
			source: "let data: bytes = to_bytes(\"AB\\r\\n\\r\\nC\")\ntest_report(find_header_end_from(data, length(data), 0))",
			want:   2,
		},
		{
			name:   "resume point before split terminator still finds it",
			source: "let data: bytes = to_bytes(\"ABC\\r\\n\\r\\nD\")\ntest_report(find_header_end_from(data, length(data), 0))",
			want:   3,
		},
		{
			name:   "resume point after terminator misses it",
			source: "let data: bytes = to_bytes(\"AB\\r\\n\\r\\nC\")\ntest_report(find_header_end_from(data, length(data), 3))",
			want:   -1,
		},
		{
			name:   "negative resume point is clamped to zero",
			source: "let data: bytes = to_bytes(\"AB\\r\\n\\r\\nC\")\ntest_report(find_header_end_from(data, length(data), -5))",
			want:   2,
		},
		{
			name:   "no terminator returns minus one",
			source: "let data: bytes = to_bytes(\"AB\\r\\nCD\")\ntest_report(find_header_end_from(data, length(data), 0))",
			want:   -1,
		},
		{
			name:   "legacy wrapper scans from zero",
			source: "let data: bytes = to_bytes(\"AB\\r\\n\\r\\nC\")\ntest_report(find_header_end(data, length(data)))",
			want:   2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := captureParserInt(t, test.source); got != test.want {
				t.Fatalf("find_header_end_from = %d, want %d", got, test.want)
			}
		})
	}
}

func TestIsTokenAndVisibleAscii(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "method token", source: "test_report(is_token(\"GET\"))", want: true},
		{name: "token punctuation", source: "test_report(is_token(\"X-Custom_Name!\"))", want: true},
		{name: "empty is not a token", source: "test_report(is_token(\"\"))", want: false},
		{name: "space is not a token", source: "test_report(is_token(\"Bad Name\"))", want: false},
		{name: "trailing space is not a token", source: "test_report(is_token(\"Name \"))", want: false},
		{name: "colon is not a token", source: "test_report(is_token(\"Na:me\"))", want: false},
		{name: "non ascii is not a token", source: "test_report(is_token(\"Nome\" + from_char_code(231)))", want: false},
		{name: "origin target is visible ascii", source: "test_report(is_visible_ascii(\"/a/b?c=1\"))", want: true},
		{name: "space is not visible ascii", source: "test_report(is_visible_ascii(\"/a b\"))", want: false},
		{name: "tab is not visible ascii", source: "test_report(is_visible_ascii(\"/a\\tb\"))", want: false},
		{name: "empty is not visible ascii", source: "test_report(is_visible_ascii(\"\"))", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := captureParserBool(t, test.source); got != test.want {
				t.Fatalf("result = %v, want %v", got, test.want)
			}
		})
	}
}

func TestGetHeaderPreservesColonsInValue(t *testing.T) {
	source := `let headers: string[64]
headers[0] = "Host: example.com:8080"
headers[1] = "Content-Length: 12"
test_report(get_header(headers, 2, "host"))`
	if got := captureParserString(t, source); got != "example.com:8080" {
		t.Fatalf("get_header = %q, want %q", got, "example.com:8080")
	}
}

func TestGetHeaderMissingReturnsEmpty(t *testing.T) {
	source := `let headers: string[64]
headers[0] = "Host: example.com"
test_report(get_header(headers, 1, "Content-Length"))`
	if got := captureParserString(t, source); got != "" {
		t.Fatalf("get_header = %q, want empty", got)
	}
}

func TestCountHeaderCountsCaseInsensitively(t *testing.T) {
	source := `let headers: string[64]
headers[0] = "Content-Length: 5"
headers[1] = "content-length: 5"
headers[2] = "Host: a"
test_report(count_header(headers, 3, "Content-Length"))`
	if got := captureParserInt(t, source); got != 2 {
		t.Fatalf("count_header = %d, want 2", got)
	}
}

func TestGetStatusTextCoversFramingCodes(t *testing.T) {
	tests := map[string]string{
		"408": "Request Timeout",
		"413": "Content Too Large",
		"414": "URI Too Long",
		"431": "Request Header Fields Too Large",
		"501": "Not Implemented",
		"505": "HTTP Version Not Supported",
	}
	for code, want := range tests {
		t.Run(code, func(t *testing.T) {
			if got := captureParserString(t, "test_report(get_status_text("+code+"))"); got != want {
				t.Fatalf("get_status_text(%s) = %q, want %q", code, got, want)
			}
		})
	}
}

func TestFrameHelpersCarryRespondFlag(t *testing.T) {
	if got := captureParserInt(t, "let f: HttpFrameResult = frame_error(431, \"x\")\ntest_report(f.status)"); got != 431 {
		t.Fatalf("frame_error status = %d, want 431", got)
	}
	if got := captureParserBool(t, "let f: HttpFrameResult = frame_error(431, \"x\")\ntest_report(f.respond)"); !got {
		t.Fatal("frame_error respond = false, want true")
	}
	if got := captureParserBool(t, "let f: HttpFrameResult = frame_silent()\ntest_report(f.respond)"); got {
		t.Fatal("frame_silent respond = true, want false")
	}
	if got := captureParserBool(t, "let f: HttpFrameResult = frame_silent()\ntest_report(f.ok)"); got {
		t.Fatal("frame_silent ok = true, want false")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run 'TestFindHeaderEndFrom|TestIsToken|TestGetHeader|TestCountHeader|TestGetStatusText|TestFrameHelpers' -v`

Expected: FAIL — the VM reports undefined variables such as `find_header_end_from`, `is_token`, `count_header`, `frame_error`, and `frame_silent`.

- [ ] **Step 3: Add the structs and constants to `http_parser.nx`**

Insert immediately after the `HttpUrl` struct (currently ending at line 35), before the `Constants` banner:

```noxy
struct HttpFrameResult
    ok: bool
    request: HttpRequest
    status: int
    message: string
    respond: bool
end

struct BodyLengthResult
    ok: bool
    length: int
    status: int
    message: string
end
```

Then, in the `Constants` section right after `let HTTP_500_INTERNAL_ERROR: int = 500`, add the framing limits and character classes:

```noxy
let HTTP_MAX_METHOD_LENGTH: int = 64
let HTTP_MAX_TARGET_LENGTH: int = 2048
let HTTP_MAX_HEADER_NAME_LENGTH: int = 256
let HTTP_MAX_HEADER_LINES: int = 64

let HTTP_TOKEN_EXTRA: string = "!#$%&'*+-.^_`|~"

func build_visible_ascii() -> string
    let out: string = ""
    let code: int = 33
    while code <= 126 do
        out = out + from_char_code(code)
        code = code + 1
    end
    return out
end

let HTTP_VISIBLE_ASCII: string = build_visible_ascii()
```

`build_visible_ascii` produces every character from `0x21` to `0x7E` without needing escape sequences in a string literal.

- [ ] **Step 4: Extend `get_status_text`**

Replace the body of `get_status_text` with:

```noxy
func get_status_text(code: int) -> string
    if code == 200 then return "OK" end
    if code == 201 then return "Created" end
    if code == 204 then return "No Content" end
    if code == 400 then return "Bad Request" end
    if code == 401 then return "Unauthorized" end
    if code == 403 then return "Forbidden" end
    if code == 404 then return "Not Found" end
    if code == 408 then return "Request Timeout" end
    if code == 413 then return "Content Too Large" end
    if code == 414 then return "URI Too Long" end
    if code == 431 then return "Request Header Fields Too Large" end
    if code == 500 then return "Internal Server Error" end
    if code == 501 then return "Not Implemented" end
    if code == 505 then return "HTTP Version Not Supported" end
    return "Unknown"
end
```

- [ ] **Step 5: Replace `find_header_end` with the resumable scan**

Replace the whole existing `find_header_end` function (currently lines 62-79) with:

```noxy
// Scan for \r\n\r\n (13, 10, 13, 10), resuming at `start`.
func find_header_end_from(data: bytes, len: int, start: int) -> int
    let i: int = start
    if i < 0 then
        i = 0
    end
    let limit: int = len - 3

    while i < limit do
        if data[i] == 13 then
            if data[i+1] == 10 then
                if data[i+2] == 13 then
                    if data[i+3] == 10 then
                        return i
                    end
                end
            end
        end
        i = i + 1
    end
    return -1
end

func find_header_end(data: bytes, len: int) -> int
    return find_header_end_from(data, len, 0)
end
```

- [ ] **Step 6: Add the validation helpers and frame constructors**

Add after `find_header_end`, still inside the `Parse Helpers` section:

```noxy
func is_token_char(ch: string) -> bool
    if ch == "" then return false end
    if is_alnum(ch) then return true end
    return contains(HTTP_TOKEN_EXTRA, ch)
end

func is_token(s: string) -> bool
    let n: int = length(s)
    if n == 0 then return false end
    let i: int = 0
    while i < n do
        if !is_token_char(char_at(s, i)) then
            return false
        end
        i = i + 1
    end
    return true
end

func is_visible_ascii(s: string) -> bool
    let n: int = length(s)
    if n == 0 then return false end
    let i: int = 0
    while i < n do
        let ch: string = char_at(s, i)
        if ch == "" then return false end
        if !contains(HTTP_VISIBLE_ASCII, ch) then
            return false
        end
        i = i + 1
    end
    return true
end

func empty_request() -> HttpRequest
    let empty_h: string[64]
    return HttpRequest("GET", "/", "", "HTTP/1.1", empty_h, 0, b"")
end

func frame_error(status: int, message: string) -> HttpFrameResult
    return HttpFrameResult(false, empty_request(), status, message, true)
end

func frame_silent() -> HttpFrameResult
    return HttpFrameResult(false, empty_request(), 0, "", false)
end
```

- [ ] **Step 7: Replace `get_header` and add `count_header`**

Replace the whole existing `get_header` function (currently lines 294-311) with:

```noxy
func header_matches(line: string, search: string) -> bool
    return starts_with(to_lower(line), search)
end

func header_line_value(line: string) -> string
    let parts: SplitResult = split(line, ":")
    if parts.count < 2 then
        return ""
    end
    let value: string = parts.parts[1]
    let k: int = 2
    while k < parts.count do
        value = value + ":" + parts.parts[k]
        k = k + 1
    end
    return trim(value)
end

func get_header(headers: string[64], count: int, name: string) -> string
    let search: string = to_lower(name) + ":"
    let i: int = 0
    while i < count do
        if header_matches(headers[i], search) then
            return header_line_value(headers[i])
        end
        i = i + 1
    end
    return ""
end

func count_header(headers: string[64], count: int, name: string) -> int
    let search: string = to_lower(name) + ":"
    let found: int = 0
    let i: int = 0
    while i < count do
        if header_matches(headers[i], search) then
            found = found + 1
        end
        i = i + 1
    end
    return found
end
```

`header_line_value` rejoins every segment after the first `:`, so `Host: example.com:8080` keeps its port. `split` is byte-based, so a value carrying non-UTF-8 bytes is not corrupted.

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestFindHeaderEndFrom|TestIsToken|TestGetHeader|TestCountHeader|TestGetStatusText|TestFrameHelpers' -v`

Expected: PASS, every subtest.

- [ ] **Step 9: Verify nothing else regressed**

Run: `go build ./... && go vet ./... && go test ./internal/...`

Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add internal/stdlib/http_parser.nx internal/vm/http_parser_framing_test.go
git commit -m "feat(http): add framing primitives and fix header lookup

Resumable CRLFCRLF scan, RFC token and visible-ASCII validation, frame
result constructors, count_header, and status texts for 408/413/414/
431/501/505. get_header no longer truncates values containing a colon.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: Strict request-head parsing

`parse_request_head` validates the request line and header block and returns an `HttpFrameResult`.

**Files:**
- Modify: `internal/stdlib/http_parser.nx`
- Test: `internal/vm/http_parser_framing_test.go`

**Interfaces:**
- Consumes: `HttpFrameResult`, `frame_error`, `empty_request`, `is_token`, `is_visible_ascii`, `HTTP_MAX_METHOD_LENGTH`, `HTTP_MAX_TARGET_LENGTH`, `HTTP_MAX_HEADER_NAME_LENGTH`, `HTTP_MAX_HEADER_LINES` from Task 1.
- Produces: `func parse_request_head(header_bytes: bytes) -> HttpFrameResult`. On success `ok` is true and `request` carries `method`, `path`, `query`, `version`, `headers`, `header_count`, with `body` empty.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/http_parser_framing_test.go`:

```go
func parseHeadStatus(t *testing.T, block string) int64 {
	t.Helper()
	source := "let head: HttpFrameResult = parse_request_head(to_bytes(" + noxyQuote(block) + "))\n" +
		"if head.ok then test_report(0) else test_report(head.status) end"
	return captureParserInt(t, source)
}

func noxyQuote(s string) string {
	var b []byte
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			b = append(b, '\\', '"')
		case '\\':
			b = append(b, '\\', '\\')
		case '\r':
			b = append(b, '\\', 'r')
		case '\n':
			b = append(b, '\\', 'n')
		case '\t':
			b = append(b, '\\', 't')
		default:
			b = append(b, s[i])
		}
	}
	return string(append(b, '"'))
}

func TestParseRequestHeadAccepts(t *testing.T) {
	block := "POST /a/b?x=1&y=2 HTTP/1.1\r\nHost: example.com:8080\r\nContent-Length: 3"
	source := "let head: HttpFrameResult = parse_request_head(to_bytes(" + noxyQuote(block) + "))\n" +
		"test_report(head.request.method + \"|\" + head.request.path + \"|\" + head.request.query + \"|\" + head.request.version + \"|\" + to_str(head.request.header_count))"
	want := "POST|/a/b|x=1&y=2|HTTP/1.1|2"
	if got := captureParserString(t, source); got != want {
		t.Fatalf("parse_request_head = %q, want %q", got, want)
	}
}

func TestParseRequestHeadQueryKeepsLaterQuestionMarks(t *testing.T) {
	block := "GET /a?x=1?y=2 HTTP/1.1\r\nHost: a"
	source := "let head: HttpFrameResult = parse_request_head(to_bytes(" + noxyQuote(block) + "))\n" +
		"test_report(head.request.query)"
	if got := captureParserString(t, source); got != "x=1?y=2" {
		t.Fatalf("query = %q, want %q", got, "x=1?y=2")
	}
}

func TestParseRequestHeadRejects(t *testing.T) {
	longTarget := "/" + strings.Repeat("a", 2100)
	longMethod := strings.Repeat("G", 65)
	longName := strings.Repeat("n", 257)
	tests := []struct {
		name  string
		block string
		want  int64
	}{
		{name: "two token request line", block: "GET /\r\nHost: a", want: 400},
		{name: "four token request line", block: "GET / x HTTP/1.1\r\nHost: a", want: 400},
		{name: "empty request line", block: "\r\nGET / HTTP/1.1", want: 400},
		{name: "non token method", block: "GE T / HTTP/1.1\r\nHost: a", want: 400},
		{name: "over long method", block: longMethod + " / HTTP/1.1\r\nHost: a", want: 400},
		{name: "absolute form target", block: "GET http://a/b HTTP/1.1\r\nHost: a", want: 400},
		{name: "over long target", block: "GET " + longTarget + " HTTP/1.1\r\nHost: a", want: 414},
		{name: "http 0.9", block: "GET / HTTP/0.9\r\nHost: a", want: 505},
		{name: "http 2.0", block: "GET / HTTP/2.0\r\nHost: a", want: 505},
		{name: "malformed version", block: "GET / HTTPS/1.1\r\nHost: a", want: 400},
		{name: "obs fold with space", block: "GET / HTTP/1.1\r\nHost: a\r\n more", want: 400},
		{name: "obs fold with tab", block: "GET / HTTP/1.1\r\nHost: a\r\n\tmore", want: 400},
		{name: "header without colon", block: "GET / HTTP/1.1\r\nHost a", want: 400},
		{name: "space before colon", block: "GET / HTTP/1.1\r\nHost : a", want: 400},
		{name: "empty header name", block: "GET / HTTP/1.1\r\n: a", want: 400},
		{name: "over long header name", block: "GET / HTTP/1.1\r\n" + longName + ": a", want: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseHeadStatus(t, test.block); got != test.want {
				t.Fatalf("status = %d, want %d", got, test.want)
			}
		})
	}
}

func TestParseRequestHeadAcceptsHttp10(t *testing.T) {
	if got := parseHeadStatus(t, "GET / HTTP/1.0\r\nHost: a"); got != 0 {
		t.Fatalf("status = %d, want 0", got)
	}
}

func TestParseRequestHeadRejectsMoreThan64Headers(t *testing.T) {
	block := "GET / HTTP/1.1"
	for i := 0; i < 65; i++ {
		block += fmt.Sprintf("\r\nX-H%d: v", i)
	}
	if got := parseHeadStatus(t, block); got != 431 {
		t.Fatalf("status = %d, want 431", got)
	}
}

func TestParseRequestHeadAcceptsExactly64Headers(t *testing.T) {
	block := "GET / HTTP/1.1"
	for i := 0; i < 64; i++ {
		block += fmt.Sprintf("\r\nX-H%d: v", i)
	}
	if got := parseHeadStatus(t, block); got != 0 {
		t.Fatalf("status = %d, want 0", got)
	}
}
```

Add `"fmt"` and `"strings"` to the import block of `internal/vm/http_parser_framing_test.go`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run TestParseRequestHead -v`

Expected: FAIL — `parse_request_head` is undefined.

- [ ] **Step 3: Implement `parse_request_head`**

Add to `internal/stdlib/http_parser.nx`, in the `Request Parser` section immediately before the existing `parse_request`:

```noxy
func parse_request_head(header_bytes: bytes) -> HttpFrameResult
    let header_str: string = to_str(header_bytes)
    let lines: SplitResult = split(header_str, "\r\n")
    if lines.count == 0 then
        return frame_error(400, "Bad Request")
    end

    let req_line: string = lines.parts[0]
    if length(req_line) == 0 then
        return frame_error(400, "Bad Request")
    end

    let req_parts: SplitResult = split(req_line, " ")
    if req_parts.count != 3 then
        return frame_error(400, "Bad Request")
    end

    let method: string = req_parts.parts[0]
    if length(method) > HTTP_MAX_METHOD_LENGTH then
        return frame_error(400, "Bad Request")
    end
    if !is_token(method) then
        return frame_error(400, "Bad Request")
    end

    let target: string = req_parts.parts[1]
    if length(target) == 0 then
        return frame_error(400, "Bad Request")
    end
    if length(target) > HTTP_MAX_TARGET_LENGTH then
        return frame_error(414, "URI Too Long")
    end
    if !starts_with(target, "/") then
        return frame_error(400, "Bad Request")
    end
    if !is_visible_ascii(target) then
        return frame_error(400, "Bad Request")
    end

    let version: string = req_parts.parts[2]
    if version != "HTTP/1.0" && version != "HTTP/1.1" then
        if starts_with(version, "HTTP/") then
            return frame_error(505, "HTTP Version Not Supported")
        end
        return frame_error(400, "Bad Request")
    end

    let req: HttpRequest = empty_request()
    req.method = method
    req.version = version

    let q: SplitResult = split(target, "?")
    req.path = q.parts[0]
    if q.count > 1 then
        let query: string = q.parts[1]
        let k: int = 2
        while k < q.count do
            query = query + "?" + q.parts[k]
            k = k + 1
        end
        req.query = query
    end

    let count: int = 0
    let i: int = 1
    while i < lines.count do
        let line: string = lines.parts[i]
        if length(line) == 0 then
            return frame_error(400, "Bad Request")
        end
        if starts_with(line, " ") || starts_with(line, "\t") then
            return frame_error(400, "Bad Request")
        end
        let parts: SplitResult = split(line, ":")
        if parts.count < 2 then
            return frame_error(400, "Bad Request")
        end
        let name: string = parts.parts[0]
        if length(name) > HTTP_MAX_HEADER_NAME_LENGTH then
            return frame_error(400, "Bad Request")
        end
        if !is_token(name) then
            return frame_error(400, "Bad Request")
        end
        if count >= HTTP_MAX_HEADER_LINES then
            return frame_error(431, "Request Header Fields Too Large")
        end
        req.headers[count] = line
        count = count + 1
        i = i + 1
    end
    req.header_count = count

    return HttpFrameResult(true, req, 0, "", true)
end
```

`is_token` rejects an empty name and any name carrying whitespace, which covers both the empty-name and space-before-colon cases without a separate check.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run TestParseRequestHead -v`

Expected: PASS, every subtest.

- [ ] **Step 5: Commit**

```bash
git add internal/stdlib/http_parser.nx internal/vm/http_parser_framing_test.go
git commit -m "feat(http): add strict request head parsing

parse_request_head validates the request line and every header line and
returns an HttpFrameResult carrying the status to answer with. Rejects
obs-folding, whitespace before the colon, non-origin targets, unsupported
versions, and silent truncation past 64 headers.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: `Content-Length` resolution

`resolve_body_length` decides how many body bytes to read, rejecting conflicting or malformed framing headers.

**Files:**
- Modify: `internal/stdlib/http_parser.nx`
- Test: `internal/vm/http_parser_framing_test.go`

**Interfaces:**
- Consumes: `BodyLengthResult`, `count_header`, `get_header` from Task 1.
- Produces: `func resolve_body_length(headers: string[64], count: int, max_body_bytes: int) -> BodyLengthResult`.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/http_parser_framing_test.go`:

```go
func resolveBodyLength(t *testing.T, headerLines []string, maxBody int) (bool, int64, int64) {
	t.Helper()
	source := "let headers: string[64]\n"
	for i, line := range headerLines {
		source += fmt.Sprintf("headers[%d] = %s\n", i, noxyQuote(line))
	}
	source += fmt.Sprintf("let r: BodyLengthResult = resolve_body_length(headers, %d, %d)\n", len(headerLines), maxBody)

	okSource := source + "test_report(r.ok)"
	lengthSource := source + "test_report(r.length)"
	statusSource := source + "test_report(r.status)"
	return captureParserBool(t, okSource), captureParserInt(t, lengthSource), captureParserInt(t, statusSource)
}

func TestResolveBodyLength(t *testing.T) {
	tests := []struct {
		name       string
		headers    []string
		maxBody    int
		wantOK     bool
		wantLength int64
		wantStatus int64
	}{
		{name: "absent means zero", headers: []string{"Host: a"}, maxBody: 1024, wantOK: true, wantLength: 0},
		{name: "valid length", headers: []string{"Content-Length: 12"}, maxBody: 1024, wantOK: true, wantLength: 12},
		{name: "zero length", headers: []string{"Content-Length: 0"}, maxBody: 1024, wantOK: true, wantLength: 0},
		{name: "case insensitive", headers: []string{"content-length: 7"}, maxBody: 1024, wantOK: true, wantLength: 7},
		{name: "duplicate identical", headers: []string{"Content-Length: 5", "Content-Length: 5"}, maxBody: 1024, wantStatus: 400},
		{name: "duplicate conflicting", headers: []string{"Content-Length: 5", "Content-Length: 6"}, maxBody: 1024, wantStatus: 400},
		{name: "comma list", headers: []string{"Content-Length: 5, 5"}, maxBody: 1024, wantStatus: 400},
		{name: "signed value", headers: []string{"Content-Length: +5"}, maxBody: 1024, wantStatus: 400},
		{name: "negative value", headers: []string{"Content-Length: -5"}, maxBody: 1024, wantStatus: 400},
		{name: "float value", headers: []string{"Content-Length: 5.5"}, maxBody: 1024, wantStatus: 400},
		{name: "empty value", headers: []string{"Content-Length:"}, maxBody: 1024, wantStatus: 400},
		{name: "hex value", headers: []string{"Content-Length: 0x10"}, maxBody: 1024, wantStatus: 400},
		{name: "twenty digits", headers: []string{"Content-Length: 99999999999999999999"}, maxBody: 1024, wantStatus: 400},
		{name: "over max body", headers: []string{"Content-Length: 2048"}, maxBody: 1024, wantStatus: 413},
		{name: "exactly max body", headers: []string{"Content-Length: 1024"}, maxBody: 1024, wantOK: true, wantLength: 1024},
		{name: "chunked", headers: []string{"Transfer-Encoding: chunked"}, maxBody: 1024, wantStatus: 501},
		{name: "chunked with length", headers: []string{"Transfer-Encoding: chunked", "Content-Length: 5"}, maxBody: 1024, wantStatus: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ok, length, status := resolveBodyLength(t, test.headers, test.maxBody)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v", ok, test.wantOK)
			}
			if length != test.wantLength {
				t.Fatalf("length = %d, want %d", length, test.wantLength)
			}
			if status != test.wantStatus {
				t.Fatalf("status = %d, want %d", status, test.wantStatus)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run TestResolveBodyLength -v`

Expected: FAIL — `resolve_body_length` is undefined.

- [ ] **Step 3: Implement `resolve_body_length`**

Add to `internal/stdlib/http_parser.nx`, at the end of the `Header Utils` section:

```noxy
func resolve_body_length(headers: string[64], count: int, max_body_bytes: int) -> BodyLengthResult
    let encodings: int = count_header(headers, count, "Transfer-Encoding")
    let lengths: int = count_header(headers, count, "Content-Length")

    if encodings > 0 && lengths > 0 then
        return BodyLengthResult(false, 0, 400, "Bad Request")
    end
    if encodings > 0 then
        return BodyLengthResult(false, 0, 501, "Not Implemented")
    end
    if lengths > 1 then
        return BodyLengthResult(false, 0, 400, "Bad Request")
    end
    if lengths == 0 then
        return BodyLengthResult(true, 0, 0, "")
    end

    let raw: string = get_header(headers, count, "Content-Length")
    if length(raw) == 0 || length(raw) > 19 then
        return BodyLengthResult(false, 0, 400, "Bad Request")
    end
    if !is_digit(raw) then
        return BodyLengthResult(false, 0, 400, "Bad Request")
    end

    let declared: int = to_int(raw)
    if declared < 0 then
        return BodyLengthResult(false, 0, 400, "Bad Request")
    end
    if declared > max_body_bytes then
        return BodyLengthResult(false, 0, 413, "Content Too Large")
    end

    return BodyLengthResult(true, declared, 0, "")
end
```

`is_digit` is ASCII-only and rejects an empty string, so `+5`, `-5`, `5.5`, `0x10`, and `5, 5` never reach `to_int`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run TestResolveBodyLength -v`

Expected: PASS, every subtest.

- [ ] **Step 5: Run the full parser suite and commit**

Run: `go test ./internal/vm/ -run 'TestFindHeaderEnd|TestIsToken|TestGetHeader|TestCountHeader|TestGetStatusText|TestFrameHelpers|TestParseRequestHead|TestResolveBodyLength'`

Expected: PASS.

```bash
git add internal/stdlib/http_parser.nx internal/vm/http_parser_framing_test.go
git commit -m "feat(http): resolve body length with strict framing rules

resolve_body_length rejects Transfer-Encoding (501), Transfer-Encoding
combined with Content-Length (400), duplicate Content-Length (400), any
non-digit value (400), and a declared length above the configured maximum
(413). An absent framing header means a zero-length body.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: Server configuration and binding

`HttpServer` gains its configuration fields, `new_server` fills defaults, `bind_server` binds and reports the real port, and `serve` reuses an existing listener.

**Files:**
- Modify: `internal/stdlib/http_server.nx`
- Test: `internal/vm/http_server_framing_test.go` (create)

**Interfaces:**
- Consumes: `HttpFrameResult` and helpers from Tasks 1-3 (not yet called here).
- Produces:
  - `struct HttpLimits` with fields `max_header_bytes: int`, `max_body_bytes: int`, `header_timeout_ms: int`, `body_timeout_ms: int`, `write_timeout_ms: int`, `read_chunk_bytes: int`
  - `func bind_server(server: ref HttpServer) -> bool`
  - `func server_limits(server: ref HttpServer) -> HttpLimits`
  - `HttpServer` with the six new fields appended after `running`

- [ ] **Step 1: Write the failing test**

Create `internal/vm/http_server_framing_test.go`:

```go
package vm

import (
	"testing"

	"noxy-vm/internal/value"
)

func captureServerInt(t *testing.T, body string) int64 {
	t.Helper()
	captured := captureVMSource(t, "use http_server select *\nuse http_parser select *\n"+body)
	if captured.Type != value.VAL_INT {
		t.Fatalf("test_report value = %#v, want int", captured)
	}
	return captured.AsInt
}

func captureServerBool(t *testing.T, body string) bool {
	t.Helper()
	captured := captureVMSource(t, "use http_server select *\nuse http_parser select *\n"+body)
	if captured.Type != value.VAL_BOOL {
		t.Fatalf("test_report value = %#v, want bool", captured)
	}
	return captured.AsBool
}

func TestNewServerInstallsDefaults(t *testing.T) {
	tests := []struct {
		field string
		want  int64
	}{
		{field: "max_header_bytes", want: 16384},
		{field: "max_body_bytes", want: 1048576},
		{field: "header_timeout_ms", want: 5000},
		{field: "body_timeout_ms", want: 15000},
		{field: "write_timeout_ms", want: 15000},
		{field: "read_chunk_bytes", want: 8192},
	}
	for _, test := range tests {
		t.Run(test.field, func(t *testing.T) {
			source := "let s: HttpServer = new_server(\"127.0.0.1\", 8080)\ntest_report(s." + test.field + ")"
			if got := captureServerInt(t, source); got != test.want {
				t.Fatalf("%s = %d, want %d", test.field, got, test.want)
			}
		})
	}
}

func TestServerLimitsReplacesNonPositiveWithDefaults(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 8080)
s.max_header_bytes = 0
s.max_body_bytes = -1
s.header_timeout_ms = 0
s.body_timeout_ms = -7
s.write_timeout_ms = 0
s.read_chunk_bytes = -3
let limits: HttpLimits = server_limits(s)
test_report(limits.max_header_bytes + limits.max_body_bytes + limits.header_timeout_ms + limits.body_timeout_ms + limits.write_timeout_ms + limits.read_chunk_bytes)`
	want := int64(16384 + 1048576 + 5000 + 15000 + 15000 + 8192)
	if got := captureServerInt(t, source); got != want {
		t.Fatalf("sum = %d, want %d", got, want)
	}
}

func TestServerLimitsClampsChunkToHeaderBudget(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 8080)
s.max_header_bytes = 512
let limits: HttpLimits = server_limits(s)
test_report(limits.read_chunk_bytes)`
	if got := captureServerInt(t, source); got != 512 {
		t.Fatalf("read_chunk_bytes = %d, want 512", got)
	}
}

func TestServerLimitsKeepsConfiguredValues(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 8080)
s.max_body_bytes = 4096
let limits: HttpLimits = server_limits(s)
test_report(limits.max_body_bytes)`
	if got := captureServerInt(t, source); got != 4096 {
		t.Fatalf("max_body_bytes = %d, want 4096", got)
	}
}

func TestBindServerWritesEphemeralPortBack(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 0)
let ok: bool = bind_server(s)
if !ok then
    test_report(0)
else
    let assigned: int = s.port
    stop_server(s)
    if assigned > 0 then test_report(1) else test_report(0) end
end`
	if got := captureServerInt(t, source); got != 1 {
		t.Fatal("bind_server did not write a positive bound port back to server.port")
	}
}

func TestBindServerIsIdempotent(t *testing.T) {
	source := `let s: HttpServer = new_server("127.0.0.1", 0)
let first: bool = bind_server(s)
let assigned: int = s.port
let second: bool = bind_server(s)
let same: bool = s.port == assigned
stop_server(s)
test_report(first && second && same)`
	if !captureServerBool(t, source) {
		t.Fatal("a second bind_server call rebound the listener")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run 'TestNewServerInstallsDefaults|TestServerLimits|TestBindServer' -v`

Expected: FAIL — `HttpServer` has no `max_header_bytes` field and `bind_server`/`server_limits`/`HttpLimits` are undefined.

- [ ] **Step 3: Replace the struct, factory, and add limits**

In `internal/stdlib/http_server.nx`, replace the `Structure` and `Factory` sections (currently lines 7-26) with:

```noxy
// ============================================
// Structure
// ============================================

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

struct HttpLimits
    max_header_bytes: int
    max_body_bytes: int
    header_timeout_ms: int
    body_timeout_ms: int
    write_timeout_ms: int
    read_chunk_bytes: int
end

// ============================================
// Defaults
// ============================================

let HTTP_DEFAULT_MAX_HEADER_BYTES: int = 16384
let HTTP_DEFAULT_MAX_BODY_BYTES: int = 1048576
let HTTP_DEFAULT_HEADER_TIMEOUT_MS: int = 5000
let HTTP_DEFAULT_BODY_TIMEOUT_MS: int = 15000
let HTTP_DEFAULT_WRITE_TIMEOUT_MS: int = 15000
let HTTP_DEFAULT_READ_CHUNK_BYTES: int = 8192

// ============================================
// Factory
// ============================================

func new_server(host: string, port: int) -> HttpServer
    // Return with invalid socket initially
    let s: Socket = Socket(-1, "", 0, false)
    let server: HttpServer = HttpServer(host, port, s, false, 0, 0, 0, 0, 0, 0)
    server.max_header_bytes = HTTP_DEFAULT_MAX_HEADER_BYTES
    server.max_body_bytes = HTTP_DEFAULT_MAX_BODY_BYTES
    server.header_timeout_ms = HTTP_DEFAULT_HEADER_TIMEOUT_MS
    server.body_timeout_ms = HTTP_DEFAULT_BODY_TIMEOUT_MS
    server.write_timeout_ms = HTTP_DEFAULT_WRITE_TIMEOUT_MS
    server.read_chunk_bytes = HTTP_DEFAULT_READ_CHUNK_BYTES
    return server
end

func positive_or(candidate: int, fallback: int) -> int
    if candidate > 0 then
        return candidate
    end
    return fallback
end

func server_limits(server: ref HttpServer) -> HttpLimits
    let max_header: int = positive_or(server.max_header_bytes, HTTP_DEFAULT_MAX_HEADER_BYTES)
    let max_body: int = positive_or(server.max_body_bytes, HTTP_DEFAULT_MAX_BODY_BYTES)
    let header_ms: int = positive_or(server.header_timeout_ms, HTTP_DEFAULT_HEADER_TIMEOUT_MS)
    let body_ms: int = positive_or(server.body_timeout_ms, HTTP_DEFAULT_BODY_TIMEOUT_MS)
    let write_ms: int = positive_or(server.write_timeout_ms, HTTP_DEFAULT_WRITE_TIMEOUT_MS)
    let chunk: int = positive_or(server.read_chunk_bytes, HTTP_DEFAULT_READ_CHUNK_BYTES)
    if chunk > max_header then
        chunk = max_header
    end
    return HttpLimits(max_header, max_body, header_ms, body_ms, write_ms, chunk)
end
```

- [ ] **Step 4: Add `bind_server` and make `serve` reuse the listener**

Replace the existing `serve` function (currently lines 66-95) with:

```noxy
func bind_server(server: ref HttpServer) -> bool
    if server.listener.open then
        server.running = true
        return true
    end

    let s: Socket = listen(server.host, server.port)
    if !s.open then
        return false
    end

    server.listener = s
    server.port = s.port
    server.running = true
    return true
end

func serve(server: ref HttpServer, handler: func) -> void
    if !bind_server(server) then
        print("Failed to bind to " + server.host + ":" + to_str(server.port))
        return
    end

    let limits: HttpLimits = server_limits(server)

    print("Server listening on " + server.host + ":" + to_str(server.port))

    // Accept Loop
    while server.running do
        // Accept blocks until a connection arrives
        let client: Socket = accept(server.listener)

        if client.open then
            // Spawn a routine to handle this client
            spawn(handle_client_connection, client, handler, limits)
        end
    end

    socket_close(server.listener)
end
```

- [ ] **Step 5: Widen `handle_client_connection` so the module still compiles**

`serve` now passes a third argument. Change only the signature of the existing `handle_client_connection` for now; its body is rewritten in Task 6:

```noxy
func handle_client_connection(client: Socket, user_handler: func, limits: HttpLimits)
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestNewServerInstallsDefaults|TestServerLimits|TestBindServer' -v`

Expected: PASS, every subtest.

- [ ] **Step 7: Commit**

```bash
git add internal/stdlib/http_server.nx internal/vm/http_server_framing_test.go
git commit -m "feat(http): add server limits and explicit binding

HttpServer carries per-server limits and timeouts with documented
defaults, server_limits normalizes them once per serve call, and
bind_server separates binding from the accept loop while writing the real
bound port back so port 0 is usable.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: Complete-write loop

`send_all` writes a response completely, resuming after a partial write.

**Files:**
- Modify: `internal/stdlib/http_server.nx`
- Test: `internal/vm/http_server_framing_test.go`

**Interfaces:**
- Consumes: `HttpLimits` from Task 4.
- Produces: `func send_all(client: Socket, data: bytes, write_timeout_ms: int) -> bool`.

- [ ] **Step 1: Write the failing test**

Append to `internal/vm/http_server_framing_test.go`. This test drives `send_all` against a real TCP peer that reads slowly, so the write cannot complete in one syscall:

```go
func TestSendAllWritesCompletelyToSlowReader(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	const payloadSize = 4 << 20
	received := make(chan int, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			received <- -1
			return
		}
		defer conn.Close()
		total := 0
		buffer := make([]byte, 4096)
		for total < payloadSize {
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			n, readErr := conn.Read(buffer)
			total += n
			if readErr != nil {
				break
			}
			time.Sleep(time.Millisecond)
		}
		received <- total
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	source := fmt.Sprintf(`use http_server select *
use net select *
use strings select *
let sock: Socket = connect("127.0.0.1", %d)
let payload: bytes = to_bytes(repeat("x", %d))
let ok: bool = send_all(sock, payload, 20000)
socket_close(sock)
test_report(ok)`, port, payloadSize)

	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if interpretErr := interpretVMSourceWithinBound(t, machine, source); interpretErr != nil {
		t.Fatal(interpretErr)
	}
	if captured.Type != value.VAL_BOOL || !captured.AsBool {
		t.Fatalf("send_all = %#v, want true", captured)
	}

	select {
	case total := <-received:
		if total != payloadSize {
			t.Fatalf("peer received %d bytes, want %d", total, payloadSize)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("peer did not finish reading")
	}
}

func TestSendAllFailsWhenPeerIsGone(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	closed := make(chan struct{})
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
		_ = listener.Close()
		close(closed)
	}()

	source := fmt.Sprintf(`use http_server select *
use net select *
use strings select *
use time select *
let sock: Socket = connect("127.0.0.1", %d)
sleep(200)
let payload: bytes = to_bytes(repeat("y", 4194304))
let ok: bool = send_all(sock, payload, 2000)
socket_close(sock)
test_report(ok)`, port)

	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if interpretErr := interpretVMSourceWithinBound(t, machine, source); interpretErr != nil {
		t.Fatal(interpretErr)
	}
	<-closed
	if captured.Type != value.VAL_BOOL || captured.AsBool {
		t.Fatalf("send_all = %#v, want false", captured)
	}
}
```

Add `"fmt"`, `"net"`, and `"time"` to the import block of `internal/vm/http_server_framing_test.go`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/vm/ -run TestSendAll -v`

Expected: FAIL — `send_all` is undefined.

- [ ] **Step 3: Implement `send_all`**

Add to `internal/stdlib/http_server.nx`, immediately before the `Handler Internal Logic` banner:

```noxy
// ============================================
// Complete Write
// ============================================

func send_all(client: Socket, data: bytes, write_timeout_ms: int) -> bool
    let size: int = length(data)
    let offset: int = 0
    let deadline: int = time_now_ms() + write_timeout_ms

    while offset < size do
        let remaining: int = deadline - time_now_ms()
        if remaining <= 0 then
            return false
        end
        settimeout(client, remaining)

        let res: NetResult = socket_send(client, slice(data, offset, size))
        offset = offset + res.count
        if !res.ok && res.count == 0 then
            return false
        end
    end
    return true
end
```

The offset advances by `res.count` even when `ok` is false, because `net_send` reports its actual transferred count alongside a failure. `remaining` is checked before `settimeout` because a non-positive timeout is a runtime error.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run TestSendAll -v`

Expected: PASS, both tests.

- [ ] **Step 5: Commit**

```bash
git add internal/stdlib/http_server.nx internal/vm/http_server_framing_test.go
git commit -m "feat(http): write responses completely

send_all resumes from the transferred offset after a partial write and
bounds the whole response with one write deadline, so a slow-reading
client no longer truncates the response.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Incremental read loop and connection lifecycle

`read_request` reads headers and body to completion under deadlines and limits; `handle_client_connection` uses it, answers rejections, and closes with `defer`.

**Files:**
- Modify: `internal/stdlib/http_server.nx`
- Test: `internal/vm/http_server_framing_test.go`

**Interfaces:**
- Consumes: `HttpLimits`, `server_limits`, `bind_server`, `send_all` (Tasks 4-5); `HttpFrameResult`, `frame_error`, `frame_silent`, `find_header_end_from`, `parse_request_head`, `resolve_body_length` (Tasks 1-3).
- Produces:
  - `func read_request(client: Socket, limits: HttpLimits) -> HttpFrameResult`
  - `func recv_failure_status(message: string) -> int` returning `408` for the normalized timeout string and `0` for anything else
  - `func handle_client_connection(client: Socket, user_handler: func, limits: HttpLimits)`

- [ ] **Step 1: Write the harness and the failing tests**

Append to `internal/vm/http_server_framing_test.go`. The harness starts the Noxy server, learns its ephemeral port through a test native, and stops it on cleanup:

```go
type noxyHTTPServer struct {
	t       *testing.T
	port    int
	release chan struct{}
	done    chan error
	once    sync.Once
}

// startNoxyHTTPServer runs the Noxy HTTP server on an ephemeral port.
// handlerBody is Noxy source for the body of `func handler(req: HttpRequest) -> HttpResponse`.
// config is Noxy source assigning fields on the `server` variable, e.g. `server.body_timeout_ms = 400`.
func startNoxyHTTPServer(t *testing.T, handlerBody string, config string) *noxyHTTPServer {
	t.Helper()

	harness := &noxyHTTPServer{
		t:       t,
		release: make(chan struct{}),
		done:    make(chan error, 1),
	}
	ready := make(chan int, 1)

	machine := New()
	machine.DefineNative("harness_ready", func(args []value.Value) value.Value {
		port := 0
		if len(args) == 1 {
			port = int(args[0].AsInt)
		}
		ready <- port
		return value.NewNull()
	})
	machine.DefineNative("harness_wait", func([]value.Value) value.Value {
		<-harness.release
		return value.NewNull()
	})

	source := `use http_server select *
use http_parser select *

func handler(req: HttpRequest) -> HttpResponse
` + handlerBody + `
end

func serve_loop()
    serve(server, handler)
end

global server: HttpServer = new_server("127.0.0.1", 0)
` + config + `
if bind_server(server) then
    spawn(serve_loop)
    harness_ready(server.port)
    harness_wait()
    stop_server(server)
else
    harness_ready(0)
    harness_wait()
end
`

	code := compileVMSource(t, source)
	go func() {
		harness.done <- machine.Interpret(code)
	}()

	select {
	case port := <-ready:
		if port <= 0 {
			harness.stop()
			t.Fatal("noxy http server failed to bind")
		}
		harness.port = port
	case <-time.After(10 * time.Second):
		harness.stop()
		t.Fatal("noxy http server did not report its port")
	}

	t.Cleanup(harness.stop)
	return harness
}

func (h *noxyHTTPServer) stop() {
	h.once.Do(func() {
		close(h.release)
		select {
		case err := <-h.done:
			if err != nil {
				h.t.Errorf("noxy http server exited with error: %v", err)
			}
		case <-time.After(10 * time.Second):
			h.t.Error("noxy http server did not shut down")
		}
	})
}

func (h *noxyHTTPServer) dial() net.Conn {
	h.t.Helper()
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", h.port), 5*time.Second)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	return conn
}

// readRawResponse reads until EOF, which the server guarantees by closing after one response.
func readRawResponse(t *testing.T, conn net.Conn) string {
	t.Helper()
	var builder strings.Builder
	buffer := make([]byte, 4096)
	for {
		n, err := conn.Read(buffer)
		builder.Write(buffer[:n])
		if err != nil {
			break
		}
	}
	return builder.String()
}

func responseStatus(t *testing.T, raw string) int {
	t.Helper()
	if raw == "" {
		return 0
	}
	line, _, found := strings.Cut(raw, "\r\n")
	if !found {
		t.Fatalf("response has no status line: %q", raw)
	}
	fields := strings.Split(line, " ")
	if len(fields) < 2 {
		t.Fatalf("malformed status line: %q", line)
	}
	code, err := strconv.Atoi(fields[1])
	if err != nil {
		t.Fatalf("status line %q: %v", line, err)
	}
	return code
}

func responseBody(t *testing.T, raw string) string {
	t.Helper()
	_, body, found := strings.Cut(raw, "\r\n\r\n")
	if !found {
		t.Fatalf("response has no body separator: %q", raw)
	}
	return body
}

const echoHandler = `    if req.path == "/echo" then
        return response_text(to_str(req.body))
    end
    if req.path == "/len" then
        return response_text(to_str(length(req.body)))
    end
    if req.path == "/query" then
        return response_text(req.query)
    end
    return response_text("ok")`

func TestServerFramesRequestDeliveredOneByteAtATime(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	request := "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 11\r\n\r\nhello world"
	for i := 0; i < len(request); i++ {
		if _, err := conn.Write([]byte(request[i : i+1])); err != nil {
			t.Fatal(err)
		}
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "hello world" {
		t.Fatalf("body = %q, want %q", got, "hello world")
	}
}

func TestServerFramesRequestSplitInsideTerminator(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	head := "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 5\r\n\r"
	if _, err := conn.Write([]byte(head)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write([]byte("\nabcde")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "abcde" {
		t.Fatalf("body = %q, want %q", got, "abcde")
	}
}

func TestServerReadsBodyLargerThanReadChunk(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "server.read_chunk_bytes = 64")
	conn := server.dial()
	payload := strings.Repeat("z", 5000)
	request := fmt.Sprintf("POST /len HTTP/1.1\r\nHost: a\r\nContent-Length: %d\r\n\r\n%s", len(payload), payload)
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "5000" {
		t.Fatalf("body = %q, want %q", got, "5000")
	}
}

func TestServerReadsBodyDeliveredInSegments(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 9\r\n\r\nabc")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write([]byte("def")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write([]byte("ghi")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseBody(t, raw); got != "abcdefghi" {
		t.Fatalf("body = %q, want %q", got, "abcdefghi")
	}
}

func TestServerTreatsMissingContentLengthAsEmptyBody(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("POST /len HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status = %d, want 200", got)
	}
	if got := responseBody(t, raw); got != "0" {
		t.Fatalf("body = %q, want %q", got, "0")
	}
}

func TestServerIgnoresSurplusPipelinedBytes(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	request := "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 2\r\n\r\nokGET /extra HTTP/1.1\r\nHost: a\r\n\r\n"
	if _, err := conn.Write([]byte(request)); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseBody(t, raw); got != "ok" {
		t.Fatalf("body = %q, want %q", got, "ok")
	}
	if strings.Count(raw, "HTTP/1.1 ") != 1 {
		t.Fatalf("expected exactly one response, got %q", raw)
	}
}

func TestServerParsesQuery(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if _, err := conn.Write([]byte("GET /query?a=1&b=2 HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, conn)
	if got := responseBody(t, raw); got != "a=1&b=2" {
		t.Fatalf("body = %q, want %q", got, "a=1&b=2")
	}
}

func TestServerRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		request string
		want    int
	}{
		{name: "header block too large", config: "server.max_header_bytes = 256", request: "GET / HTTP/1.1\r\nX-Big: " + strings.Repeat("p", 400) + "\r\n\r\n", want: 431},
		{name: "body too large", config: "server.max_body_bytes = 16", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 64\r\n\r\n", want: 413},
		{name: "duplicate content length", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 5\r\nContent-Length: 5\r\n\r\nabcde", want: 400},
		{name: "signed content length", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: +5\r\n\r\nabcde", want: 400},
		{name: "chunked encoding", request: "POST /echo HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n", want: 501},
		{name: "chunked with length", request: "POST /echo HTTP/1.1\r\nHost: a\r\nTransfer-Encoding: chunked\r\nContent-Length: 5\r\n\r\nabcde", want: 400},
		{name: "unsupported version", request: "GET / HTTP/2.0\r\nHost: a\r\n\r\n", want: 505},
		{name: "obs fold", request: "GET / HTTP/1.1\r\nHost: a\r\n more\r\n\r\n", want: 400},
		{name: "space before colon", request: "GET / HTTP/1.1\r\nHost : a\r\n\r\n", want: 400},
		{name: "target with space", request: "GET /a b HTTP/1.1\r\nHost: a\r\n\r\n", want: 400},
		{name: "absolute form target", request: "GET http://a/b HTTP/1.1\r\nHost: a\r\n\r\n", want: 400},
		{name: "target too long", request: "GET /" + strings.Repeat("q", 2100) + " HTTP/1.1\r\nHost: a\r\n\r\n", want: 414},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startNoxyHTTPServer(t, echoHandler, test.config)
			conn := server.dial()
			if _, err := conn.Write([]byte(test.request)); err != nil {
				t.Fatal(err)
			}
			raw := readRawResponse(t, conn)
			if got := responseStatus(t, raw); got != test.want {
				t.Fatalf("status = %d, want %d (response %q)", got, test.want, raw)
			}
			if !strings.Contains(raw, "Connection: close") {
				t.Fatalf("response is missing Connection: close: %q", raw)
			}
			declared := fmt.Sprintf("Content-Length: %d\r\n", len(responseBody(t, raw)))
			if !strings.Contains(raw, declared) {
				t.Fatalf("response does not declare %q: %q", declared, raw)
			}
		})
	}
}

func TestServerRejectsEofMidRequest(t *testing.T) {
	tests := []struct {
		name    string
		request string
	}{
		{name: "eof mid header", request: "GET / HTTP/1.1\r\nHost: a"},
		{name: "eof mid body", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 10\r\n\r\nabc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startNoxyHTTPServer(t, echoHandler, "")
			conn := server.dial()
			if _, err := conn.Write([]byte(test.request)); err != nil {
				t.Fatal(err)
			}
			if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			raw := readRawResponse(t, conn)
			if got := responseStatus(t, raw); got != 400 {
				t.Fatalf("status = %d, want 400 (response %q)", got, raw)
			}
		})
	}
}

func TestServerTimesOutStalledClients(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		request string
	}{
		{name: "stalled mid header", config: "server.header_timeout_ms = 400", request: "GET / HTTP/1.1\r\nHost: a\r\n"},
		{name: "stalled mid body", config: "server.body_timeout_ms = 400", request: "POST /echo HTTP/1.1\r\nHost: a\r\nContent-Length: 10\r\n\r\nabc"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := startNoxyHTTPServer(t, echoHandler, test.config)
			conn := server.dial()
			started := time.Now()
			if _, err := conn.Write([]byte(test.request)); err != nil {
				t.Fatal(err)
			}
			raw := readRawResponse(t, conn)
			if got := responseStatus(t, raw); got != 408 {
				t.Fatalf("status = %d, want 408 (response %q)", got, raw)
			}
			if elapsed := time.Since(started); elapsed > 10*time.Second {
				t.Fatalf("timeout took %s, want a bounded wait", elapsed)
			}
		})
	}
}

func TestServerTimesOutSlowlorisTrickle(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "server.header_timeout_ms = 600")
	conn := server.dial()
	request := "GET / HTTP/1.1\r\nHost: a\r\nX-Pad: 0123456789\r\n\r\n"
	go func() {
		for i := 0; i < len(request); i++ {
			if _, err := conn.Write([]byte(request[i : i+1])); err != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	raw := readRawResponse(t, conn)
	if got := responseStatus(t, raw); got != 408 {
		t.Fatalf("status = %d, want 408 (response %q)", got, raw)
	}
}

func TestServerClosesSilentlyOnEmptyConnection(t *testing.T) {
	server := startNoxyHTTPServer(t, echoHandler, "")
	conn := server.dial()
	if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if raw := readRawResponse(t, conn); raw != "" {
		t.Fatalf("response = %q, want no bytes", raw)
	}
}

func TestServerClosesConnectionWhenHandlerFails(t *testing.T) {
	failing := `    if req.path == "/boom" then
        let numbers: int[2]
        return response_text(to_str(numbers[9]))
    end
    return response_text("ok")`
	server := startNoxyHTTPServer(t, failing, "")

	boom := server.dial()
	if _, err := boom.Write([]byte("GET /boom HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = readRawResponse(t, boom)

	survivor := server.dial()
	if _, err := survivor.Write([]byte("GET /ok HTTP/1.1\r\nHost: a\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	raw := readRawResponse(t, survivor)
	if got := responseStatus(t, raw); got != 200 {
		t.Fatalf("status after failing handler = %d, want 200 (response %q)", got, raw)
	}
}
```

Add `"strconv"`, `"strings"`, and `"sync"` to the import block of `internal/vm/http_server_framing_test.go`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/vm/ -run 'TestServerFrames|TestServerReads|TestServerTreats|TestServerIgnores|TestServerParses|TestServerRejects|TestServerTimesOut|TestServerCloses' -v`

Expected: FAIL — the server still performs a single `socket_recv`, so fragmented requests time out or are mis-framed and no rejection status is ever produced.

- [ ] **Step 3: Implement `read_request`**

In `internal/stdlib/http_server.nx`, replace the whole `Handler Internal Logic` section (the `handle_client_connection` function, currently lines 32-60) with:

```noxy
// ============================================
// Request Framing
// ============================================

func recv_failure_status(message: string) -> int
    if message == "operation timed out" then
        return 408
    end
    return 0
end

func read_request(client: Socket, limits: HttpLimits) -> HttpFrameResult
    let buffer: bytes = b""
    let total: int = 0
    let scanned: int = 0
    let terminator: int = -1
    let deadline: int = time_now_ms() + limits.header_timeout_ms

    // Phase 1: read until the header terminator arrives.
    while terminator < 0 do
        terminator = find_header_end_from(buffer, total, scanned)
        if terminator < 0 then
            if total >= limits.max_header_bytes then
                return frame_error(431, "Request Header Fields Too Large")
            end

            let remaining: int = deadline - time_now_ms()
            if remaining <= 0 then
                return frame_error(408, "Request Timeout")
            end
            settimeout(client, remaining)

            let res: NetResult = socket_recv(client, limits.read_chunk_bytes)
            if !res.ok then
                let status: int = recv_failure_status(res.error)
                if status == 0 then
                    return frame_silent()
                end
                return frame_error(status, "Request Timeout")
            end
            if res.count == 0 then
                if total == 0 then
                    return frame_silent()
                end
                return frame_error(400, "Bad Request")
            end

            scanned = total - 3
            if scanned < 0 then
                scanned = 0
            end
            buffer = buffer + slice(res.data, 0, res.count)
            total = total + res.count
        end
    end

    if terminator + 4 > limits.max_header_bytes then
        return frame_error(431, "Request Header Fields Too Large")
    end

    // Phase 2: validate the request line and headers.
    let head: HttpFrameResult = parse_request_head(slice(buffer, 0, terminator))
    if !head.ok then
        return head
    end
    let req: HttpRequest = head.request

    // Phase 3: resolve the declared body length.
    let framing: BodyLengthResult = resolve_body_length(req.headers, req.header_count, limits.max_body_bytes)
    if !framing.ok then
        return frame_error(framing.status, framing.message)
    end

    // Phase 4: read exactly the declared number of body bytes.
    let body_start: int = terminator + 4
    let have: int = total - body_start
    if have > framing.length then
        have = framing.length
    end
    let body: bytes = slice(buffer, body_start, body_start + have)

    if have < framing.length then
        let body_deadline: int = time_now_ms() + limits.body_timeout_ms
        while have < framing.length do
            let body_remaining: int = body_deadline - time_now_ms()
            if body_remaining <= 0 then
                return frame_error(408, "Request Timeout")
            end

            let want: int = framing.length - have
            if want > limits.read_chunk_bytes then
                want = limits.read_chunk_bytes
            end
            settimeout(client, body_remaining)

            let chunk: NetResult = socket_recv(client, want)
            if !chunk.ok then
                let chunk_status: int = recv_failure_status(chunk.error)
                if chunk_status == 0 then
                    return frame_silent()
                end
                return frame_error(chunk_status, "Request Timeout")
            end
            if chunk.count == 0 then
                return frame_error(400, "Bad Request")
            end

            body = body + slice(chunk.data, 0, chunk.count)
            have = have + chunk.count
        end
    end

    req.body = body
    return HttpFrameResult(true, req, 0, "", true)
end

// ============================================
// Handler Internal Logic
// ============================================

// This function runs in a separate routine (spawned)
func handle_client_connection(client: Socket, user_handler: func, limits: HttpLimits)
    defer socket_close(client)

    let frame: HttpFrameResult = read_request(client, limits)

    if !frame.ok then
        if frame.respond then
            let rejection: HttpResponse = response_error(frame.status, frame.message)
            let rejection_bytes: bytes = build_response(rejection.status_code, rejection.status_text, rejection.headers, rejection.header_count, rejection.body)
            send_all(client, rejection_bytes, limits.write_timeout_ms)
        end
        return
    end

    // Dispatch request to user handler. Handler must match expected signature.
    let response: HttpResponse = user_handler(frame.request)

    let resp_bytes: bytes = build_response(response.status_code, response.status_text, response.headers, response.header_count, response.body)
    send_all(client, resp_bytes, limits.write_timeout_ms)
end
```

Note `scanned = total - 3` is assigned **before** the buffer grows, so the next scan re-examines only the three bytes that could carry a split terminator. `want` is capped at the exact remaining need, so the body phase never over-reads.

- [ ] **Step 4: Fix `response_error` to declare a byte-exact length**

Replace the existing `response_error` with:

```noxy
func response_error(status: int, msg: string) -> HttpResponse
    let body: bytes = to_bytes(msg)
    let headers: string[64]
    headers[0] = "Content-Type: text/plain"
    headers[1] = "Content-Length: " + to_str(length(body))
    headers[2] = "Connection: close"

    return HttpResponse("HTTP/1.1", status, get_status_text(status), headers, 3, body)
end
```

`length` on a `string` counts runes while `length` on `bytes` counts bytes, so measuring the encoded body is what keeps a non-ASCII message correctly framed.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/vm/ -run 'TestServerFrames|TestServerReads|TestServerTreats|TestServerIgnores|TestServerParses|TestServerRejects|TestServerTimesOut|TestServerCloses' -v`

Expected: PASS, every subtest.

- [ ] **Step 6: Add the non-ASCII length regression test**

Append to `internal/vm/http_parser_framing_test.go`:

```go
func TestResponseErrorDeclaresByteLength(t *testing.T) {
	source := `use http_server select *
use http_parser select *
let r: HttpResponse = response_error(400, "requisição inválida")
test_report(r.headers[1] + "|" + to_str(length(r.body)))`
	captured := captureVMSource(t, source)
	got, ok := captured.Obj.(string)
	if !ok {
		t.Fatalf("test_report value = %#v, want string", captured)
	}
	if got != "Content-Length: 22|22" {
		t.Fatalf("response_error framing = %q, want %q", got, "Content-Length: 22|22")
	}
}
```

`requisição inválida` is 19 runes and 22 bytes in UTF-8, so a rune-count regression fails this test.

- [ ] **Step 7: Run the new test**

Run: `go test ./internal/vm/ -run TestResponseErrorDeclaresByteLength -v`

Expected: PASS.

- [ ] **Step 8: Run the whole suite under the race detector**

Run: `go build ./... && go vet ./... && go test ./internal/... && go test -race ./internal/vm/`

Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/stdlib/http_server.nx internal/vm/http_server_framing_test.go internal/vm/http_parser_framing_test.go
git commit -m "fix(http): read requests incrementally instead of one chunk

Resolve point 13 from PR #17. read_request reads the header block and the
Content-Length body to completion under per-phase deadlines and explicit
limits, rejects malformed framing with a real HTTP response, and closes
the socket through defer so a failing handler cannot leak it.
response_error now declares a byte-exact Content-Length.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Noxy end-to-end test

An example-suite test that runs the real server against the real client.

**Files:**
- Create: `noxy_examples/test_http_server.nx`

**Interfaces:**
- Consumes: `new_server`, `bind_server`, `serve`, `stop_server`, `response_text`, `response_json`, `response_404` from `http_server`; `get`, `post` from `http_client`.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Write the test script**

Create `noxy_examples/test_http_server.nx`:

```noxy
// Integration test: HTTP streaming server framing
use http_server select *
use http_client select *
use http_parser select *
use strings select *
use time select *

global passed: int = 0
global failed: int = 0

func check(name: string, condition: bool) -> void
    if condition then
        passed = passed + 1
        print("PASS: " + name)
    else
        failed = failed + 1
        print("FAIL: " + name)
    end
end

func handler(req: HttpRequest) -> HttpResponse
    if req.path == "/hello" then
        return response_text("Hello Noxy World")
    end
    if req.path == "/echo" then
        return response_text(to_str(req.body))
    end
    if req.path == "/size" then
        return response_text(to_str(length(req.body)))
    end
    if req.path == "/host" then
        return response_text(get_header(req.headers, req.header_count, "Host"))
    end
    return response_404()
end

global server: HttpServer = new_server("127.0.0.1", 0)

func run_server()
    serve(server, handler)
end

func main()
    if !bind_server(server) then
        print("FAIL: could not bind server")
        return
    end

    let base: string = "http://127.0.0.1:" + to_str(server.port)
    spawn(run_server)
    sleep(300)

    let hello: ClientResponse = get(base + "/hello")
    check("GET /hello succeeds", hello.ok)
    check("GET /hello body", hello.body == b"Hello Noxy World")

    let echoed: ClientResponse = post(base + "/echo", b"{\"msg\": \"noxy\"}")
    check("POST /echo succeeds", echoed.ok)
    check("POST /echo echoes the body", echoed.body == b"{\"msg\": \"noxy\"}")

    let large_body: bytes = to_bytes(repeat("n", 20000))
    let large: ClientResponse = post(base + "/size", large_body)
    check("POST large body succeeds", large.ok)
    check("POST large body is complete", large.body == b"20000")

    let host: ClientResponse = get(base + "/host")
    check("Host header keeps its port", host.body == to_bytes("127.0.0.1:" + to_str(server.port)))

    let missing: ClientResponse = get(base + "/nope")
    check("unknown path returns 404", missing.status_code == 404)

    stop_server(server)
    sleep(200)

    print("")
    print("passed=" + to_str(passed) + " failed=" + to_str(failed))
    if failed > 0 then
        print("HTTP SERVER TESTS FAILED")
    else
        print("HTTP SERVER TESTS OK")
    end
end

main()
```

The `/size` check is the direct regression for point 13: 20000 bytes exceeds the old single-read behavior's usable window once headers are counted, and it spans several `read_chunk_bytes` reads.

- [ ] **Step 2: Run it**

Run: `go run cmd/noxy/main.go noxy_examples/test_http_server.nx`

Expected: every line prints `PASS:` and the final line is `HTTP SERVER TESTS OK`.

- [ ] **Step 3: Verify it runs inside the example suites**

Run: `go run cmd/noxy/main.go noxy_examples/run_all_tests.nx`

Expected: the run completes and `test_http_server.nx` is counted among the passing examples. It is deliberately not added to the exclusion lists in `run_all_tests.nx` or `run_all_tests_concurrent.nx`.

- [ ] **Step 4: Run the concurrent suite**

Run: `go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add noxy_examples/test_http_server.nx
git commit -m "test(http): add end-to-end streaming server example

Drives the real server with the real client over an ephemeral port,
covering a 20 KB body that the previous single-read implementation could
not deliver, plus Host header preservation and 404 routing.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Documentation

**Files:**
- Create: `docs/HTTP_SERVER.md`
- Modify: `CHANGELOG.md`

**Interfaces:**
- Consumes: every public name produced in Tasks 1-6.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Read the current changelog format**

Run: `head -40 CHANGELOG.md`

Note the heading style and the most recent version entry so the new entry matches.

- [ ] **Step 2: Write `docs/HTTP_SERVER.md`**

Create `docs/HTTP_SERVER.md`:

````markdown
# HTTP Server

`http_server` implements an HTTP/1.1 origin server that frames each request
incrementally. It reads the header block and the `Content-Length` body to
completion, enforces explicit limits, bounds every phase with a deadline, and
answers an invalid request with a real HTTP response.

The server handles one request per connection and always responds with
`Connection: close`.

## Quick start

```noxy
use http_server select *
use http_parser select *

func handler(req: HttpRequest) -> HttpResponse
    if req.path == "/" then
        return response_text("Hello")
    end
    return response_404()
end

let server: HttpServer = new_server("127.0.0.1", 8080)
serve(server, handler)
```

## Binding

`bind_server(server)` binds the listener without entering the accept loop and
returns whether it succeeded. It writes the real bound port back to
`server.port`, so passing port `0` gives an ephemeral port the caller can read:

```noxy
let server: HttpServer = new_server("127.0.0.1", 0)
if bind_server(server) then
    print("listening on " + to_str(server.port))
    serve(server, handler)
end
```

`serve(server, handler)` binds automatically when the server is not already
bound, so the single-call form keeps working unchanged.

## Configuration

Assign the fields before calling `serve`. A non-positive value falls back to its
default, and `read_chunk_bytes` is clamped to `max_header_bytes`.

| Field | Default | Meaning |
|---|---|---|
| `max_header_bytes` | `16384` | maximum size of the header block, terminator included |
| `max_body_bytes` | `1048576` | maximum accepted `Content-Length` |
| `header_timeout_ms` | `5000` | total budget for receiving the header block |
| `body_timeout_ms` | `15000` | total budget for receiving the body |
| `write_timeout_ms` | `15000` | total budget for writing one response |
| `read_chunk_bytes` | `8192` | receive size per socket read |

```noxy
let server: HttpServer = new_server("0.0.0.0", 8080)
server.max_body_bytes = 8388608
server.header_timeout_ms = 3000
serve(server, handler)
```

The timeouts are the slowloris defense: they are absolute budgets per phase, so
a client that trickles one byte at a time still hits them.

## Framing contract

- The header block ends at the first `\r\n\r\n`. A lone `\n` is not a line
  terminator.
- The request line must have exactly three space-separated fields. The method
  is an RFC 9110 token of at most 64 characters; the target is origin-form,
  starts with `/`, is at most 2048 characters, and contains only visible ASCII;
  the version is `HTTP/1.0` or `HTTP/1.1`.
- A header name is a token of at most 256 characters with no whitespace before
  the `:`. Obsolete line folding is rejected rather than joined. At most 64
  header lines are accepted; the 65th is an error rather than a silent drop.
- A header value keeps every character after the first `:`, so
  `Host: example.com:8080` is read whole.
- The body length comes from `Content-Length` only. It must be 1 to 19 ASCII
  digits with no sign, decimal point, or list. A request with neither
  `Content-Length` nor `Transfer-Encoding` has no body, including a `POST`.
- `Transfer-Encoding` is not implemented; chunked requests are refused rather
  than mis-framed.
- Bytes arriving after the declared body length are discarded, because the
  connection closes after one response.

## Status codes for invalid requests

| Status | Condition |
|---|---|
| `400 Bad Request` | malformed request line or header, obsolete folding, whitespace before `:`, non-origin target, duplicate or non-digit `Content-Length`, `Transfer-Encoding` with `Content-Length`, connection ended mid-request |
| `408 Request Timeout` | the header or body budget expired |
| `413 Content Too Large` | declared `Content-Length` above `max_body_bytes` |
| `414 URI Too Long` | target longer than 2048 characters |
| `431 Request Header Fields Too Large` | header block at or above `max_header_bytes`, or more than 64 header lines |
| `501 Not Implemented` | `Transfer-Encoding` present |
| `505 HTTP Version Not Supported` | version other than `HTTP/1.0` or `HTTP/1.1` |

Every error response carries `Content-Type: text/plain`, a byte-exact
`Content-Length`, `Connection: close`, and the reason as its body. A connection
that closes before any request byte arrives receives no response at all.

## Responses

`response_ok`, `response_text`, `response_json`, `response_html`,
`response_error`, `response_404`, `response_500`, and `serve_static` keep their
signatures. Every response is written completely: a partial write resumes from
the transferred offset until the response is sent or `write_timeout_ms`
expires.

The connection is closed through `defer`, so a runtime error raised inside a
handler closes the socket instead of leaking it.

## Not supported

Keep-alive, request pipelining, chunked `Transfer-Encoding`,
`Expect: 100-continue`, HTTP/2, and TLS.
````

- [ ] **Step 3: Add the changelog entry**

Add an entry to `CHANGELOG.md` matching the file's existing format, containing:

```markdown
### Fixed
- **HTTP server reads only one chunk (point 13 from PR #17):** `http_server`
  now frames requests incrementally. It reads the header block and the
  `Content-Length` body to completion, so fragmented requests and bodies larger
  than one socket read are no longer truncated.

### Added
- **HTTP server limits and timeouts:** `max_header_bytes`, `max_body_bytes`,
  `header_timeout_ms`, `body_timeout_ms`, `write_timeout_ms`, and
  `read_chunk_bytes` on `HttpServer`, with slowloris protection through
  per-phase deadlines.
- **`bind_server(server)`:** binds without entering the accept loop and writes
  the real bound port back to `server.port`, making port `0` usable.
- **Coherent rejections:** invalid requests receive 400, 408, 413, 414, 431,
  501, or 505 with a byte-exact `Content-Length` instead of a bare disconnect.
- **`count_header(headers, count, name)`** in `http_parser`.

### Fixed
- **`get_header` truncated values containing a colon,** so
  `Host: example.com:8080` returned `example.com`.
- **`response_error` declared a rune count as `Content-Length`,** understating
  the length of any non-ASCII message.
- **A failing handler leaked its client socket;** the connection now closes
  through `defer`.
```

Merge the two `### Fixed` blocks into one if the changelog format requires a
single section per heading.

- [ ] **Step 4: Full validation**

Run each command and confirm it passes:

```bash
go build ./...
go vet ./...
go test ./internal/...
go test -race ./internal/vm/
go run cmd/noxy/main.go noxy_examples/run_all_tests_concurrent.nx
```

Expected: all pass, and the example suite reports the same or a higher pass
count than before the branch.

- [ ] **Step 5: Commit**

```bash
git add docs/HTTP_SERVER.md CHANGELOG.md
git commit -m "docs(http): document the streaming server contract

Framing rules, configuration fields and defaults, the status code each
invalid-request condition produces, and the explicit exclusions.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Pull request

**Files:**
- None modified.

- [ ] **Step 1: Push the branch**

```bash
git push -u origin feat/http-streaming-server
```

- [ ] **Step 2: Open the PR against `develop`**

```bash
gh pr create --base develop --title "feat/http-streaming-server - Frame HTTP requests incrementally" --body "$(cat <<'EOF'
## Summary
- Replace the HTTP server's single 64 KB socket read with incremental framing that reads the header block and the `Content-Length` body to completion.
- Enforce explicit per-server limits and per-phase deadlines, giving slowloris protection through absolute header, body, and write budgets.
- Validate framing strictly and answer invalid requests with real HTTP responses (400, 408, 413, 414, 431, 501, 505) instead of a bare disconnect.
- Write every response completely, resuming from the transferred offset after a partial write.
- Keep one request per connection with `Connection: close`, and keep every existing exported signature.

## Components
- `internal/stdlib/http_parser.nx`: resumable `find_header_end_from`, token and visible-ASCII validation, `parse_request_head`, `resolve_body_length`, `count_header`, corrected `get_header`, extended `get_status_text`.
- `internal/stdlib/http_server.nx`: `HttpLimits`, configuration fields and defaults on `HttpServer`, `bind_server`, `server_limits`, `read_request`, `send_all`, and a `defer`-closed connection lifecycle.
- `internal/vm/http_parser_framing_test.go`: Go tests for the pure framing primitives through the Noxy source harness.
- `internal/vm/http_server_framing_test.go`: Go tests running the Noxy server on the VM and driving it over raw TCP.
- `noxy_examples/test_http_server.nx`: end-to-end test with the real client.
- `docs/HTTP_SERVER.md`, `CHANGELOG.md`: framing contract and release notes.

## Related Issues
- Resolves point 13 from PR #17.

## Test Plan
- [x] Implementação
- [x] Testes unitários passando (`go test ./internal/...`)
- [x] Race detector limpo (`go test -race ./internal/vm/`)
- [x] Build e vet passando
- [x] Testado integrado (`noxy_examples/run_all_tests_concurrent.nx`)
- [ ] Revisão de código

## Follow-up Work
- Keep-alive and request pipelining.
- Chunked `Transfer-Encoding` decoding.
- `Expect: 100-continue`.
- `http_client` response framing by `Content-Length` instead of read-until-EOF.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Report the PR URL to the user**

---

## Notes for the implementer

**Why the resumable scan matters.** With a plain rescan from index `0`, a
request delivered in `n` segments scans the accumulated buffer `n` times. A
slow client sending 16 KB one byte at a time would cost roughly 134 million
byte comparisons inside the VM. Resuming at `total - 3` makes the total cost
linear in the header size.

**Why `remaining <= 0` is checked before `settimeout`.** `net_settimeout`
raises a synchronous runtime error for a non-positive timeout. Inside a spawned
connection routine that error terminates the routine, so the check must reject
first and let the normal 408 path answer the client.

**Why `to_int` is never trusted.** `to_int("+5")` is `5`, `to_int("5.5")` is
`5`, and `to_int("abc")` is `0`. A `Content-Length` resolved through `to_int`
alone would accept values that desynchronize framing, so `is_digit` — which is
ASCII-only and rejects the empty string — gates every conversion.

**Byte-based versus rune-based helpers.** `split`, `index_of`, `starts_with`,
`contains`, and `trim` operate on bytes; `substring`, `char_at`, and
`length(string)` operate on runes. Framing decisions use only the byte-based
helpers so a header carrying non-UTF-8 bytes cannot shift an offset. The
rune-based helpers appear only in ASCII-constrained validation, where a
non-ASCII rune fails the check and the request is rejected.
