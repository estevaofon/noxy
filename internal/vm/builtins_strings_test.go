package vm

import (
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
		{name: "index of normal byte offset", builtin: "strings_index_of", args: []value.Value{value.NewString("aéz"), value.NewString("z")}, want: value.NewInt(3)},
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
		{name: "ord unicode returns first utf8 byte", builtin: "ord", args: []value.Value{value.NewString("é")}, want: value.NewInt(195)},
		{name: "ord empty", builtin: "ord", args: []value.Value{value.NewString("")}, want: value.NewInt(0)},
		{name: "ord short", builtin: "ord", want: value.NewInt(0)},
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
