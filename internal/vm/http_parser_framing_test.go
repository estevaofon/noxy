package vm

import (
	"fmt"
	"strings"
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

func TestIsTokenVisibleAsciiAndFieldText(t *testing.T) {
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
		{name: "plain header line is field text", source: "test_report(is_field_text(\"Host: example.com:8080\"))", want: true},
		{name: "empty is field text", source: "test_report(is_field_text(\"\"))", want: true},
		{name: "tab is field text", source: "test_report(is_field_text(\"X: a\\tb\"))", want: true},
		{name: "non ascii is field text", source: "test_report(is_field_text(\"X: caf\" + from_char_code(233)))", want: true},
		{name: "bare lf is not field text", source: "test_report(is_field_text(\"X: a\\nb\"))", want: false},
		{name: "bare cr is not field text", source: "test_report(is_field_text(\"X: a\\rb\"))", want: false},
		{name: "nul is not field text", source: "test_report(is_field_text(\"X: a\" + from_char_code(0) + \"b\"))", want: false},
		{name: "del is not field text", source: "test_report(is_field_text(\"X: a\" + from_char_code(127) + \"b\"))", want: false},
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

// noxyBytes renders a Go string as a Noxy bytes literal.
//
// A bytes literal accepts \xNN, so any byte — a control character, or a byte
// that is not valid UTF-8 — is written directly. That is why this returns
// b"..." rather than a string literal: parse_request_head takes bytes, and a
// string literal could not carry the invalid-UTF-8 cases the tests need.
func noxyBytes(s string) string {
	var b []byte
	b = append(b, 'b', '"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			b = append(b, '\\', '"')
		case c == '\\':
			b = append(b, '\\', '\\')
		case c == '\r':
			b = append(b, '\\', 'r')
		case c == '\n':
			b = append(b, '\\', 'n')
		case c == '\t':
			b = append(b, '\\', 't')
		case c < 0x20 || c >= 0x7F:
			b = append(b, []byte(fmt.Sprintf(`\x%02x`, c))...)
		default:
			b = append(b, c)
		}
	}
	return string(append(b, '"'))
}

func parseHeadStatus(t *testing.T, block string) int64 {
	t.Helper()
	source := "let head: HttpFrameResult = parse_request_head(" + noxyBytes(block) + ")\n" +
		"if head.ok then test_report(0) else test_report(head.status) end"
	return captureParserInt(t, source)
}

func TestParseRequestHeadAccepts(t *testing.T) {
	block := "POST /a/b?x=1&y=2 HTTP/1.1\r\nHost: example.com:8080\r\nContent-Length: 3"
	source := "let head: HttpFrameResult = parse_request_head(" + noxyBytes(block) + ")\n" +
		"test_report(head.request.method + \"|\" + head.request.path + \"|\" + head.request.query + \"|\" + head.request.version + \"|\" + to_str(head.request.header_count))"
	want := "POST|/a/b|x=1&y=2|HTTP/1.1|2"
	if got := captureParserString(t, source); got != want {
		t.Fatalf("parse_request_head = %q, want %q", got, want)
	}
}

func TestParseRequestHeadQueryKeepsLaterQuestionMarks(t *testing.T) {
	block := "GET /a?x=1?y=2 HTTP/1.1\r\nHost: a"
	source := "let head: HttpFrameResult = parse_request_head(" + noxyBytes(block) + ")\n" +
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
		{name: "bare lf in header value", block: "GET / HTTP/1.1\r\nX-Echo: a\nInjected: yes", want: 400},
		{name: "bare cr in header value", block: "GET / HTTP/1.1\r\nX-Echo: a\rInjected: yes", want: 400},
		{name: "nul in header value", block: "GET / HTTP/1.1\r\nX-Echo: a\x00b", want: 400},
		{name: "del in header value", block: "GET / HTTP/1.1\r\nX-Echo: a\x7fb", want: 400},
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

// A header block carrying a raw 0xFF is not valid UTF-8. Without the
// is_valid_utf8 gate, to_str raises and the caller never receives a status at
// all, so this test fails by erroring out rather than by returning a wrong
// number.
func TestParseRequestHeadRejectsNonUTF8HeaderBlock(t *testing.T) {
	if got := parseHeadStatus(t, "GET / HTTP/1.1\r\nX-Bad: \xff\xfe\r\nHost: a"); got != 400 {
		t.Fatalf("status = %d, want 400", got)
	}
}

func TestParseRequestRetainedGateReturnsDefaults(t *testing.T) {
	source := "let raw: bytes = " + noxyBytes("GET /x HTTP/1.1\r\nX-Bad: \xff\r\n\r\n") + "\n" +
		"let req: HttpRequest = parse_request(raw, length(raw))\n" +
		"test_report(req.path)"
	if got := captureParserString(t, source); got != "/" {
		t.Fatalf("path = %q, want the default %q", got, "/")
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
