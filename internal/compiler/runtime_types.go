package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/value"
)

func (c *Compiler) runtimeTypeInfo(t ast.NoxyType) *value.RuntimeTypeInfo {
	return c.runtimeTypeInfoWithStructs(t, make(map[string]*value.RuntimeTypeInfo))
}

func (c *Compiler) runtimeTypeInfoWithStructs(t ast.NoxyType, structs map[string]*value.RuntimeTypeInfo) *value.RuntimeTypeInfo {
	switch typed := t.(type) {
	case *ast.PrimitiveType:
		switch typed.Name {
		case "any":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_ANY}
		case "null":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_NULL}
		case "bool":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_BOOL}
		case "int":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_INT}
		case "float":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_FLOAT}
		case "string":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_STRING}
		case "bytes":
			return &value.RuntimeTypeInfo{Kind: value.TYPE_BYTES}
		}
		definition, ok := c.structs[typed.Name]
		if !ok {
			return nil
		}
		if existing, ok := structs[typed.Name]; ok {
			return existing
		}
		info := &value.RuntimeTypeInfo{
			Kind:   value.TYPE_STRUCT,
			Name:   typed.Name,
			Fields: make(map[string]*value.RuntimeTypeInfo, len(definition.FieldsList)),
		}
		structs[typed.Name] = info
		for _, field := range definition.FieldsList {
			info.Fields[field.Name] = c.runtimeTypeInfoWithStructs(field.Type, structs)
		}
		return info
	case *ast.ArrayType:
		return &value.RuntimeTypeInfo{Kind: value.TYPE_ARRAY, Element: c.runtimeTypeInfoWithStructs(typed.ElementType, structs)}
	case *ast.MapType:
		return &value.RuntimeTypeInfo{
			Kind:  value.TYPE_MAP,
			Key:   c.runtimeTypeInfoWithStructs(typed.KeyType, structs),
			Value: c.runtimeTypeInfoWithStructs(typed.ValueType, structs),
		}
	case *ast.RefType:
		return &value.RuntimeTypeInfo{Kind: value.TYPE_REF, Element: c.runtimeTypeInfoWithStructs(typed.ElementType, structs)}
	default:
		return nil
	}
}
