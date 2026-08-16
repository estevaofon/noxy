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
