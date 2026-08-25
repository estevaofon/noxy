package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
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

// compileBorrowedArgument e a semantica PRE-Task-6 do argumento de container
// dos builtins mutantes (append/pop/delete/json_loads): aceita `ref x` OU x
// nu (cria a referencia implicitamente — R5 ainda nao vale para builtins,
// fica para a Task 7) e, se x ja for `ref T`, encaminha o valor em vez de
// erro (forwarding — R1 tambem so vale para call sites de funcao tipada,
// nao aqui). E a copia da antiga compileReferenceArgumentValue, mantida so
// para estes quatro builtins; compileReferenceArgumentValue (abaixo, em
// compiler.go) agora so cria (R1).
func (c *Compiler) compileBorrowedArgument(expression ast.Expression) (ast.NoxyType, error) {
	targetType, err := c.compileBorrowedArgumentValue(expression)
	if err != nil || targetType == nil {
		return targetType, err
	}
	if runtimeType := c.runtimeTypeInfo(targetType); runtimeType != nil {
		typeConstant := c.makeConstant(value.NewRuntimeTypeInfo(runtimeType))
		if typeConstant > 65535 {
			return nil, fmt.Errorf("[line %d] too many constants for reference target metadata", c.currentLine)
		}
		c.emitByte(byte(chunk.OP_MARK_REF_TARGET_TYPE))
		c.emitByte(byte((typeConstant >> 8) & 0xff))
		c.emitByte(byte(typeConstant & 0xff))
	}
	return targetType, nil
}

func (c *Compiler) compileBorrowedArgumentValue(expression ast.Expression) (ast.NoxyType, error) {
	if prefix, ok := expression.(*ast.PrefixExpression); ok && prefix.Operator == "ref" {
		expression = prefix.Right
	}

	switch target := expression.(type) {
	case *ast.Identifier:
		if slot, declared := c.resolveLocal(target.Value); slot != -1 {
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitBytes(byte(chunk.OP_GET_LOCAL), byte(slot))
				return ref.ElementType, nil
			}
			if c.localOwns(slot) {
				c.emitBytes(byte(chunk.OP_REF_LOCAL), byte(slot))
			} else {
				c.emitBytes(byte(chunk.OP_REF_LOCAL_BORROW), byte(slot))
			}
			c.locals[slot].IsCaptured = true
			return declared, nil
		}
		if upvalue, declared := c.resolveUpvalue(target.Value); upvalue != -1 {
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitBytes(byte(chunk.OP_GET_UPVALUE), byte(upvalue))
				return ref.ElementType, nil
			}
			c.emitBytes(byte(chunk.OP_REF_UPVALUE), byte(upvalue))
			return declared, nil
		}
		name := c.makeConstant(value.NewString(target.Value))
		if declared, ok := c.resolveGlobalType(target.Value); ok {
			if ref, ok := declared.(*ast.RefType); ok {
				c.emitOpWithConstantIndex(chunk.OP_GET_GLOBAL, name)
				return ref.ElementType, nil
			}
			c.emitOpWithConstantIndex(chunk.OP_REF_GLOBAL, name)
			return declared, nil
		}
		c.emitOpWithConstantIndex(chunk.OP_REF_GLOBAL, name)
		return nil, nil
	case *ast.MemberAccessExpression:
		owner, _, err := c.compileLValueBase(target.Left)
		if err != nil {
			return nil, err
		}
		element := c.memberType(owner, target.Member)
		name := c.makeConstant(value.NewString(target.Member))
		if ref, ok := element.(*ast.RefType); ok {
			c.emitOpWithConstantIndex(chunk.OP_CONTEXT_REF_PROPERTY, name)
			return ref.ElementType, nil
		}
		c.emitOpWithConstantIndex(chunk.OP_REF_PROPERTY, name)
		return element, nil
	case *ast.IndexExpression:
		container, _, err := c.compileLValueBase(target.Left)
		if err != nil {
			return nil, err
		}
		element := indexElementType(container)
		_, indexType, err := c.Compile(target.Index)
		if err != nil {
			return nil, err
		}
		if err := c.rejectRefRead(indexType, target.Index, "index"); err != nil {
			return nil, err
		}
		switch collection := unwrapRefType(container).(type) {
		case *ast.ArrayType:
			expected := &ast.PrimitiveType{Name: "int"}
			if !c.areStrictTypesCompatible(expected, indexType) {
				return nil, fmt.Errorf(
					"[line %d] array reference index must be int, got %s",
					c.currentLine, noxyTypeName(indexType),
				)
			}
		case *ast.MapType:
			if !c.areStrictTypesCompatible(collection.KeyType, indexType) {
				return nil, fmt.Errorf(
					"[line %d] map reference key must be %s, got %s",
					c.currentLine, noxyTypeName(collection.KeyType), noxyTypeName(indexType),
				)
			}
		}
		if ref, ok := element.(*ast.RefType); ok {
			c.emitByte(byte(chunk.OP_CONTEXT_REF_INDEX))
			return ref.ElementType, nil
		}
		c.emitByte(byte(chunk.OP_REF_INDEX))
		return element, nil
	case *ast.NullLiteral:
		c.emitByte(byte(chunk.OP_NULL))
		return nil, nil
	case *ast.CallExpression:
		_, result, err := c.Compile(target)
		if err != nil {
			return nil, err
		}
		if ref, ok := result.(*ast.RefType); ok {
			return ref.ElementType, nil
		}
		return nil, fmt.Errorf(
			"[line %d] reference argument '%s' is not addressable\n  hint: use a variable, property, index, or null",
			c.currentLine, expression.String(),
		)
	default:
		return nil, fmt.Errorf(
			"[line %d] reference argument '%s' is not addressable\n  hint: use a variable, property, index, or null",
			c.currentLine, expression.String(),
		)
	}
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
		container, err := c.compileBorrowedArgument(call.Arguments[0])
		if err != nil {
			return true, nil, err
		}
		array, ok := container.(*ast.ArrayType)
		if !ok {
			return true, nil, fmt.Errorf("[line %d] append expects an array, got %s", c.currentLine, noxyTypeName(container))
		}
		if expectedRef, ok := array.ElementType.(*ast.RefType); ok {
			actualElement, err := c.compileBorrowedArgument(call.Arguments[1])
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
		container, err := c.compileBorrowedArgument(call.Arguments[0])
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
		container, err := c.compileBorrowedArgument(call.Arguments[0])
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
		targetType, err := c.compileBorrowedArgument(call.Arguments[1])
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
