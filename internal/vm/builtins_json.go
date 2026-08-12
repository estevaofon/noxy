package vm

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"noxy-vm/internal/value"
)

func (vm *VM) defineJSONBuiltins() {
	vm.DefineNative("json_dumps", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewString("null")
		}
		goVal := jsonValToGo(args[0])
		bytes, err := json.Marshal(goVal)
		if err != nil {
			return value.NewString("null") // Or error?
		}
		return value.NewString(string(bytes))
	})

	vm.DefineNative("json_dumps_result", func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewNull()
		}
		resultType, ok := args[1].Obj.(*value.ObjStruct)
		if !ok {
			return value.NewNull()
		}
		result := value.NewInstance(resultType).Obj.(*value.ObjInstance)
		result.Fields["success"] = value.NewBool(false)
		result.Fields["data"] = value.NewString("")
		result.Fields["error"] = value.NewString(errJSONUnsupported.Error())

		goValue, err := strictJSONValToGo(args[0])
		if err != nil {
			result.Fields["error"] = value.NewString(err.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}
		encoded, err := json.Marshal(goValue)
		if err != nil {
			result.Fields["error"] = value.NewString(err.Error())
			return value.Value{Type: value.VAL_OBJ, Obj: result}
		}
		result.Fields["success"] = value.NewBool(true)
		result.Fields["data"] = value.NewString(string(encoded))
		result.Fields["error"] = value.NewString("")
		return value.Value{Type: value.VAL_OBJ, Obj: result}
	})

	// json_parse(str) -> Value
	vm.DefineNative("json_parse", func(args []value.Value) value.Value {
		if len(args) < 1 {
			return value.NewNull()
		}
		jsonStr := args[0].String()

		var result interface{}
		err := json.Unmarshal([]byte(jsonStr), &result)
		if err != nil {
			return value.NewNull()
		}
		return goValToNoxy(result)
	})

	// json_loads(str, target) -> Bool
	jsonLoadsSignature := value.NativeSignature{
		Arity: 2,
		Params: []value.ParamInfo{
			{IsRef: false, TypeName: "string"},
			{IsRef: true, TypeName: "ref any"},
		},
		ReturnType: "bool",
	}
	vm.DefineNativeWithSignature("json_loads", jsonLoadsSignature, func(args []value.Value) value.Value {
		if len(args) < 2 {
			return value.NewBool(false)
		}
		jsonStr := args[0].String()
		target := args[1]

		var result interface{}
		decoder := json.NewDecoder(strings.NewReader(jsonStr))
		decoder.UseNumber()
		if err := decoder.Decode(&result); err != nil {
			return value.NewBool(false)
		}
		var trailing interface{}
		if err := decoder.Decode(&trailing); err != io.EOF {
			return value.NewBool(false)
		}

		// Try to populate target
		if populateTarget(vm, target, result) {
			return value.NewBool(true)
		}
		return value.NewBool(false)
	})
}

// Helper: Convert Noxy Value to Go Interface for JSON Marshal
func jsonValToGo(v value.Value) interface{} {
	switch v.Type {
	case value.VAL_NULL:
		return nil
	case value.VAL_BOOL:
		return v.AsBool
	case value.VAL_INT:
		return v.AsInt
	case value.VAL_FLOAT:
		return v.AsFloat
	case value.VAL_OBJ:
		switch o := v.Obj.(type) {
		case string:
			return o
		case *value.ObjArray:
			arr := make([]interface{}, len(o.Elements))
			for i, el := range o.Elements {
				arr[i] = jsonValToGo(el)
			}
			return arr
		case *value.ObjMap:
			m := make(map[string]interface{})
			for k, val := range o.Data {
				keyStr := fmt.Sprintf("%v", k)
				m[keyStr] = jsonValToGo(val)
			}
			return m
		case *value.ObjInstance:
			m := make(map[string]interface{})
			for k, val := range o.Fields {
				m[k] = jsonValToGo(val)
			}
			return m
		case *value.ObjStruct:
			return o.Name
		}
	case value.VAL_BYTES:
		// Base64 encode bytes? Or generic string?
		return v.Obj.(string)
	}
	return v.String()
}

// Helper: Convert Go Interface to Noxy Value
func goValToNoxy(i interface{}) value.Value {
	if i == nil {
		return value.NewNull()
	}
	switch v := i.(type) {
	case bool:
		return value.NewBool(v)
	case float64:
		// JSON numbers are float64 by default
		// Try to see if it's an int
		if v == float64(int64(v)) {
			return value.NewInt(int64(v))
		}
		return value.NewFloat(v)
	case json.Number:
		if parsed, ok := exactJSONInt(v); ok {
			return value.NewInt(parsed)
		}
		if parsed, err := v.Float64(); err == nil {
			return value.NewFloat(parsed)
		}
		return value.NewNull()
	case string:
		return value.NewString(v)
	case []interface{}:
		arr := make([]value.Value, len(v))
		for idx, el := range v {
			arr[idx] = goValToNoxy(el)
		}
		return value.NewArray(arr)
	case map[string]interface{}:
		m := make(map[string]value.Value)
		for k, val := range v {
			m[k] = goValToNoxy(val)
		}
		return value.NewMapWithData(m)
	}
	return value.NewString(fmt.Sprintf("%v", i))
}

// Helper: Populate Target
func populateTarget(vm *VM, target value.Value, data interface{}) bool {
	if target.Type == value.VAL_REF {
		ref, ok := target.Obj.(*value.ObjRef)
		if !ok || ref == nil {
			return false
		}
		return populateRef(vm, ref, data)
	} else if target.Type == value.VAL_OBJ {
		// Populate Object In-Place
		return populateObj(vm, target, data)
	}
	// Cannot populate primitive value passed by value
	return false
}

func populateObj(vm *VM, currentVal value.Value, data interface{}) bool {
	commit, ok := prepareJSONMutation(vm, currentVal, nil, data, nil)
	if !ok {
		return false
	}
	commit()
	return true
}

// Helper: Populate a Reference with Go Data (Deeply)
func populateRef(vm *VM, ref *value.ObjRef, data interface{}) bool {
	currentVal, exists, store, err := vm.referenceStorage(ref)
	if err != nil || !exists || store == nil {
		return false
	}
	if ref.JSONDynamic.Load() {
		replacement, ok := dynamicJSONValue(data)
		if !ok {
			return false
		}
		store(replacement)
		return true
	}
	commit, ok := prepareJSONMutation(vm, currentVal, ref.TargetType.Load(), data, jsonSetter(store))
	if !ok {
		return false
	}
	commit()
	return true
}

func jsonReplacementCompatible(current, replacement value.Value) bool {
	if current.Type == value.VAL_NULL {
		return true
	}
	if current.Type != replacement.Type {
		return false
	}
	if current.Type != value.VAL_OBJ {
		return true
	}
	switch current.Obj.(type) {
	case string:
		_, ok := replacement.Obj.(string)
		return ok
	case *value.ObjArray:
		_, ok := replacement.Obj.(*value.ObjArray)
		return ok
	case *value.ObjMap:
		_, ok := replacement.Obj.(*value.ObjMap)
		return ok
	case *value.ObjInstance:
		_, ok := replacement.Obj.(*value.ObjInstance)
		return ok
	default:
		return false
	}
}
