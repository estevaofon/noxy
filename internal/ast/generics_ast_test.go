package ast

import "testing"

func TestGenericTypeString(t *testing.T) {
	g := &GenericType{Name: "Stack", Args: []NoxyType{&PrimitiveType{Name: "int"}}}
	if got := g.String(); got != "Stack<int>" {
		t.Fatalf("String() = %q, quer Stack<int>", got)
	}
	nested := &GenericType{Name: "Stack", Args: []NoxyType{g}}
	if got := nested.String(); got != "Stack<Stack<int>>" {
		t.Fatalf("String() = %q, quer Stack<Stack<int>>", got)
	}
	multi := &GenericType{Name: "Map", Args: []NoxyType{
		&PrimitiveType{Name: "string"}, &PrimitiveType{Name: "int"},
	}}
	if got := multi.String(); got != "Map<string, int>" {
		t.Fatalf("String() = %q, quer Map<string, int>", got)
	}
}

func TestTypeParamTypeString(t *testing.T) {
	p := &TypeParamType{Name: "T"}
	if got := p.String(); got != "T" {
		t.Fatalf("String() = %q, quer T", got)
	}
}

func TestTypeParamsFieldsExist(t *testing.T) {
	f := &FunctionStatement{TypeParams: []string{"T"}}
	s := &StructStatement{TypeParams: []string{"K", "V"}}
	if len(f.TypeParams) != 1 || len(s.TypeParams) != 2 {
		t.Fatal("campos TypeParams ausentes")
	}
}
