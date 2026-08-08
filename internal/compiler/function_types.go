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
	if expected == nil || actual == nil {
		return true
	}
	if isAny(expected) {
		return true
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
