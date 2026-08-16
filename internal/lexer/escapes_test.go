package lexer

import (
	"strings"
	"testing"

	"noxy-vm/internal/token"
)

// firstToken lexes source and returns its first token, which for every case
// here is the literal under test.
func firstToken(t *testing.T, source string) token.Token {
	t.Helper()
	return New(source).NextToken()
}

func requireLiteral(t *testing.T, source string, wantType token.TokenType, want string) {
	t.Helper()
	got := firstToken(t, source)
	if got.Type != wantType {
		t.Fatalf("lexing %s produced %s (%q), want %s", source, got.Type, got.Literal, wantType)
	}
	if got.Literal != want {
		t.Fatalf("lexing %s produced %q, want %q", source, got.Literal, want)
	}
}

func requireIllegal(t *testing.T, source string, wantReason string) {
	t.Helper()
	got := firstToken(t, source)
	if got.Type != token.ILLEGAL {
		t.Fatalf("lexing %s produced %s (%q), want ILLEGAL", source, got.Type, got.Literal)
	}
	if !strings.Contains(got.Literal, wantReason) {
		t.Fatalf("lexing %s reported %q, want it to mention %q", source, got.Literal, wantReason)
	}
}

// TestUnicodeEscapeInString covers the escape three shipped examples already
// reach for. Before this change "\u001b[H" lexed to the literal 8-character
// text \u001b[H, so conway.nx, conway_random.nx and langtons_ant.nx emitted
// their ANSI sequences as visible garbage instead of clearing the screen.
func TestUnicodeEscapeInString(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "four digit escape", source: `"\u001b[H"`, want: "\x1b[H"},
		{name: "four digit uppercase hex", source: `"\u001B"`, want: "\x1b"},
		{name: "braced escape", source: `"\u{1b}"`, want: "\x1b"},
		{name: "braced multibyte", source: `"caf\u{e9}"`, want: "café"},
		{name: "braced astral", source: `"\u{1F600}"`, want: "\U0001F600"},
		{name: "braced nul", source: `"a\u{0}b"`, want: "a\x00b"},
		{name: "braced del", source: `"a\u{7f}b"`, want: "a\x7fb"},
		{name: "escape next to text", source: `"a\u0041b"`, want: "aAb"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireLiteral(t, test.source, token.STRING, test.want)
		})
	}
}

func TestUnicodeEscapeRejectsInvalidCodepoints(t *testing.T) {
	tests := []struct {
		name   string
		source string
		reason string
	}{
		{name: "high surrogate", source: `"\u{D800}"`, reason: "surrogate"},
		{name: "low surrogate", source: `"\uDFFF"`, reason: "surrogate"},
		{name: "above max codepoint", source: `"\u{110000}"`, reason: "range"},
		{name: "empty braces", source: `"\u{}"`, reason: "hex"},
		{name: "unterminated braces", source: `"\u{1b"`, reason: "unterminated"},
		{name: "too many digits", source: `"\u{1234567}"`, reason: "hex"},
		{name: "short four digit form", source: `"\u01"`, reason: "hex"},
		{name: "non hex in four digit form", source: `"\u00zz"`, reason: "hex"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireIllegal(t, test.source, test.reason)
		})
	}
}

// A \x escape in a string literal would let a source file construct a Noxy
// string holding invalid UTF-8, which is exactly the invariant to_str exists
// to protect. It is refused, and the message points at the escape that does
// work.
func TestHexEscapeIsRejectedInStrings(t *testing.T) {
	requireIllegal(t, `"a\xffb"`, `\u`)
	requireIllegal(t, `f"a\xffb"`, `\u`)
}

func TestHexEscapeInBytes(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "invalid utf8 byte", source: `b"a\xffb"`, want: "a\xffb"},
		{name: "nul byte", source: `b"a\x00b"`, want: "a\x00b"},
		{name: "uppercase hex", source: `b"\xFF"`, want: "\xff"},
		{name: "crlf pair", source: `b"\x0d\x0a"`, want: "\r\n"},
		{name: "unicode escape encodes utf8", source: `b"caf\u{e9}"`, want: "café"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireLiteral(t, test.source, token.BYTES, test.want)
		})
	}
}

func TestHexEscapeRejectsMalformedDigits(t *testing.T) {
	requireIllegal(t, `b"\xZZ"`, "hex")
	requireIllegal(t, `b"\xf"`, "hex")
}

// The escapes that already worked must keep working, and an unknown escape
// must keep its current permissive behaviour: the backslash is preserved. A
// Windows path such as "C:\path" relies on that, so tightening it would be a
// silent breaking change unrelated to this work.
func TestExistingEscapeBehaviourIsPreserved(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "newline", source: `"a\nb"`, want: "a\nb"},
		{name: "carriage return", source: `"a\rb"`, want: "a\rb"},
		{name: "tab", source: `"a\tb"`, want: "a\tb"},
		{name: "escaped quote", source: `"a\"b"`, want: `a"b`},
		{name: "escaped backslash", source: `"a\\b"`, want: `a\b`},
		{name: "unknown escape keeps backslash", source: `"C:\path"`, want: `C:\path`},
		{name: "backslash u without brace or digits", source: `"\users"`, want: `\users`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			requireLiteral(t, test.source, token.STRING, test.want)
		})
	}
}

func TestEscapesApplyToSingleQuotedAndFStrings(t *testing.T) {
	requireLiteral(t, `'\u0041'`, token.STRING, "A")
	requireLiteral(t, `f"\u0041"`, token.FSTRING, "A")
	requireLiteral(t, `b'\xff'`, token.BYTES, "\xff")
}

func TestUnterminatedLiteralsStillReported(t *testing.T) {
	requireIllegal(t, `"abc`, "unterminated")
	requireIllegal(t, `b"abc`, "unterminated")
	requireIllegal(t, `f"abc`, "unterminated")
}
