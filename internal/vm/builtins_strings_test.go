package vm

import (
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func TestStringBuiltinsScalarTables(t *testing.T) {
	machine := New()
	tests := []struct {
		name    string
		builtin string
		args    []value.Value
		want    value.Value
	}{
		{name: "contains normal", builtin: "strings_contains", args: []value.Value{value.NewString("banana"), value.NewString("nan")}, want: value.NewBool(true)},
		{name: "contains empty", builtin: "strings_contains", args: []value.Value{value.NewString(""), value.NewString("")}, want: value.NewBool(true)},
		{name: "contains short", builtin: "strings_contains", args: []value.Value{value.NewString("banana")}, want: value.NewBool(false)},
		{name: "starts with normal", builtin: "strings_starts_with", args: []value.Value{value.NewString("noxy vm"), value.NewString("noxy")}, want: value.NewBool(true)},
		{name: "starts with empty", builtin: "strings_starts_with", args: []value.Value{value.NewString(""), value.NewString("")}, want: value.NewBool(true)},
		{name: "starts with short", builtin: "strings_starts_with", want: value.NewBool(false)},
		{name: "ends with normal", builtin: "strings_ends_with", args: []value.Value{value.NewString("noxy.vm"), value.NewString(".vm")}, want: value.NewBool(true)},
		{name: "ends with empty", builtin: "strings_ends_with", args: []value.Value{value.NewString(""), value.NewString("")}, want: value.NewBool(true)},
		{name: "ends with short", builtin: "strings_ends_with", args: []value.Value{value.NewString("noxy")}, want: value.NewBool(false)},
		{name: "index of returns character index", builtin: "strings_index_of", args: []value.Value{value.NewString("aéz"), value.NewString("z")}, want: value.NewInt(2)},
		{name: "index of empty", builtin: "strings_index_of", args: []value.Value{value.NewString(""), value.NewString("")}, want: value.NewInt(0)},
		{name: "index of short", builtin: "strings_index_of", want: value.NewInt(-1)},
		{name: "count normal", builtin: "strings_count", args: []value.Value{value.NewString("banana"), value.NewString("an")}, want: value.NewInt(2)},
		{name: "count empty", builtin: "strings_count", args: []value.Value{value.NewString(""), value.NewString("")}, want: value.NewInt(1)},
		{name: "count short", builtin: "strings_count", args: []value.Value{value.NewString("banana")}, want: value.NewInt(0)},
		{name: "upper normal unicode", builtin: "strings_to_upper", args: []value.Value{value.NewString("Olá")}, want: value.NewString("OLÁ")},
		{name: "upper empty", builtin: "strings_to_upper", args: []value.Value{value.NewString("")}, want: value.NewString("")},
		{name: "upper short", builtin: "strings_to_upper", want: value.NewString("")},
		{name: "lower normal unicode", builtin: "strings_to_lower", args: []value.Value{value.NewString("OLÁ")}, want: value.NewString("olá")},
		{name: "lower empty", builtin: "strings_to_lower", args: []value.Value{value.NewString("")}, want: value.NewString("")},
		{name: "lower short", builtin: "strings_to_lower", want: value.NewString("")},
		{name: "trim normal", builtin: "strings_trim", args: []value.Value{value.NewString("\t Olá \n")}, want: value.NewString("Olá")},
		{name: "trim empty", builtin: "strings_trim", args: []value.Value{value.NewString("")}, want: value.NewString("")},
		{name: "trim short", builtin: "strings_trim", want: value.NewString("")},
		{name: "reverse normal unicode runes", builtin: "strings_reverse", args: []value.Value{value.NewString("aé🙂")}, want: value.NewString("🙂éa")},
		{name: "reverse empty", builtin: "strings_reverse", args: []value.Value{value.NewString("")}, want: value.NewString("")},
		{name: "reverse short", builtin: "strings_reverse", want: value.NewString("")},
		{name: "repeat normal", builtin: "strings_repeat", args: []value.Value{value.NewString("ab"), value.NewInt(3)}, want: value.NewString("ababab")},
		{name: "repeat empty", builtin: "strings_repeat", args: []value.Value{value.NewString(""), value.NewInt(3)}, want: value.NewString("")},
		{name: "repeat short", builtin: "strings_repeat", args: []value.Value{value.NewString("ab")}, want: value.NewString("")},
		{name: "replace normal", builtin: "strings_replace", args: []value.Value{value.NewString("one one"), value.NewString("one"), value.NewString("two")}, want: value.NewString("two two")},
		{name: "replace empty old inserts", builtin: "strings_replace", args: []value.Value{value.NewString(""), value.NewString(""), value.NewString("x")}, want: value.NewString("x")},
		{name: "replace short", builtin: "strings_replace", args: []value.Value{value.NewString("one"), value.NewString("one")}, want: value.NewString("")},
		{name: "replace first normal", builtin: "strings_replace_first", args: []value.Value{value.NewString("one one"), value.NewString("one"), value.NewString("two")}, want: value.NewString("two one")},
		{name: "replace first empty old inserts", builtin: "strings_replace_first", args: []value.Value{value.NewString(""), value.NewString(""), value.NewString("x")}, want: value.NewString("x")},
		{name: "replace first short", builtin: "strings_replace_first", want: value.NewString("")},
		{name: "pad left normal counts bytes", builtin: "strings_pad_left", args: []value.Value{value.NewString("é"), value.NewInt(3), value.NewString("_")}, want: value.NewString("_é")},
		{name: "pad left empty repeats whole pad string", builtin: "strings_pad_left", args: []value.Value{value.NewString(""), value.NewInt(3), value.NewString("ab")}, want: value.NewString("ababab")},
		{name: "pad left short", builtin: "strings_pad_left", args: []value.Value{value.NewString("x")}, want: value.NewString("")},
		{name: "join count normal", builtin: "strings_join_count", args: []value.Value{value.NewArray([]value.Value{value.NewString("a"), value.NewString("é"), value.NewString("c")}), value.NewString("-"), value.NewInt(2)}, want: value.NewString("a-é")},
		{name: "join count empty", builtin: "strings_join_count", args: []value.Value{value.NewArray(nil), value.NewString(","), value.NewInt(2)}, want: value.NewString("")},
		{name: "join count short", builtin: "strings_join_count", args: []value.Value{value.NewArray(nil)}, want: value.NewString("")},
		{name: "substring normal unicode runes", builtin: "strings_substring", args: []value.Value{value.NewString("aé🙂z"), value.NewInt(1), value.NewInt(3)}, want: value.NewString("é🙂")},
		{name: "substring end exclusive", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(0), value.NewInt(2)}, want: value.NewString("He")},
		{name: "substring mid range", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(1), value.NewInt(4)}, want: value.NewString("ell")},
		{name: "substring to end clamped", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(3), value.NewInt(100)}, want: value.NewString("lo")},
		{name: "substring start equals end", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(2), value.NewInt(2)}, want: value.NewString("")},
		{name: "substring start greater than end", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(3), value.NewInt(1)}, want: value.NewString("")},
		{name: "substring negative start from end", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(-2), value.NewInt(5)}, want: value.NewString("lo")},
		{name: "substring negative end from end", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(0), value.NewInt(-1)}, want: value.NewString("Hell")},
		{name: "substring both negative", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(-3), value.NewInt(-1)}, want: value.NewString("ll")},
		{name: "substring negative overflow clamped", builtin: "strings_substring", args: []value.Value{value.NewString("Hello"), value.NewInt(-100), value.NewInt(3)}, want: value.NewString("Hel")},
		{name: "substring empty string", builtin: "strings_substring", args: []value.Value{value.NewString(""), value.NewInt(0), value.NewInt(1)}, want: value.NewString("")},
		{name: "substring short args", builtin: "strings_substring", args: []value.Value{value.NewString("abc"), value.NewInt(1)}, want: value.NewString("")},
		{name: "is empty normal", builtin: "strings_is_empty", args: []value.Value{value.NewString("x")}, want: value.NewBool(false)},
		{name: "is empty empty", builtin: "strings_is_empty", args: []value.Value{value.NewString("")}, want: value.NewBool(true)},
		{name: "is empty short sentinel", builtin: "strings_is_empty", want: value.NewBool(true)},
		{name: "is digit normal", builtin: "strings_is_digit", args: []value.Value{value.NewString("0123")}, want: value.NewBool(true)},
		{name: "is digit empty", builtin: "strings_is_digit", args: []value.Value{value.NewString("")}, want: value.NewBool(false)},
		{name: "is digit short", builtin: "strings_is_digit", want: value.NewBool(false)},
		{name: "is digit unicode is ascii only", builtin: "strings_is_digit", args: []value.Value{value.NewString("１２")}, want: value.NewBool(false)},
		{name: "is alpha normal", builtin: "strings_is_alpha", args: []value.Value{value.NewString("Noxy")}, want: value.NewBool(true)},
		{name: "is alpha empty", builtin: "strings_is_alpha", args: []value.Value{value.NewString("")}, want: value.NewBool(false)},
		{name: "is alpha short", builtin: "strings_is_alpha", want: value.NewBool(false)},
		{name: "is alpha unicode is ascii only", builtin: "strings_is_alpha", args: []value.Value{value.NewString("Olá")}, want: value.NewBool(false)},
		{name: "is alnum normal", builtin: "strings_is_alnum", args: []value.Value{value.NewString("Noxy42")}, want: value.NewBool(true)},
		{name: "is alnum empty", builtin: "strings_is_alnum", args: []value.Value{value.NewString("")}, want: value.NewBool(false)},
		{name: "is alnum short", builtin: "strings_is_alnum", want: value.NewBool(false)},
		{name: "is alnum unicode is ascii only", builtin: "strings_is_alnum", args: []value.Value{value.NewString("Nóxy42")}, want: value.NewBool(false)},
		{name: "is space normal", builtin: "strings_is_space", args: []value.Value{value.NewString(" \t\n\r")}, want: value.NewBool(true)},
		{name: "is space empty", builtin: "strings_is_space", args: []value.Value{value.NewString("")}, want: value.NewBool(false)},
		{name: "is space short", builtin: "strings_is_space", want: value.NewBool(false)},
		{name: "char at normal unicode runes", builtin: "strings_char_at", args: []value.Value{value.NewString("aé🙂"), value.NewInt(2)}, want: value.NewString("🙂")},
		{name: "char at empty", builtin: "strings_char_at", args: []value.Value{value.NewString(""), value.NewInt(0)}, want: value.NewString("")},
		{name: "char at short", builtin: "strings_char_at", args: []value.Value{value.NewString("abc")}, want: value.NewString("")},
		{name: "from char code normal unicode rune", builtin: "strings_from_char_code", args: []value.Value{value.NewInt(0x1f642)}, want: value.NewString("🙂")},
		{name: "from char code zero produces nul", builtin: "strings_from_char_code", args: []value.Value{value.NewInt(0)}, want: value.NewString("\x00")},
		{name: "from char code short", builtin: "strings_from_char_code", want: value.NewString("")},
		{name: "ord normal", builtin: "ord", args: []value.Value{value.NewString("A")}, want: value.NewInt(65)},
		{name: "ord unicode returns code point", builtin: "ord", args: []value.Value{value.NewString("é")}, want: value.NewInt(233)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, tt.builtin, tt.args...), tt.want)
		})
	}
}

func TestStringsSplitBuiltin(t *testing.T) {
	machine := New()
	splitResult := value.NewStruct("SplitResult", []string{"count", "parts"})
	tests := []struct {
		name      string
		args      []value.Value
		wantParts []string
		wantNull  bool
	}{
		{name: "normal", args: []value.Value{value.NewString("a,é"), value.NewString(","), splitResult}, wantParts: []string{"a", "é"}},
		{name: "empty", args: []value.Value{value.NewString(""), value.NewString(","), splitResult}, wantParts: []string{""}},
		{name: "short", args: []value.Value{value.NewString("a,b"), value.NewString(",")}, wantNull: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "strings_split", tt.args...)
			if tt.wantNull {
				assertBuiltinValue(t, got, value.NewNull())
				return
			}
			if got.Type != value.VAL_OBJ {
				t.Fatalf("type = %v, want object", got.Type)
			}
			instance, ok := got.Obj.(*value.ObjInstance)
			if !ok {
				t.Fatalf("payload = %#v, want *value.ObjInstance", got.Obj)
			}
			if instance.Struct != splitResult.Obj.(*value.ObjStruct) {
				t.Fatal("split result does not use the supplied struct definition")
			}
			assertBuiltinValue(t, instance.Fields["count"], value.NewInt(int64(len(tt.wantParts))))
			parts := requireBuiltinArray(t, instance.Fields["parts"])
			if len(parts.Elements) != len(tt.wantParts) {
				t.Fatalf("part count = %d, want %d", len(parts.Elements), len(tt.wantParts))
			}
			for i, want := range tt.wantParts {
				assertBuiltinValue(t, parts.Elements[i], value.NewString(want))
			}
		})
	}
}

func TestIndexOfReturnsCharacterIndex(t *testing.T) {
	machine := New()
	tests := []struct {
		name    string
		subject string
		needle  string
		want    int64
	}{
		{name: "ascii matches byte offset", subject: "abc:def", needle: ":", want: 3},
		{name: "multibyte before match", subject: "münchen.de/path", needle: "/", want: 10},
		{name: "multibyte needle", subject: "café bar", needle: "é", want: 3},
		{name: "emoji before match", subject: "\U0001F600x:y", needle: ":", want: 2},
		{name: "absent", subject: "abc", needle: "z", want: -1},
		{name: "empty needle", subject: "abc", needle: "", want: 0},
		{name: "match at start", subject: ":abc", needle: ":", want: 0},
		{name: "empty subject", subject: "", needle: "a", want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "strings_index_of", value.NewString(test.subject), value.NewString(test.needle))
			if got.Type != value.VAL_INT || got.Int() != test.want {
				t.Fatalf("strings_index_of(%q, %q) = %#v, want %d", test.subject, test.needle, got, test.want)
			}
		})
	}
}

func TestIndexOfComposesWithSubstring(t *testing.T) {
	source := `use strings select *
let line: string = "München: Bayern"
let name: string = substring(line, 0, index_of(line, ":"))
test_report(name)`
	captured := captureVMSource(t, source)
	got, ok := captured.Obj.(string)
	if !ok || got != "München" {
		t.Fatalf("substring with index_of = %#v, want %q", captured, "München")
	}
}

func TestParseUrlKeepsMultibyteHostSeparateFromPath(t *testing.T) {
	source := "use http_parser select *\n" +
		"let u: HttpUrl = parse_url(\"http://münchen.de/path\")\n" +
		"test_report(u.host + \"|\" + u.path)"
	captured := captureVMSource(t, source)
	got, ok := captured.Obj.(string)
	if !ok || got != "münchen.de|/path" {
		t.Fatalf("parse_url = %#v, want %q", captured, "münchen.de|/path")
	}
}

func TestStringNativesRejectBytes(t *testing.T) {
	machine := New()
	text := value.NewString("x")
	number := value.NewInt(1)
	payload := value.NewBytes("hello")

	tests := []struct {
		native string
		args   []value.Value
	}{
		{native: "strings_contains", args: []value.Value{payload, text}},
		{native: "strings_contains", args: []value.Value{text, payload}},
		{native: "strings_starts_with", args: []value.Value{payload, text}},
		{native: "strings_ends_with", args: []value.Value{payload, text}},
		{native: "strings_index_of", args: []value.Value{payload, text}},
		{native: "strings_index_of", args: []value.Value{text, payload}},
		{native: "strings_count", args: []value.Value{payload, text}},
		{native: "strings_to_upper", args: []value.Value{payload}},
		{native: "strings_to_lower", args: []value.Value{payload}},
		{native: "strings_trim", args: []value.Value{payload}},
		{native: "strings_reverse", args: []value.Value{payload}},
		{native: "strings_repeat", args: []value.Value{payload, number}},
		{native: "strings_replace", args: []value.Value{payload, text, text}},
		{native: "strings_replace_first", args: []value.Value{payload, text, text}},
		{native: "strings_pad_left", args: []value.Value{payload, number, text}},
		{native: "strings_substring", args: []value.Value{payload, number, number}},
		{native: "strings_is_empty", args: []value.Value{payload}},
		{native: "strings_is_digit", args: []value.Value{payload}},
		{native: "strings_is_alpha", args: []value.Value{payload}},
		{native: "strings_is_alnum", args: []value.Value{payload}},
		{native: "strings_is_space", args: []value.Value{payload}},
		{native: "strings_char_at", args: []value.Value{payload, number}},
		{native: "ord", args: []value.Value{payload}},
	}
	for _, test := range tests {
		t.Run(test.native, func(t *testing.T) {
			_, err := requireBuiltin(t, machine, test.native).Invoke(machine, test.args)
			if err == nil {
				t.Fatalf("%s accepted a bytes argument", test.native)
			}
			if !strings.Contains(err.Error(), "expected string, got bytes") {
				t.Fatalf("message = %q, want it to name the type mismatch", err.Error())
			}
			if !strings.Contains(err.Error(), "to_str") {
				t.Fatalf("message = %q, want it to point at to_str", err.Error())
			}
		})
	}
}

func TestStringNativesStillAcceptStrings(t *testing.T) {
	machine := New()
	got := callBuiltin(t, machine, "strings_contains", value.NewString("hello"), value.NewString("ell"))
	if got.Type != value.VAL_BOOL || !got.Bool() {
		t.Fatalf("strings_contains = %#v, want true", got)
	}
}

func TestSplitRejectsBytesButAcceptsItsStructArgument(t *testing.T) {
	source := `use strings select *
let parts: SplitResult = split(to_str(b"a,b"), ",")
test_report(parts.count)`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_INT || captured.Int() != 2 {
		t.Fatalf("split after explicit to_str = %#v, want 2", captured)
	}
}

func TestOrdReturnsCodePoint(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{name: "ascii", input: "A", want: 65},
		{name: "latin1 supplement", input: "é", want: 233},
		{name: "cjk", input: "中", want: 20013},
		{name: "emoji", input: "\U0001F600", want: 128512},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "ord", value.NewString(test.input))
			if got.Type != value.VAL_INT || got.Int() != test.want {
				t.Fatalf("ord(%q) = %#v, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestOrdRoundTripsWithFromCharCode(t *testing.T) {
	machine := New()
	for _, code := range []int64{65, 233, 20013, 128512} {
		character := callBuiltin(t, machine, "strings_from_char_code", value.NewInt(code))
		back := callBuiltin(t, machine, "ord", character)
		if back.Type != value.VAL_INT || back.Int() != code {
			t.Fatalf("ord(from_char_code(%d)) = %#v, want %d", code, back, code)
		}
	}
}

func TestOrdRequiresExactlyOneCharacter(t *testing.T) {
	machine := New()
	for _, test := range []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "two characters", input: "ab"},
		{name: "two multibyte characters", input: "éé"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := requireBuiltin(t, machine, "ord").Invoke(machine, []value.Value{value.NewString(test.input)}); err == nil {
				t.Fatalf("ord(%q) did not fail", test.input)
			}
		})
	}
}

func TestCharCodeIsExportedByStrings(t *testing.T) {
	source := `use strings select *
test_report(char_code("é") == 233 && from_char_code(233) == "é")`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || !captured.Bool() {
		t.Fatalf("char_code round trip = %#v, want true", captured)
	}
}

func TestIsValidUTF8(t *testing.T) {
	machine := New()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "ascii", input: "hello", want: true},
		{name: "empty", input: "", want: true},
		{name: "multibyte", input: "café", want: true},
		{name: "emoji", input: "\U0001F600", want: true},
		{name: "lone 0xFF", input: "h\xffi", want: false},
		{name: "truncated multibyte", input: "caf\xc3", want: false},
		{name: "bare continuation byte", input: "\x80", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "strings_is_valid_utf8", value.NewBytes(test.input))
			if got.Type != value.VAL_BOOL || got.Bool() != test.want {
				t.Fatalf("strings_is_valid_utf8(%q) = %#v, want %v", test.input, got, test.want)
			}
		})
	}
}

func TestIsValidUTF8RejectsStringThroughModuleImport(t *testing.T) {
	// This supersedes a prior version of this test that sidestepped the real
	// call path: `use strings select *` erases is_valid_utf8's static
	// `b: bytes` parameter type at its call site (every `use` import path
	// records the imported name with a nil type in c.globals — see
	// compiler.predeclareImport / the UseStmt case in compiler.Compile), so
	// the compiler's static argument-type check never fires here. The
	// previous test worked around that by declaring `func is_valid_utf8(b:
	// bytes)` inline in the same compiled unit (where
	// predeclareGlobalBindings does register a real *ast.FunctionType),
	// which correctly demonstrated the compiler check exists but never
	// exercised what a real caller of `use strings select *` experiences.
	// Through the real module-import path, the string silently reached the
	// native, which checked only VAL_BYTES and otherwise returned false —
	// so `is_valid_utf8("text")` printed false instead of raising a type
	// error. The fix guards strings_is_valid_utf8 itself with
	// requireBytesArgument (the mirror image of requireTextArgument, which
	// already protects every other string native, e.g. ord/char_code, the
	// same way). This test exercises the real path and must now observe a
	// raised error, not a silently wrong answer.
	source := `use strings select *
test_report(is_valid_utf8("text"))`
	machine := New()
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) == 1 {
			captured = args[0]
		}
		return value.NewNull()
	})
	// Task 12 (§8): `use strings select *` now predeclares is_valid_utf8 with
	// its declared `b: bytes` type instead of erasing it to nil, so this
	// mismatch is caught at compile time rather than reaching the native at
	// runtime — same message ("expected bytes, got string"), earlier stage.
	// interpretOrCompileErr accepts either, since the fix documented above
	// (requireBytesArgument) is now backed up, not superseded, by a static
	// check.
	err := interpretOrCompileErr(t, machine, source)
	if err == nil {
		t.Fatalf("is_valid_utf8(\"text\") through use strings select * returned %#v with no error; want a raised type error", captured)
	}
	if !strings.Contains(err.Error(), "expected bytes, got string") {
		t.Fatalf("error = %q, want it to name the type mismatch", err.Error())
	}
}

func TestIsValidUTF8AcceptsBytesFromNoxy(t *testing.T) {
	source := `use strings select *
test_report(is_valid_utf8(b"café") && !is_valid_utf8(to_bytes([104, 255, 105])))`
	captured := captureVMSource(t, source)
	if captured.Type != value.VAL_BOOL || !captured.Bool() {
		t.Fatalf("is_valid_utf8 through the module = %#v, want true", captured)
	}
}
