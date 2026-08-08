package compiler

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"strings"
	"testing"
)

func compileFunctionSource(t *testing.T, input string) (*Compiler, error) {
	t.Helper()
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	c := New()
	_, _, err := c.Compile(program)
	return c, err
}

func primitive(name string) ast.NoxyType {
	return &ast.PrimitiveType{Name: name}
}

func TestFunctionTypeCompatibility(t *testing.T) {
	c := New()
	exact := &ast.FunctionType{Params: []ast.NoxyType{primitive("int")}, Return: primitive("int")}
	same := &ast.FunctionType{Params: []ast.NoxyType{primitive("int")}, Return: primitive("int")}
	different := &ast.FunctionType{Params: []ast.NoxyType{primitive("string")}, Return: primitive("int")}
	bare := primitive("func")

	if !c.areTypesCompatible(exact, same) {
		t.Fatal("equal exact signatures must be compatible")
	}
	if c.areTypesCompatible(exact, different) {
		t.Fatal("different exact signatures must be incompatible")
	}
	if !c.areTypesCompatible(bare, exact) {
		t.Fatal("exact function must widen to bare func")
	}
	if c.areTypesCompatible(exact, bare) {
		t.Fatal("bare func must not narrow to an exact signature")
	}
	if c.areTypesCompatible(bare, primitive("int")) {
		t.Fatal("bare func must accept only callable values")
	}
}

func TestStrictCompatibilityRejectsActualAny(t *testing.T) {
	c := New()
	if c.areStrictTypesCompatible(primitive("int"), primitive("any")) {
		t.Fatal("actual any must not satisfy concrete int in an exact contract")
	}
	if !c.areStrictTypesCompatible(primitive("any"), primitive("int")) {
		t.Fatal("expected any must accept int")
	}
}

func TestHeterogeneousCallableArrayWidensToBareFunction(t *testing.T) {
	_, err := compileFunctionSource(t, `
let callbacks: func[] = [
    func() -> int
        return 1
    end,
    func(value: int) -> int
        return value
    end
]`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPredeclaresForwardFunctionSignature(t *testing.T) {
	c, err := compileFunctionSource(t, `
func first() -> int
    return second()
end
func second() -> int
    return 42
end`)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := c.GetGlobals()["second"].(*ast.FunctionType)
	if !ok || got.Return.String() != "int" {
		t.Fatalf("second signature=%v", c.GetGlobals()["second"])
	}
}

func TestPredeclaresMutualRecursion(t *testing.T) {
	c, err := compileFunctionSource(t, `
func even(n: int) -> bool
    if n == 0 then
        return true
    else
        return odd(n - 1)
    end
end
func odd(n: int) -> bool
    if n == 0 then
        return false
    else
        return even(n - 1)
    end
end`)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"even", "odd"} {
		got, ok := c.GetGlobals()[name].(*ast.FunctionType)
		if !ok || got.Return.String() != "bool" {
			t.Fatalf("%s signature=%v", name, c.GetGlobals()[name])
		}
	}
}

func TestRejectsDuplicateTopLevelFunctionNames(t *testing.T) {
	_, err := compileFunctionSource(t, `
func same() -> int
    return 1
end
func same() -> int
    return 2
end`)
	if err == nil || !strings.Contains(err.Error(), "duplicate function 'same'") {
		t.Fatalf("error=%v", err)
	}
}

func TestFunctionLiteralKeepsExactSignature(t *testing.T) {
	_, err := compileFunctionSource(t, `
let stringify: func(int) -> string = func(v: int) -> string
    return "value"
end`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestExactCallDiagnostics(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"few", `func add(a: int, b: int) -> int
    return a + b
end
add(1)`, "expects 2 arguments, got 1"},
		{"many", `func add(a: int) -> int
    return a
end
add(1, 2)`, "expects 1 arguments, got 2"},
		{"type", `func add(a: int) -> int
    return a
end
add("x")`, "argument 1 to 'add': expected int, got string"},
		{"any", `func add(a: int) -> int
    return a
end
let x: any = 1
add(x)`, "expected int, got any"},
		{"ref", `func set(v: ref int) -> void
    return
end
set(1)`, "reference argument must be a variable, property, index, or null"},
		{"ref type", `func set(v: ref int) -> void
    return
end
let text: string = "x"
set(text)`, "argument 1 to 'set': expected ref int, got ref string"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestExactCallPropagatesReturnType(t *testing.T) {
	_, err := compileFunctionSource(t, `
func bad() -> string
    return "x"
end
let value: int = bad()`)
	if err == nil || !strings.Contains(err.Error(), "expected int, got string") {
		t.Fatalf("error=%v", err)
	}
}

func TestExactReferenceCallsCompile(t *testing.T) {
	_, err := compileFunctionSource(t, `
func set(v: ref int) -> void
    return
end
let value: int = 1
let values: int[] = [1]
set(value)
set(values[0])`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReturnChecking(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"wrong type", `func bad() -> int
    return "x"
end`, "return type mismatch in 'bad': expected int, got string"},
		{"missing value", `func bad() -> int
    return
end`, "must return int"},
		{"void value", `func bad() -> void
    return 1
end`, "void function 'bad' cannot return int"},
		{"fallthrough", `func bad(v: bool) -> int
    if v then
        return 1
    end
end`, "may finish without returning int"},
		{"actual any", `func bad(v: any) -> int
    return v
end`, "expected int, got any"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := compileFunctionSource(t, tt.input)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want %q", err, tt.want)
			}
		})
	}
}

func TestAllConditionalBranchesReturn(t *testing.T) {
	_, err := compileFunctionSource(t, `
func classify(v: int) -> string
    if v > 0 then
        return "positive"
    else
        return "other"
    end
end`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWhenGuaranteesReturnOnlyWithReturningDefault(t *testing.T) {
	returning := func() *ast.BlockStatement {
		return &ast.BlockStatement{Statements: []ast.Statement{&ast.ReturnStmt{}}}
	}
	complete := &ast.WhenStatement{Cases: []*ast.CaseClause{
		{Body: returning()},
		{IsDefault: true, Body: returning()},
	}}
	if !statementGuaranteesReturn(complete) {
		t.Fatal("when with a default and returning bodies must guarantee return")
	}
	withoutDefault := &ast.WhenStatement{Cases: []*ast.CaseClause{{Body: returning()}}}
	if statementGuaranteesReturn(withoutDefault) {
		t.Fatal("when without default must not guarantee return")
	}
}
