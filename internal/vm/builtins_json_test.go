package vm

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"noxy-vm/internal/value"
)

func TestStrictJSONValToGoAcceptsJSONDomain(t *testing.T) {
	nested := value.NewMap()
	setTestMap(nested.Obj.(*value.ObjMap), "city", value.NewString("Cuiabá"))
	document := value.NewMap()
	setTestMap(document.Obj.(*value.ObjMap), "empty", value.NewString(""))
	setTestMap(document.Obj.(*value.ObjMap), "int", value.NewInt(30))
	setTestMap(document.Obj.(*value.ObjMap), "minimum", value.NewInt(-9223372036854775807-1))
	setTestMap(document.Obj.(*value.ObjMap), "maximum", value.NewInt(9223372036854775807))
	setTestMap(document.Obj.(*value.ObjMap), "float", value.NewFloat(1.25))
	setTestMap(document.Obj.(*value.ObjMap), "active", value.NewBool(true))
	setTestMap(document.Obj.(*value.ObjMap), "nothing", value.NewNull())
	setTestMap(document.Obj.(*value.ObjMap), "items", value.NewArray(
		[]value.Value{value.NewString("Noxy"), value.NewInt(2), nested},
	))

	got, err := strictJSONValToGo(document)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{
		"empty": "", "int": int64(30),
		"minimum": int64(-9223372036854775807 - 1),
		"maximum": int64(9223372036854775807),
		"float":   1.25, "active": true,
		"nothing": nil,
		"items":   []interface{}{"Noxy", int64(2), map[string]interface{}{"city": "Cuiabá"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strict conversion = %#v, want %#v", got, want)
	}
}

func TestStrictJSONValToGoRejectsNonJSONValues(t *testing.T) {
	definition := value.NewStruct("Point", []string{"x"})
	instance := value.NewInstance(definition.Obj.(*value.ObjStruct))
	badKeyMap := value.NewMap()
	setTestMap(badKeyMap.Obj.(*value.ObjMap), int64(7), value.NewString("seven"))
	tests := []struct {
		name  string
		input value.Value
	}{
		{"bytes", value.NewBytes("raw")},
		{"struct definition", definition},
		{"struct instance", instance},
		{"function", value.NewFunction("noop", 0, 0, nil, nil, nil)},
		{"native", value.NewNative("noop", func([]value.Value) value.Value { return value.NewNull() })},
		{"channel", value.NewChannel(1)},
		{"wait group", value.NewWaitGroup()},
		{"reference", value.Value{Type: value.VAL_REF, Obj: &value.ObjRef{}}},
		{"non-string key", badKeyMap},
		{"nan", value.NewFloat(math.NaN())},
		{"positive infinity", value.NewFloat(math.Inf(1))},
		{"negative infinity", value.NewFloat(math.Inf(-1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := strictJSONValToGo(tt.input); err == nil {
				t.Fatal("strict conversion accepted a non-JSON value")
			}
		})
	}
}

func TestStrictJSONValToGoRejectsTaskHandle(t *testing.T) {
	if _, err := strictJSONValToGo(value.NewTask()); err != errJSONUnsupported {
		t.Fatalf("task conversion error = %v, want %v", err, errJSONUnsupported)
	}
}

func TestStrictJSONValToGoRejectsInvalidUTF8Strings(t *testing.T) {
	invalidValue := value.NewString(string([]byte{0xff}))
	invalidKey := value.NewMap()
	setTestMap(invalidKey.Obj.(*value.ObjMap), string([]byte{0xff}), value.NewBool(true))
	tests := []struct {
		name  string
		input value.Value
	}{
		{name: "value", input: invalidValue},
		{name: "map key", input: invalidKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := strictJSONValToGo(tt.input); err == nil {
				t.Fatal("strict conversion accepted invalid UTF-8")
			}
		})
	}
}

func TestStrictJSONValToGoRejectsCyclesAndAcceptsSharedChildren(t *testing.T) {
	arrayCycle := value.NewArray(nil)
	arrayCycle.Obj.(*value.ObjArray).Elements = []value.Value{arrayCycle}
	if _, err := strictJSONValToGo(arrayCycle); err == nil {
		t.Fatal("array cycle was accepted")
	}

	mapCycle := value.NewMap()
	setTestMap(mapCycle.Obj.(*value.ObjMap), "self", mapCycle)
	if _, err := strictJSONValToGo(mapCycle); err == nil {
		t.Fatal("map cycle was accepted")
	}

	left := value.NewMap()
	right := value.NewArray(nil)
	setTestMap(left.Obj.(*value.ObjMap), "right", right)
	right.Obj.(*value.ObjArray).Elements = []value.Value{left}
	if _, err := strictJSONValToGo(left); err == nil {
		t.Fatal("indirect cycle was accepted")
	}

	child := value.NewMap()
	setTestMap(child.Obj.(*value.ObjMap), "value", value.NewInt(1))
	root := value.NewArray([]value.Value{child, child})
	if _, err := strictJSONValToGo(root); err != nil {
		t.Fatalf("shared acyclic child rejected: %v", err)
	}
}

func TestStrictJSONValToGoRejectsTypedNilContainers(t *testing.T) {
	var nilArray *value.ObjArray
	var nilMap *value.ObjMap
	tests := []struct {
		name  string
		input value.Value
	}{
		{"typed nil array", value.Value{Type: value.VAL_OBJ, Obj: nilArray}},
		{"typed nil map", value.Value{Type: value.VAL_OBJ, Obj: nilMap}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := strictJSONValToGo(tt.input); err == nil {
				t.Fatal("strict conversion accepted a typed nil container")
			}
		})
	}
}

func requireBuiltinMap(t *testing.T, got value.Value) *value.ObjMap {
	t.Helper()
	if got.Type != value.VAL_OBJ {
		t.Fatalf("type = %v, want object", got.Type)
	}
	mapping, ok := got.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("payload = %#v, want *value.ObjMap", got.Obj)
	}
	return mapping
}

func TestJSONDumpsBuiltin(t *testing.T) {
	machine := New()
	mapValue := value.NewMap()
	setTestMap(mapValue.Obj.(*value.ObjMap), "name", value.NewString("Noxy"))
	setTestMap(mapValue.Obj.(*value.ObjMap), int64(7), value.NewBool(true))
	definition := value.NewStruct("Point", []string{"x", "label"})
	instance := value.NewInstance(definition.Obj.(*value.ObjStruct))
	instance.Obj.(*value.ObjInstance).MustSet("x", value.NewInt(3))
	instance.Obj.(*value.ObjInstance).MustSet("label", value.NewString("p"))

	tests := []struct {
		name string
		args []value.Value
		want string
	}{
		{name: "null", args: []value.Value{value.NewNull()}, want: "null"},
		{name: "bool", args: []value.Value{value.NewBool(true)}, want: "true"},
		{name: "int", args: []value.Value{value.NewInt(42)}, want: "42"},
		{name: "float", args: []value.Value{value.NewFloat(1.25)}, want: "1.25"},
		{name: "string", args: []value.Value{value.NewString("noxy")}, want: `"noxy"`},
		{name: "bytes become string", args: []value.Value{value.NewBytes("abc")}, want: `"abc"`},
		{name: "array", args: []value.Value{value.NewArray([]value.Value{value.NewInt(1), value.NewString("two"), value.NewNull()})}, want: `[1,"two",null]`},
		{name: "map keys stringify", args: []value.Value{mapValue}, want: `{"7":true,"name":"Noxy"}`},
		{name: "struct instance", args: []value.Value{instance}, want: `{"label":"p","x":3}`},
		{name: "struct definition becomes name", args: []value.Value{definition}, want: `"Point"`},
		{name: "short args sentinel", want: "null"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, "json_dumps", tt.args...), value.NewString(tt.want))
		})
	}
}

func TestJSONDumpsResultBuiltinReportsSuccessAndFailure(t *testing.T) {
	machine := New()
	resultType := value.NewStruct("EncodeResult", []string{"success", "data", "error"})
	valid := value.NewMap()
	setTestMap(valid.Obj.(*value.ObjMap), "name", value.NewString("Noxy"))
	success := callBuiltin(t, machine, "json_dumps_result", valid, resultType)
	successFields := success.Obj.(*value.ObjInstance).Snapshot()
	assertBuiltinValue(t, successFields["success"], value.NewBool(true))
	assertBuiltinValue(t, successFields["data"], value.NewString("{\"name\":\"Noxy\"}"))
	assertBuiltinValue(t, successFields["error"], value.NewString(""))

	invalid := value.NewMap()
	setTestMap(invalid.Obj.(*value.ObjMap), "raw", value.NewBytes("abc"))
	failure := callBuiltin(t, machine, "json_dumps_result", invalid, resultType)
	failureFields := failure.Obj.(*value.ObjInstance).Snapshot()
	assertBuiltinValue(t, failureFields["success"], value.NewBool(false))
	assertBuiltinValue(t, failureFields["data"], value.NewString(""))
	if failureFields["error"].String() == "" {
		t.Fatal("strict encoder returned an empty error")
	}
}

func TestJSONDumpsResultBuiltinRejectsInvalidStrictValues(t *testing.T) {
	machine := New()
	resultType := value.NewStruct("EncodeResult", []string{"success", "data", "error"})
	invalidKey := value.NewMap()
	setTestMap(invalidKey.Obj.(*value.ObjMap), string([]byte{0xff}), value.NewBool(true))
	arrayCycle := value.NewArray(nil)
	arrayCycle.Obj.(*value.ObjArray).Elements = []value.Value{arrayCycle}
	mapCycle := value.NewMap()
	setTestMap(mapCycle.Obj.(*value.ObjMap), "self", mapCycle)
	tests := []struct {
		name  string
		input value.Value
	}{
		{name: "invalid UTF-8 string", input: value.NewString(string([]byte{0xff}))},
		{name: "invalid UTF-8 map key", input: invalidKey},
		{name: "NaN", input: value.NewFloat(math.NaN())},
		{name: "array cycle", input: arrayCycle},
		{name: "map cycle", input: mapCycle},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failure := callBuiltin(t, machine, "json_dumps_result", tt.input, resultType)
			fields := failure.Obj.(*value.ObjInstance).Snapshot()
			assertBuiltinValue(t, fields["success"], value.NewBool(false))
			assertBuiltinValue(t, fields["data"], value.NewString(""))
			if fields["error"].String() == "" {
				t.Fatal("strict encoder returned an empty error")
			}
		})
	}
}

func TestJSONLoadsBuiltinRejectsInvalidUTF8WithoutMutation(t *testing.T) {
	machine := New()
	target := value.NewMap()
	targetMap := target.Obj.(*value.ObjMap)
	setTestMap(targetMap, "sentinel", value.NewString("keep"))
	invalidJSON := value.NewString(string([]byte{'{', '"', 'n', 'a', 'm', 'e', '"', ':', '"', 0xff, '"', '}'}))

	result := callBuiltin(t, machine, "json_loads", invalidJSON, target)
	assertBuiltinValue(t, result, value.NewBool(false))
	if targetMap.Len() != 1 {
		t.Fatalf("target mutated after invalid JSON: %#v", targetMap.Snapshot())
	}
	assertBuiltinValue(t, requireTestMapValue(t, targetMap, "sentinel"), value.NewString("keep"))
	if _, exists := targetMap.Get("name"); exists {
		t.Fatal("target gained a field from invalid JSON")
	}
}

func TestJSONParseBuiltin(t *testing.T) {
	machine := New()
	scalarTests := []struct {
		name string
		json string
		want value.Value
	}{
		{name: "null", json: "null", want: value.NewNull()},
		{name: "bool", json: "true", want: value.NewBool(true)},
		{name: "integer number", json: "42", want: value.NewInt(42)},
		{name: "floating number", json: "1.25", want: value.NewFloat(1.25)},
		{name: "string", json: `"noxy"`, want: value.NewString("noxy")},
		{name: "invalid sentinel", json: "{", want: value.NewNull()},
	}
	for _, tt := range scalarTests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, callBuiltin(t, machine, "json_parse", value.NewString(tt.json)), tt.want)
		})
	}
	assertBuiltinValue(t, callBuiltin(t, machine, "json_parse"), value.NewNull())

	array := callBuiltin(t, machine, "json_parse", value.NewString(`[1,"two",false,null]`))
	parsedArray := requireBuiltinArray(t, array)
	if len(parsedArray.Elements) != 4 {
		t.Fatalf("parsed array length = %d, want 4", len(parsedArray.Elements))
	}
	assertBuiltinValue(t, parsedArray.Elements[0], value.NewInt(1))
	assertBuiltinValue(t, parsedArray.Elements[1], value.NewString("two"))
	assertBuiltinValue(t, parsedArray.Elements[2], value.NewBool(false))
	assertBuiltinValue(t, parsedArray.Elements[3], value.NewNull())

	parsedMap := requireBuiltinMap(t, callBuiltin(t, machine, "json_parse", value.NewString(`{"a":1,"nested":{"ok":true}}`)))
	assertBuiltinValue(t, requireTestMapValue(t, parsedMap, "a"), value.NewInt(1))
	nested := requireBuiltinMap(t, requireTestMapValue(t, parsedMap, "nested"))
	assertBuiltinValue(t, requireTestMapValue(t, nested, "ok"), value.NewBool(true))
}

func TestJSONValToGo(t *testing.T) {
	mapValue := value.NewMap()
	setTestMap(mapValue.Obj.(*value.ObjMap), "name", value.NewString("Noxy"))
	setTestMap(mapValue.Obj.(*value.ObjMap), int64(7), value.NewBool(true))
	definition := value.NewStruct("Point", []string{"x"})
	instance := value.NewInstance(definition.Obj.(*value.ObjStruct))
	instance.Obj.(*value.ObjInstance).MustSet("x", value.NewInt(3))

	tests := []struct {
		name  string
		input value.Value
		want  interface{}
	}{
		{name: "null", input: value.NewNull(), want: nil},
		{name: "bool", input: value.NewBool(true), want: true},
		{name: "int", input: value.NewInt(42), want: int64(42)},
		{name: "float", input: value.NewFloat(1.25), want: 1.25},
		{name: "string", input: value.NewString("noxy"), want: "noxy"},
		{name: "bytes", input: value.NewBytes("abc"), want: "abc"},
		{name: "array", input: value.NewArray([]value.Value{value.NewInt(1), value.NewString("two")}), want: []interface{}{int64(1), "two"}},
		{name: "map", input: mapValue, want: map[string]interface{}{"7": true, "name": "Noxy"}},
		{name: "struct instance", input: instance, want: map[string]interface{}{"x": int64(3)}},
		{name: "struct definition", input: definition, want: "Point"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonValToGo(tt.input); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("jsonValToGo(%#v) = %#v, want %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestGoValToNoxy(t *testing.T) {
	scalarTests := []struct {
		name  string
		input interface{}
		want  value.Value
	}{
		{name: "nil", input: nil, want: value.NewNull()},
		{name: "bool", input: true, want: value.NewBool(true)},
		{name: "integral float64 becomes int", input: float64(42), want: value.NewInt(42)},
		{name: "fractional float64", input: 1.25, want: value.NewFloat(1.25)},
		{name: "json integer number", input: json.Number("9223372036854775807"), want: value.NewInt(9223372036854775807)},
		{name: "json fractional number", input: json.Number("1.25"), want: value.NewFloat(1.25)},
		{name: "invalid json number sentinel", input: json.Number("not-a-number"), want: value.NewNull()},
		{name: "string", input: "noxy", want: value.NewString("noxy")},
		{name: "unsupported value stringifies", input: int(7), want: value.NewString("7")},
	}
	for _, tt := range scalarTests {
		t.Run(tt.name, func(t *testing.T) {
			assertBuiltinValue(t, goValToNoxy(tt.input), tt.want)
		})
	}

	array := requireBuiltinArray(t, goValToNoxy([]interface{}{float64(1), "two", nil}))
	assertBuiltinValue(t, array.Elements[0], value.NewInt(1))
	assertBuiltinValue(t, array.Elements[1], value.NewString("two"))
	assertBuiltinValue(t, array.Elements[2], value.NewNull())

	mapping := requireBuiltinMap(t, goValToNoxy(map[string]interface{}{"answer": float64(42), "ok": true}))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "answer"), value.NewInt(42))
	assertBuiltinValue(t, requireTestMapValue(t, mapping, "ok"), value.NewBool(true))
}

func TestJSONReplacementCompatible(t *testing.T) {
	pointDefinition := value.NewStruct("Point", []string{"x"})
	otherDefinition := value.NewStruct("Other", []string{"name"})
	point := value.NewInstance(pointDefinition.Obj.(*value.ObjStruct))
	other := value.NewInstance(otherDefinition.Obj.(*value.ObjStruct))
	tests := []struct {
		name        string
		current     value.Value
		replacement value.Value
		want        bool
	}{
		{name: "null accepts scalar", current: value.NewNull(), replacement: value.NewInt(1), want: true},
		{name: "same scalar type", current: value.NewInt(1), replacement: value.NewInt(2), want: true},
		{name: "different scalar type", current: value.NewInt(1), replacement: value.NewFloat(1), want: false},
		{name: "strings", current: value.NewString("a"), replacement: value.NewString("b"), want: true},
		{name: "string and array", current: value.NewString("a"), replacement: value.NewArray(nil), want: false},
		{name: "arrays", current: value.NewArray(nil), replacement: value.NewArray([]value.Value{value.NewInt(1)}), want: true},
		{name: "array and map", current: value.NewArray(nil), replacement: value.NewMap(), want: false},
		{name: "maps", current: value.NewMap(), replacement: value.NewMap(), want: true},
		{name: "map and instance", current: value.NewMap(), replacement: point, want: false},
		{name: "instances ignore nominal struct", current: point, replacement: other, want: true},
		{name: "instance and map", current: point, replacement: value.NewMap(), want: false},
		{name: "struct definitions are not replaceable", current: pointDefinition, replacement: otherDefinition, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jsonReplacementCompatible(tt.current, tt.replacement); got != tt.want {
				t.Fatalf("jsonReplacementCompatible(%#v, %#v) = %t, want %t", tt.current, tt.replacement, got, tt.want)
			}
		})
	}
}
