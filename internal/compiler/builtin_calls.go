package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
)

func builtinType(name string) ast.NoxyType {
	return &ast.PrimitiveType{Name: name}
}

// compileBuiltinValueArgument compila um argumento de VALOR de builtin
// (item de append, chave de delete, texto de json_loads, limites de range).
// R2: um `ref T` aqui e devolvido como esta — o chamador rejeita com hint.
func (c *Compiler) compileBuiltinValueArgument(expression ast.Expression) (ast.NoxyType, error) {
	_, actual, err := c.Compile(expression)
	return actual, err
}

// compileBuiltinRefArgument aplica R5 aos builtins com parametro ref:
// append/pop/delete (arg 1), json_loads (arg 2), append em (ref T)[] (arg 2).
// `position` e "argument N to 'nome'"; `expected` e o texto do tipo esperado.
func (c *Compiler) compileBuiltinRefArgument(arg ast.Expression, position, expected string) (ast.NoxyType, error) {
	refArg, err := c.compileRefArgument(arg)
	if err != nil {
		return nil, err
	}
	// refArg.proven e descartado de proposito: builtins sempre emitem
	// emitCall(n, emission, false) — nunca OP_CALL_STATIC — entao nao ha
	// modesProven a alimentar aqui.
	if refArg.plain != nil {
		return nil, fmt.Errorf("[line %d] %s: expected %s, got %s%s",
			c.currentLine, position, expected, noxyTypeName(refArg.plain), refArgumentHint(arg))
	}
	return refArg.element, nil
}

func (c *Compiler) compileBuiltinCall(call *ast.CallExpression, emission callEmission) (bool, ast.NoxyType, error) {
	ident, ok := call.Function.(*ast.Identifier)
	if !ok {
		return false, nil, nil
	}

	name := ident.Value
	if name != "append" && name != "pop" && name != "delete" && name != "json_loads" && name != "range" {
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
	if name == "range" {
		if len(call.Arguments) < 1 || len(call.Arguments) > 3 {
			return true, nil, fmt.Errorf(
				"[line %d] range expects 1 to 3 arguments, got %d",
				c.currentLine, len(call.Arguments),
			)
		}
	} else if wantArity := map[string]int{"append": 2, "pop": 1, "delete": 2, "json_loads": 2}[name]; len(call.Arguments) != wantArity {
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
		container, err := c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to 'append'", "ref T[]")
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] append expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		if expectedRef, ok := asRefType(array.ElementType); ok {
			actualElement, err := c.compileBuiltinRefArgument(call.Arguments[1], "argument 2 to 'append'", noxyTypeName(array.ElementType))
			if err != nil {
				return true, nil, err
			}
			if actualElement != nil && !c.areStrictTypesCompatible(expectedRef.ElementType, actualElement) {
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
			if _, explicitRef := asRefType(item); explicitRef {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to 'append': expected %s, got %s%s",
					c.currentLine, noxyTypeName(array.ElementType), noxyTypeName(item),
					c.derefReadHint(array.ElementType, item, call.Arguments[1]),
				)
			}
			if !c.areStrictTypesCompatible(array.ElementType, item) {
				return true, nil, fmt.Errorf(
					"[line %d] argument 2 to 'append': expected %s, got %s",
					c.currentLine, noxyTypeName(array.ElementType), noxyTypeName(item),
				)
			}
			if err := c.emitRuntimeValueType(array.ElementType); err != nil {
				return true, nil, err
			}
		}
		c.emitCall(2, emission, false)
		return true, builtinType("void"), nil
	case "pop":
		container, err := c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to 'pop'", "ref T[]")
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] pop expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		c.emitCall(1, emission, false)
		return true, array.ElementType, nil
	case "delete":
		container, err := c.compileBuiltinRefArgument(call.Arguments[0], "argument 1 to 'delete'", "ref map")
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
		if _, explicitRef := asRefType(key); explicitRef {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'delete': expected %s, got %s%s",
				c.currentLine, noxyTypeName(mapping.KeyType), noxyTypeName(key),
				c.derefReadHint(mapping.KeyType, key, call.Arguments[1]),
			)
		}
		if !c.areStrictTypesCompatible(mapping.KeyType, key) {
			return true, nil, fmt.Errorf(
				"[line %d] argument 2 to 'delete': expected %s, got %s",
				c.currentLine, noxyTypeName(mapping.KeyType), noxyTypeName(key),
			)
		}
		c.emitCall(2, emission, false)
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
		targetType, err := c.compileBuiltinRefArgument(call.Arguments[1], "argument 2 to 'json_loads'", "ref T")
		if err != nil {
			return true, nil, err
		}
		if primitive, ok := targetType.(*ast.PrimitiveType); ok && primitive.Name == "any" {
			c.emitByte(byte(chunk.OP_MARK_REF_JSON_DYNAMIC))
		}
		c.emitCall(2, emission, false)
		return true, builtinType("bool"), nil
	case "range":
		// range(stop) | range(start, stop) | range(start, stop, step) -> int[].
		// Tipado aqui, e nao como native sem assinatura (retorno desconhecido):
		// `for i in range(n)` da i: int e aridade/tipo dos argumentos falham na
		// compilacao. O native revalida em runtime (chamada dinamica, plugin).
		for i, argument := range call.Arguments {
			actual, err := c.compileBuiltinValueArgument(argument)
			if err != nil {
				return true, nil, err
			}
			if !c.areStrictTypesCompatible(builtinType("int"), actual) {
				return true, nil, fmt.Errorf(
					"[line %d] argument %d to 'range': expected int, got %s",
					c.currentLine, i+1, noxyTypeName(actual),
				)
			}
		}
		c.emitCall(len(call.Arguments), emission, false)
		return true, &ast.ArrayType{ElementType: builtinType("int")}, nil
	default:
		return false, nil, nil
	}
}
