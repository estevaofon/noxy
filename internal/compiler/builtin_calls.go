package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
)

func builtinType(name string) ast.NoxyType {
	return &ast.PrimitiveType{Name: name}
}

func (c *Compiler) compileBuiltinValueArgument(expression ast.Expression) (ast.NoxyType, error) {
	_, actual, err := c.Compile(expression)
	if err != nil {
		return nil, err
	}
	explicitReference := false
	if prefix, ok := expression.(*ast.PrefixExpression); ok {
		explicitReference = prefix.Operator == "ref"
	}
	if ref, ok := actual.(*ast.RefType); ok && !explicitReference {
		c.emitByte(byte(chunk.OP_DEREF))
		return ref.ElementType, nil
	}
	return actual, nil
}

func (c *Compiler) compileBuiltinCall(call *ast.CallExpression) (bool, ast.NoxyType, error) {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false, nil, nil
	}

	name := ident.Value
	if name != "append" && name != "pop" && name != "delete" && name != "json_loads" {
		return false, nil, nil
	}
	if slot, _ := c.resolveLocal(name); slot != -1 {
		return false, nil, nil
	}
	if upvalue, _ := c.resolveUpvalue(name); upvalue != -1 {
		return false, nil, nil
	}
	if _, declared := c.resolveGlobalType(name); declared {
		return false, nil, nil
	}
	wantArity := map[string]int{"append": 2, "pop": 1, "delete": 2, "json_loads": 2}[name]
	if len(call.Arguments) != wantArity {
		return true, nil, fmt.Errorf(
			"[line %d] %s expects %d arguments, got %d",
			c.currentLine, name, wantArity, len(call.Arguments),
		)
	}
	if _, _, err := c.Compile(call.Function); err != nil {
		return true, nil, err
	}

	switch name {
	case "append":
		container, err := c.compileReferenceArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] append expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		if expectedRef, ok := array.ElementType.(*ast.RefType); ok {
			actualElement, err := c.compileReferenceArgument(call.Arguments[1])
			if err != nil {
				return true, nil, err
			}
			if !c.areStrictTypesCompatible(expectedRef.ElementType, actualElement) {
				actual := &ast.RefType{ElementType: actualElement}
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to 'append': expected %s, got %s",
					c.currentLine, noxyTypeName(array.ElementType), actual.String(),
				)
			}
		} else {
			item, err := c.compileBuiltinValueArgument(call.Arguments[1])
			if err != nil {
				return true, nil, err
			}
			if _, explicitRef := item.(*ast.RefType); explicitRef {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to 'append': expected %s, got %s",
					c.currentLine, noxyTypeName(array.ElementType), noxyTypeName(item),
				)
			}
			if !c.areStrictTypesCompatible(array.ElementType, item) {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to 'append': expected %s, got %s",
					c.currentLine, noxyTypeName(array.ElementType), noxyTypeName(item),
				)
			}
		}
		c.emitBytes(byte(chunk.OP_CALL), 2)
		return true, builtinType("void"), nil
	case "pop":
		container, err := c.compileReferenceArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] pop expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		c.emitBytes(byte(chunk.OP_CALL), 1)
		return true, array.ElementType, nil
	case "delete":
		container, err := c.compileReferenceArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		mapping, ok := container.(*ast.MapType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] delete expects a map, got %s", c.currentLine, noxyTypeName(container))
		}
		key, err := c.compileBuiltinValueArgument(call.Arguments[1])
		if err != nil {
			return true, nil, err
		}
		if _, explicitRef := key.(*ast.RefType); explicitRef {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'delete': expected %s, got %s",
				c.currentLine, noxyTypeName(mapping.KeyType), noxyTypeName(key),
			)
		}
		if !c.areStrictTypesCompatible(mapping.KeyType, key) {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'delete': expected %s, got %s",
				c.currentLine, noxyTypeName(mapping.KeyType), noxyTypeName(key),
			)
		}
		c.emitBytes(byte(chunk.OP_CALL), 2)
		return true, builtinType("void"), nil
	case "json_loads":
		jsonText, err := c.compileBuiltinValueArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		if !c.areStrictTypesCompatible(builtinType("string"), jsonText) {
			return true, nil, fmt.Errorf(
				"[line %d] argument 1 to 'json_loads': expected string, got %s",
				c.currentLine, noxyTypeName(jsonText),
			)
		}
		targetType, err := c.compileReferenceArgument(call.Arguments[1])
		if err != nil {
			return true, nil, err
		}
		if primitive, ok := targetType.(*ast.PrimitiveType); ok && primitive.Name == "any" {
			c.emitByte(byte(chunk.OP_MARK_REF_JSON_DYNAMIC))
		}
		c.emitBytes(byte(chunk.OP_CALL), 2)
		return true, builtinType("bool"), nil
	default:
		return false, nil, nil
	}
}
