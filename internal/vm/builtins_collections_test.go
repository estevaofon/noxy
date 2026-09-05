package vm

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/estevaofon/noxy/internal/value"
)

func pointerReference(target *value.Value) value.Value {
	return value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{RefType: value.REF_PTR, Ptr: target}}
}

func requireBuiltinArray(t *testing.T, got value.Value) *value.ObjArray {
	t.Helper()
	if got.Type != value.VAL_OBJ {
		t.Fatalf("type = %v, want object", got.Type)
	}
	array, ok := got.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("payload = %#v, want *value.ObjArray", got.Obj)
	}
	return array
}

func assertBuiltinArray(t *testing.T, got value.Value, want []value.Value) {
	t.Helper()
	array := requireBuiltinArray(t, got)
	if len(array.Elements) != len(want) {
		t.Fatalf("array length = %d, want %d: %#v", len(array.Elements), len(want), array.Elements)
	}
	for i := range want {
		t.Run(fmt.Sprintf("element_%d", i), func(t *testing.T) {
			assertBuiltinValue(t, array.Elements[i], want[i])
		})
	}
}

func TestLengthAndKeysBuiltins(t *testing.T) {
	machine := New()
	mapValue := value.NewMap()
	mapObject := mapValue.Obj.(*value.ObjMap)
	setTestMap(mapObject, "name", value.NewString("Noxy"))
	setTestMap(mapObject, int64(7), value.NewBool(true))
	setTestMap(mapObject, false, value.NewInt(99))

	lengthTests := []struct {
		name string
		arg  value.Value
		want int64
	}{
		{name: "array", arg: value.NewArray([]value.Value{value.NewInt(1), value.NewInt(2)}), want: 2},
		{name: "empty array", arg: value.NewArray(nil), want: 0},
		{name: "map", arg: mapValue, want: 3},
		{name: "empty map", arg: value.NewMap(), want: 0},
		{name: "unicode string counts runes", arg: value.NewString("aé🙂"), want: 3},
		{name: "empty string", arg: value.NewString(""), want: 0},
		{name: "bytes count raw bytes", arg: value.NewBytes("aé🙂"), want: 7},
		{name: "empty bytes", arg: value.NewBytes(""), want: 0},
		{name: "invalid type sentinel", arg: value.NewInt(42), want: 0},
	}
	for _, tt := range lengthTests {
		t.Run("length "+tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, "length", tt.arg), value.NewInt(tt.want))
		})
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "length"), value.NewInt(0))

	keys := requireBuiltinArray(t, callBuiltin(t, machine, "keys", mapValue))
	actualKeys := make([]string, 0, len(keys.Elements))
	for _, key := range keys.Elements {
		switch key.Type {
		case value.VAL_INT:
			actualKeys = append(actualKeys, fmt.Sprintf("int:%d", key.Int()))
		case value.VAL_OBJ:
			keyString, ok := key.Obj.(string)
			if !ok {
				t.Fatalf("unexpected object key payload %#v", key.Obj)
			}
			actualKeys = append(actualKeys, "string:"+keyString)
		default:
			t.Fatalf("unexpected key type %v", key.Type)
		}
	}
	sort.Strings(actualKeys)
	wantKeys := []string{"int:7", "string:name"}
	if fmt.Sprint(actualKeys) != fmt.Sprint(wantKeys) {
		t.Fatalf("keys = %v, want %v", actualKeys, wantKeys)
	}
	assertBuiltinArray(t, callBuiltin(t, machine, "keys", value.NewMap()), nil)
	assertBuiltinArray(t, callBuiltin(t, machine, "keys", value.NewArray(nil)), nil)
	assertBuiltinArray(t, callBuiltin(t, machine, "keys"), nil)
}

func TestMutatingCollectionBuiltinsUseReferencedTarget(t *testing.T) {
	machine := New()

	storedMap := value.NewMap()
	mapObject := storedMap.Obj.(*value.ObjMap)
	setTestMap(mapObject, "remove", value.NewInt(1))
	setTestMap(mapObject, "keep", value.NewInt(2))
	setTestMap(mapObject, int64(3), value.NewString("three"))
	mapRef := pointerReference(&storedMap)
	assertBuiltinValue(t, callBuiltin(t, machine, "delete", mapRef, value.NewString("remove")), value.NewNull())
	if _, exists := storedMap.Obj.(*value.ObjMap).Get("remove"); exists {
		t.Fatal("delete did not mutate the referenced map")
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "delete", mapRef, value.NewInt(3)), value.NewNull())
	if _, exists := storedMap.Obj.(*value.ObjMap).Get(int64(3)); exists {
		t.Fatal("delete did not remove an integer key")
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "delete", mapRef, value.NewBool(true)), value.NewNull())
	if _, exists := storedMap.Obj.(*value.ObjMap).Get("keep"); !exists {
		t.Fatal("delete with an unsupported key changed the map")
	}
	plainMap := value.NewMapWithData(map[string]value.Value{"still": value.NewInt(1)})
	assertBuiltinValue(t, callBuiltin(t, machine, "delete", plainMap, value.NewString("still")), value.NewNull())
	if _, exists := plainMap.Obj.(*value.ObjMap).Get("still"); !exists {
		t.Fatal("delete mutated a non-reference argument")
	}

	storedArray := value.NewArray([]value.Value{value.NewInt(1)})
	arrayRef := pointerReference(&storedArray)
	assertBuiltinValue(t, callBuiltin(t, machine, "append", arrayRef, value.NewInt(2)), value.NewNull())
	assertBuiltinArray(t, storedArray, []value.Value{value.NewInt(1), value.NewInt(2)})
	popped := callBuiltin(t, machine, "pop", arrayRef)
	assertBuiltinValue(t, popped, value.NewInt(2))
	assertBuiltinArray(t, storedArray, []value.Value{value.NewInt(1)})
	assertBuiltinValue(t, callBuiltin(t, machine, "pop", arrayRef), value.NewInt(1))
	assertBuiltinArray(t, storedArray, nil)
	// Issue #126 (regra da #121): pop em array vazio e erro, nao null sentinela.
	if _, err := requireBuiltin(t, machine, "pop").Invoke(machine, []value.Value{arrayRef}); err == nil || !strings.Contains(err.Error(), "pop from empty array") {
		t.Fatalf("pop on empty array: err = %v, want pop from empty array", err)
	}

	invalidArray := value.NewArray([]value.Value{value.NewInt(9)})
	assertBuiltinValue(t, callBuiltin(t, machine, "append", invalidArray, value.NewInt(10)), value.NewNull())
	assertBuiltinArray(t, invalidArray, []value.Value{value.NewInt(9)})
	// Issue #126 (regra da #121): pop sobre argumento nao-ref e erro (o texto
	// vem de unicizeThroughRefValue), nao null sentinela.
	if _, err := requireBuiltin(t, machine, "pop").Invoke(machine, []value.Value{invalidArray}); err == nil {
		t.Fatalf("pop on non-ref argument: err = %v, want an error", err)
	}
	assertBuiltinArray(t, invalidArray, []value.Value{value.NewInt(9)})
	assertBuiltinValue(t, callBuiltin(t, machine, "append", arrayRef), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "delete", mapRef), value.NewNull())
}

// Issue #126 item 4: pop com indice opcional (Python list.pop([i])) e
// swap_remove (Rust Vec::swap_remove). Devolvem o elemento; posicao
// inexistente e erro, como indexar.
func TestPopWithIndexAndSwapRemoveBuiltins(t *testing.T) {
	machine := New()
	stored := value.NewArray([]value.Value{value.NewInt(10), value.NewInt(20), value.NewInt(30), value.NewInt(40)})
	ref := pointerReference(&stored)

	assertBuiltinValue(t, callBuiltin(t, machine, "pop", ref, value.NewInt(1)), value.NewInt(20))
	assertBuiltinArray(t, stored, []value.Value{value.NewInt(10), value.NewInt(30), value.NewInt(40)})

	assertBuiltinValue(t, callBuiltin(t, machine, "swap_remove", ref, value.NewInt(0)), value.NewInt(10))
	assertBuiltinArray(t, stored, []value.Value{value.NewInt(40), value.NewInt(30)})

	assertBuiltinValue(t, callBuiltin(t, machine, "pop", ref), value.NewInt(30))
	assertBuiltinValue(t, callBuiltin(t, machine, "swap_remove", ref, value.NewInt(0)), value.NewInt(40))
	assertBuiltinArray(t, stored, nil)

	for _, name := range []string{"pop", "swap_remove"} {
		stored = value.NewArray([]value.Value{value.NewInt(1)})
		ref = pointerReference(&stored)
		for _, idx := range []int64{1, -1} {
			_, err := requireBuiltin(t, machine, name).Invoke(machine, []value.Value{ref, value.NewInt(idx)})
			if err == nil || !strings.Contains(err.Error(), "array index out of bounds") {
				t.Fatalf("%s(%d): err = %v, want array index out of bounds", name, idx, err)
			}
		}
		assertBuiltinArray(t, stored, []value.Value{value.NewInt(1)})
		if _, err := requireBuiltin(t, machine, name).Invoke(machine, []value.Value{ref, value.NewString("0")}); err == nil || !strings.Contains(err.Error(), "index must be an int, got string") {
			t.Fatalf("%s with string index: err = %v", name, err)
		}
	}
	if _, err := requireBuiltin(t, machine, "pop").Invoke(machine, []value.Value{ref, value.NewInt(0), value.NewInt(0)}); err == nil || !strings.Contains(err.Error(), "pop: expects 1 or 2 arguments, got 3") {
		t.Fatalf("pop arity: err = %v", err)
	}
	if _, err := requireBuiltin(t, machine, "swap_remove").Invoke(machine, []value.Value{ref}); err == nil || !strings.Contains(err.Error(), "swap_remove: expects exactly 2 arguments, got 1") {
		t.Fatalf("swap_remove arity: err = %v", err)
	}
}

func TestSliceBuiltin(t *testing.T) {
	machine := New()

	stringTests := []struct {
		name       string
		sequence   value.Value
		start, end int64
		want       value.Value
	}{
		{name: "unicode string uses rune indexes", sequence: value.NewString("aé🙂z"), start: 1, end: 3, want: value.NewString("é🙂")},
		{name: "string clamps boundaries", sequence: value.NewString("abc"), start: -2, end: 99, want: value.NewString("abc")},
		{name: "string reversed range", sequence: value.NewString("abc"), start: 2, end: 1, want: value.NewString("")},
		{name: "empty string", sequence: value.NewString(""), start: 0, end: 1, want: value.NewString("")},
		// issue #66 item 2: ramo ASCII por byte == ramo por rune
		{name: "ascii string mid", sequence: value.NewString("item_12345"), start: 5, end: 6, want: value.NewString("1")},
		{name: "ascii string clamps", sequence: value.NewString("abc"), start: -2, end: 99, want: value.NewString("abc")},
		{name: "ascii string reversed", sequence: value.NewString("abc"), start: 2, end: 1, want: value.NewString("")},
		{name: "ascii string end at n", sequence: value.NewString("abc"), start: 2, end: 3, want: value.NewString("c")},
		{name: "accent string end clamp", sequence: value.NewString("café"), start: 3, end: 10, want: value.NewString("é")},
		{name: "bytes use byte indexes", sequence: value.NewBytes("aéz"), start: 1, end: 3, want: value.NewBytes("é")},
		{name: "bytes reversed range", sequence: value.NewBytes("abc"), start: 2, end: 1, want: value.NewBytes("")},
		{name: "empty bytes", sequence: value.NewBytes(""), start: -1, end: 1, want: value.NewBytes("")},
	}
	for _, tt := range stringTests {
		t.Run(tt.name, func(t *testing.T) {
			got := callBuiltin(t, machine, "slice", tt.sequence, value.NewInt(tt.start), value.NewInt(tt.end))
			assertBuiltinValue(t, got, tt.want)
		})
	}

	array := value.NewArray([]value.Value{value.NewInt(1), value.NewInt(2), value.NewInt(3)})
	assertBuiltinArray(t, callBuiltin(t, machine, "slice", array, value.NewInt(1), value.NewInt(3)), []value.Value{value.NewInt(2), value.NewInt(3)})
	assertBuiltinArray(t, callBuiltin(t, machine, "slice", array, value.NewInt(-1), value.NewInt(99)), []value.Value{value.NewInt(1), value.NewInt(2), value.NewInt(3)})
	assertBuiltinArray(t, callBuiltin(t, machine, "slice", array, value.NewInt(2), value.NewInt(1)), nil)
	assertBuiltinArray(t, callBuiltin(t, machine, "slice", value.NewArray(nil), value.NewInt(0), value.NewInt(1)), nil)
	assertBuiltinValue(t, callBuiltin(t, machine, "slice", value.NewMap(), value.NewInt(0), value.NewInt(1)), value.NewNull())
	assertBuiltinValue(t, callBuiltin(t, machine, "slice", value.NewString("abc"), value.NewInt(0)), value.NewNull())
}

func TestCollectionMembershipBuiltins(t *testing.T) {
	machine := New()
	array := value.NewArray([]value.Value{value.NewInt(1), value.NewString("one"), value.NewBool(true)})
	tests := []struct {
		name string
		args []value.Value
		want bool
	}{
		{name: "contains int", args: []value.Value{array, value.NewInt(1)}, want: true},
		{name: "contains string", args: []value.Value{array, value.NewString("one")}, want: true},
		{name: "does not contain", args: []value.Value{array, value.NewInt(2)}, want: false},
		{name: "empty array", args: []value.Value{value.NewArray(nil), value.NewInt(1)}, want: false},
		{name: "invalid collection", args: []value.Value{value.NewString("one"), value.NewString("o")}, want: false},
		{name: "short args", args: []value.Value{array}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, "contains", tt.args...), value.NewBool(tt.want))
		})
	}

	mapValue := value.NewMap()
	mapObject := mapValue.Obj.(*value.ObjMap)
	setTestMap(mapObject, "one", value.NewInt(1))
	setTestMap(mapObject, int64(2), value.NewString("two"))
	hasKeyTests := []struct {
		name string
		args []value.Value
		want bool
	}{
		{name: "string key", args: []value.Value{mapValue, value.NewString("one")}, want: true},
		{name: "int key", args: []value.Value{mapValue, value.NewInt(2)}, want: true},
		{name: "missing key", args: []value.Value{mapValue, value.NewString("missing")}, want: false},
		{name: "empty map", args: []value.Value{value.NewMap(), value.NewString("one")}, want: false},
		{name: "invalid key type", args: []value.Value{mapValue, value.NewBool(true)}, want: false},
		{name: "invalid collection", args: []value.Value{value.NewArray(nil), value.NewString("one")}, want: false},
		{name: "short args", args: []value.Value{mapValue}, want: false},
	}
	for _, tt := range hasKeyTests {
		t.Run("has key "+tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, "has_key", tt.args...), value.NewBool(tt.want))
		})
	}
}

func TestToBytesBuiltin(t *testing.T) {
	machine := New()
	tests := []struct {
		name string
		args []value.Value
		want string
	}{
		{name: "string", args: []value.Value{value.NewString("aé")}, want: "aé"},
		{name: "empty string", args: []value.Value{value.NewString("")}, want: ""},
		{name: "int", args: []value.Value{value.NewInt(255)}, want: "\xff"},
		{name: "int truncates to byte", args: []value.Value{value.NewInt(256)}, want: "\x00"},
		{name: "int array", args: []value.Value{value.NewArray([]value.Value{value.NewInt(65), value.NewInt(255)})}, want: "A\xff"},
		{name: "non-int array element becomes zero byte", args: []value.Value{value.NewArray([]value.Value{value.NewInt(65), value.NewString("B")})}, want: "A\x00"},
		{name: "empty array", args: []value.Value{value.NewArray(nil)}, want: ""},
		{name: "bytes input is invalid sentinel", args: []value.Value{value.NewBytes("abc")}, want: ""},
		{name: "map input is invalid sentinel", args: []value.Value{value.NewMap()}, want: ""},
		{name: "short args", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, "to_bytes", tt.args...), value.NewBytes(tt.want))
		})
	}
}
