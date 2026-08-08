package compiler

import (
	"fmt"
	"noxy-vm/internal/ast"
)

func normalizeReturnType(t ast.NoxyType) ast.NoxyType {
	if t == nil {
		return &ast.PrimitiveType{Name: "void"}
	}
	return t
}

func newFunctionType(params []*ast.Parameter, result ast.NoxyType) *ast.FunctionType {
	types := make([]ast.NoxyType, len(params))
	for i, param := range params {
		types[i] = param.Type
	}
	return &ast.FunctionType{Params: types, Return: normalizeReturnType(result)}
}

func isBareFunctionType(t ast.NoxyType) bool {
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "func"
}

func isCallableType(t ast.NoxyType) bool {
	if isBareFunctionType(t) {
		return true
	}
	_, ok := t.(*ast.FunctionType)
	return ok
}

func isNullType(t ast.NoxyType) bool {
	p, ok := t.(*ast.PrimitiveType)
	return ok && p.Name == "null"
}

func (c *Compiler) acceptsNull(t ast.NoxyType) bool {
	if isAny(t) || isNullType(t) {
		return true
	}
	if _, ok := t.(*ast.RefType); ok {
		return true
	}
	primitive, ok := t.(*ast.PrimitiveType)
	if !ok {
		return false
	}
	_, isStruct := c.structs[primitive.Name]
	return isStruct
}

func (c *Compiler) containsCallableType(t ast.NoxyType, visiting map[string]bool) bool {
	switch typed := t.(type) {
	case *ast.FunctionType:
		return true
	case *ast.PrimitiveType:
		if typed.Name == "func" {
			return true
		}
		definition, ok := c.structs[typed.Name]
		if !ok {
			return false
		}
		if visiting == nil {
			visiting = make(map[string]bool)
		}
		if visiting[typed.Name] {
			return false
		}
		visiting[typed.Name] = true
		defer delete(visiting, typed.Name)
		for _, field := range definition.FieldsList {
			if c.containsCallableType(field.Type, visiting) {
				return true
			}
		}
		return false
	case *ast.ArrayType:
		return c.containsCallableType(typed.ElementType, visiting)
	case *ast.MapType:
		return c.containsCallableType(typed.KeyType, visiting) ||
			c.containsCallableType(typed.ValueType, visiting)
	case *ast.ChanType:
		return c.containsCallableType(typed.ElementType, visiting)
	case *ast.RefType:
		return c.containsCallableType(typed.ElementType, visiting)
	default:
		return false
	}
}

func noxyTypeName(t ast.NoxyType) string {
	if t == nil {
		return "unknown"
	}
	return t.String()
}

func callableName(expression ast.Expression) string {
	if identifier, ok := expression.(*ast.Identifier); ok {
		return identifier.Value
	}
	return expression.String()
}

func unwrapRefType(t ast.NoxyType) ast.NoxyType {
	if ref, ok := t.(*ast.RefType); ok {
		return ref.ElementType
	}
	return t
}

func indexElementType(container ast.NoxyType) ast.NoxyType {
	switch typed := unwrapRefType(container).(type) {
	case *ast.ArrayType:
		return typed.ElementType
	case *ast.MapType:
		return typed.ValueType
	default:
		return nil
	}
}

func sameExactType(left, right ast.NoxyType) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	switch l := left.(type) {
	case *ast.PrimitiveType:
		r, ok := right.(*ast.PrimitiveType)
		return ok && l.Name == r.Name
	case *ast.ArrayType:
		r, ok := right.(*ast.ArrayType)
		return ok && l.Size == r.Size && sameExactType(l.ElementType, r.ElementType)
	case *ast.MapType:
		r, ok := right.(*ast.MapType)
		return ok && sameExactType(l.KeyType, r.KeyType) && sameExactType(l.ValueType, r.ValueType)
	case *ast.ChanType:
		r, ok := right.(*ast.ChanType)
		return ok && sameExactType(l.ElementType, r.ElementType)
	case *ast.RefType:
		r, ok := right.(*ast.RefType)
		return ok && sameExactType(l.ElementType, r.ElementType)
	case *ast.FunctionType:
		r, ok := right.(*ast.FunctionType)
		if !ok || len(l.Params) != len(r.Params) || !sameExactType(l.Return, r.Return) {
			return false
		}
		for i := range l.Params {
			if !sameExactType(l.Params[i], r.Params[i]) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (c *Compiler) areStrictTypesCompatible(expected, actual ast.NoxyType) bool {
	if expected == nil {
		return true
	}
	if actual == nil {
		return !c.containsCallableType(expected, nil)
	}
	if isAny(expected) {
		return true
	}
	if isNullType(actual) {
		return c.acceptsNull(expected)
	}
	if isAny(actual) {
		return false
	}
	if isBareFunctionType(expected) {
		return isCallableType(actual)
	}
	if isBareFunctionType(actual) {
		return isBareFunctionType(expected)
	}
	if _, ok := expected.(*ast.FunctionType); ok {
		return sameExactType(expected, actual)
	}
	switch e := expected.(type) {
	case *ast.ArrayType:
		a, ok := actual.(*ast.ArrayType)
		return ok && (e.Size == 0 || e.Size == a.Size) &&
			c.areStrictTypesCompatible(e.ElementType, a.ElementType)
	case *ast.MapType:
		a, ok := actual.(*ast.MapType)
		return ok && c.areStrictTypesCompatible(e.KeyType, a.KeyType) &&
			c.areStrictTypesCompatible(e.ValueType, a.ValueType)
	case *ast.ChanType:
		a, ok := actual.(*ast.ChanType)
		return ok && c.areStrictTypesCompatible(e.ElementType, a.ElementType)
	case *ast.RefType:
		a, ok := actual.(*ast.RefType)
		return ok && c.areStrictTypesCompatible(e.ElementType, a.ElementType)
	default:
		return expected.String() == actual.String()
	}
}

func commonInferredType(left, right ast.NoxyType) ast.NoxyType {
	if isCallableType(left) && isCallableType(right) {
		return &ast.PrimitiveType{Name: "func"}
	}
	return &ast.PrimitiveType{Name: "any"}
}

func (c *Compiler) predeclareFunctions(statements []ast.Statement) error {
	seen := make(map[string]struct{})
	for _, statement := range statements {
		fn, ok := statement.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		if _, duplicate := seen[fn.Name]; duplicate {
			return fmt.Errorf("[line %d] duplicate function '%s'", fn.Token.Line, fn.Name)
		}
		seen[fn.Name] = struct{}{}
		c.globals[fn.Name] = newFunctionType(fn.Parameters, fn.ReturnType)
	}
	return nil
}

func (c *Compiler) predeclareStructs(statements []ast.Statement) {
	for _, statement := range statements {
		definition, ok := statement.(*ast.StructStatement)
		if ok {
			c.structs[definition.Name] = definition
		}
	}
}

func blockGuaranteesReturn(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}
	for _, statement := range block.Statements {
		if statementGuaranteesReturn(statement) {
			return true
		}
	}
	return false
}

func statementGuaranteesReturn(statement ast.Statement) bool {
	switch s := statement.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BlockStatement:
		return blockGuaranteesReturn(s)
	case *ast.IfStatement:
		return s.Alternative != nil &&
			blockGuaranteesReturn(s.Consequence) &&
			blockGuaranteesReturn(s.Alternative)
	case *ast.WhenStatement:
		if len(s.Cases) == 0 {
			return false
		}
		hasDefault := false
		for _, clause := range s.Cases {
			hasDefault = hasDefault || clause.IsDefault
			if !blockGuaranteesReturn(clause.Body) {
				return false
			}
		}
		return hasDefault
	default:
		return false
	}
}
