package vm

import (
	"encoding/json"
	"reflect"
	"testing"

	"noxy-vm/internal/value"
)

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
	mapValue.Obj.(*value.ObjMap).Data["name"] = value.NewString("Noxy")
	mapValue.Obj.(*value.ObjMap).Data[int64(7)] = value.NewBool(true)
	definition := value.NewStruct("Point", []string{"x", "label"})
	instance := value.NewInstance(definition.Obj.(*value.ObjStruct))
	instance.Obj.(*value.ObjInstance).Fields["x"] = value.NewInt(3)
	instance.Obj.(*value.ObjInstance).Fields["label"] = value.NewString("p")

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
	assertBuiltinValue(t, parsedMap.Data["a"], value.NewInt(1))
	nested := requireBuiltinMap(t, parsedMap.Data["nested"])
	assertBuiltinValue(t, nested.Data["ok"], value.NewBool(true))
}

func TestJSONValToGo(t *testing.T) {
	mapValue := value.NewMap()
	mapValue.Obj.(*value.ObjMap).Data["name"] = value.NewString("Noxy")
	mapValue.Obj.(*value.ObjMap).Data[int64(7)] = value.NewBool(true)
	definition := value.NewStruct("Point", []string{"x"})
	instance := value.NewInstance(definition.Obj.(*value.ObjStruct))
	instance.Obj.(*value.ObjInstance).Fields["x"] = value.NewInt(3)

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
	assertBuiltinValue(t, mapping.Data["answer"], value.NewInt(42))
	assertBuiltinValue(t, mapping.Data["ok"], value.NewBool(true))
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
