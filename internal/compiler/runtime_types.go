package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func (c *Compiler) runtimeTypeInfo(t ast.NoxyType) *value.RuntimeTypeInfo {
	info, ok := c.runtimeTypeInfoWithStructs(t, make(map[string]*value.RuntimeTypeInfo))
	if !ok {
		return nil
	}
	return info
}

func (c *Compiler) emitRuntimeValueType(t ast.NoxyType) error {
	if !containsChannelType(t, make(map[string]bool), c.structs) {
		return nil
	}
	runtimeType := c.runtimeTypeInfo(t)
	if runtimeType == nil {
		return nil
	}
	typeConstant := c.makeConstant(value.NewRuntimeTypeInfo(runtimeType))
	if typeConstant > 65535 {
		return fmt.Errorf("[line %d] too many constants for runtime value metadata", c.currentLine)
	}
	c.emitByte(byte(chunk.OP_MARK_RUNTIME_VALUE_TYPE))
	c.emitByte(byte((typeConstant >> 8) & 0xff))
	c.emitByte(byte(typeConstant & 0xff))
	return nil
}

func containsChannelType(t ast.NoxyType, visiting map[string]bool, structs map[string]*ast.StructStatement) bool {
	switch typed := t.(type) {
	case *ast.ChanType:
		return true
	case *ast.ArrayType:
		return containsChannelType(typed.ElementType, visiting, structs)
	case *ast.MapType:
		return containsChannelType(typed.KeyType, visiting, structs) || containsChannelType(typed.ValueType, visiting, structs)
	case *ast.RefType:
		return containsChannelType(typed.ElementType, visiting, structs)
	case *ast.FunctionType:
		// Callable signatures carry their own immutable runtime schema. Channels
		// named by that signature are not values embedded in the callable object.
		return false
	case *ast.PrimitiveType:
		definition, ok := structs[typed.Name]
		if !ok || visiting[typed.Name] {
			return false
		}
		visiting[typed.Name] = true
		defer delete(visiting, typed.Name)
		for _, field := range definition.FieldsList {
			if containsChannelType(field.Type, visiting, structs) {
				return true
			}
		}
	}
	return false
}

// runtimeTypeInfoWithStructs returns ok only for a complete runtime schema.
// A composite carrying an unknown child would otherwise turn that child into
// an accidental dynamic wildcard at typed native boundaries.
func (c *Compiler) runtimeTypeInfoWithStructs(t ast.NoxyType, structs map[string]*value.RuntimeTypeInfo) (*value.RuntimeTypeInfo, bool) {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		switch typed.Name {
		case "any":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_ANY}, true
		case "null":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_NULL}, true
		case "bool":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_BOOL}, true
		case "int":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_INT}, true
		case "float":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_FLOAT}, true
		case "string":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}, true
		case "bytes":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_BYTES}, true
		case "void":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_VOID}, true
		case "func":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_CALLABLE, CallableBare: true}, true
		}
		definition, ok := c.structs[typed.Name]
		if !ok {
			return nil, false
		}
		if existing, ok := structs[typed.Name]; ok {
			return existing, true
		}
		info := &value.RuntimeTypeInfo{
			Kind:   value.TYPE_STRUCT,
			Name:   typed.Name,
			Fields: make(map[string]*value.RuntimeTypeInfo, len(definition.FieldsList)),
		}
		structs[typed.Name] = info
		for _, field := range definition.FieldsList {
			fieldInfo, complete := c.runtimeTypeInfoWithStructs(field.Type, structs)
			if !complete {
				delete(structs, typed.Name)
				return nil, false
			}
			info.Fields[field.Name] = fieldInfo
		}
		return info, true
	case *ast.ArrayType:
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: element, Size: typed.Size}, true
	case *ast.MapType:
		key, ok := c.runtimeTypeInfoWithStructs(typed.KeyType, structs)
		if !ok {
			return nil, false
		}
		mapValue, ok := c.runtimeTypeInfoWithStructs(typed.ValueType, structs)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_MAP, Key: key, Value: mapValue}, true
	case *ast.RefType:
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_REF, Element: element}, true
	case *ast.ChanType:
		element, ok := c.runtimeTypeInfoWithStructs(typed.ElementType, structs)
		if !ok {
			return nil, false
		}
		return &value.RuntimeTypeInfo{Kind: value.TYPE_CHANNEL, Element: element}, true
	case *ast.FunctionType:
		info := &value.RuntimeTypeInfo{
			Kind:       value.TYPE_CALLABLE,
			Params:     make([]*value.RuntimeTypeInfo, len(typed.Params)),
			ParamIsRef: make([]bool, len(typed.Params)),
		}
		for i, param := range typed.Params {
			paramInfo, ok := c.runtimeTypeInfoWithStructs(param, structs)
			if !ok {
				return nil, false
			}
			info.Params[i] = paramInfo
			_, info.ParamIsRef[i] = param.(*ast.RefType)
		}
		result := normalizeReturnType(typed.Return)
		returnInfo, ok := c.runtimeTypeInfoWithStructs(result, structs)
		if !ok {
			return nil, false
		}
		info.Return = returnInfo
		return info, true
	default:
		return nil, false
	}
}
