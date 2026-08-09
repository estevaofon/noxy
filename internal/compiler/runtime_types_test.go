package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/value"
	"testing"
)

func TestRuntimeTypeInfoRepresentsCallableAndChannelSchemas(t *testing.T) {
	c := New()
	integer := &ast.PrimitiveType{Name: "int"}
	callable := &ast.FunctionType{Params: []ast.NoxyType{integer}, Return: integer}

	exact := c.runtimeTypeInfo(callable)
	if exact == nil || exact.Kind != value.TYPE_CALLABLE || exact.CallableBare || len(exact.Params) != 1 || exact.Return == nil {
		t.Fatalf("exact callable schema=%#v", exact)
	}
	bare := c.runtimeTypeInfo(&ast.PrimitiveType{Name: "func"})
	if bare == nil || bare.Kind != value.TYPE_CALLABLE || !bare.CallableBare {
		t.Fatalf("bare callable schema=%#v", bare)
	}
	channel := c.runtimeTypeInfo(&ast.ChanType{ElementType: callable})
	if channel == nil || channel.Kind != value.TYPE_CHANNEL || channel.Element == nil || channel.Element.Kind != value.TYPE_CALLABLE {
		t.Fatalf("channel schema=%#v", channel)
	}
}

func TestRuntimeTypeInfoNeverReturnsPartialComposite(t *testing.T) {
	c := New()
	unknown := &ast.PrimitiveType{Name: "Missing"}
	tests := []ast.NoxyType{
		&ast.ArrayType{ElementType: unknown},
		&ast.MapType{KeyType: &ast.PrimitiveType{Name: "string"}, ValueType: unknown},
		&ast.RefType{ElementType: unknown},
		&ast.ChanType{ElementType: unknown},
	}
	for _, schemaType := range tests {
		if got := c.runtimeTypeInfo(schemaType); got != nil {
			t.Fatalf("runtimeTypeInfo(%s)=%#v, want nil", schemaType.String(), got)
		}
	}

	c.structs["Partial"] = &ast.StructStatement{
		Name: "Partial",
		FieldsList: []*ast.StructField{
			{Name: "known", Type: &ast.PrimitiveType{Name: "int"}},
			{Name: "missing", Type: unknown},
		},
	}
	if got := c.runtimeTypeInfo(&ast.PrimitiveType{Name: "Partial"}); got != nil {
		t.Fatalf("partial struct schema=%#v, want nil", got)
	}
}

func TestRuntimeTypeInfoHandlesRecursiveStructSchema(t *testing.T) {
	c := New()
	c.structs["Node"] = &ast.StructStatement{
		Name: "Node",
		FieldsList: []*ast.StructField{
			{Name: "next", Type: &ast.PrimitiveType{Name: "Node"}},
		},
	}
	got := c.runtimeTypeInfo(&ast.PrimitiveType{Name: "Node"})
	if got == nil || got.Kind != value.TYPE_STRUCT || got.Fields["next"] != got {
		t.Fatalf("recursive struct schema=%#v", got)
	}
}

func TestRuntimeTypeInfoUsesCanonicalNestedCallableAndFixedArraySpelling(t *testing.T) {
	c := New()
	integer := &ast.PrimitiveType{Name: "int"}
	callable := &ast.FunctionType{Params: []ast.NoxyType{integer}, Return: integer}
	tests := []ast.NoxyType{
		&ast.ArrayType{ElementType: callable},
		&ast.MapType{KeyType: &ast.PrimitiveType{Name: "string"}, ValueType: callable},
		&ast.RefType{ElementType: callable},
		&ast.ChanType{ElementType: callable},
	}
	for _, staticType := range tests {
		runtimeType := c.runtimeTypeInfo(staticType)
		if runtimeType == nil || runtimeType.String() != staticType.String() {
			t.Fatalf("runtime spelling=%q, want AST spelling %q", runtimeType.String(), staticType.String())
		}
	}
	fixed := c.runtimeTypeInfo(&ast.ArrayType{ElementType: callable, Size: 4})
	if fixed == nil || fixed.String() != "(func(int) -> int)[4]" {
		t.Fatalf("fixed callable array spelling=%q", fixed.String())
	}
}
