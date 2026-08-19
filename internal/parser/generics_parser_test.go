package parser

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"testing"
)

func parseProgramNoErrors(t *testing.T, src string) *ast.Program {
	t.Helper()
	p := New(lexer.New(src))
	prog := p.ParseProgram()
	if errs := p.Errors(); len(errs) > 0 {
		t.Fatalf("erros de parse: %v", errs)
	}
	return prog
}

func TestParseGenericFunctionDeclaration(t *testing.T) {
	prog := parseProgramNoErrors(t, "func first<T>(arr: T[]) -> T\n    return arr[0]\nend")
	fn := prog.Statements[0].(*ast.FunctionStatement)
	if len(fn.TypeParams) != 1 || fn.TypeParams[0] != "T" {
		t.Fatalf("TypeParams = %v, quer [T]", fn.TypeParams)
	}
	arr := fn.Parameters[0].Type.(*ast.ArrayType)
	if _, ok := arr.ElementType.(*ast.TypeParamType); !ok {
		t.Fatalf("param deve ser T[] com TypeParamType, veio %T", arr.ElementType)
	}
	if _, ok := fn.ReturnType.(*ast.TypeParamType); !ok {
		t.Fatalf("retorno deve ser TypeParamType, veio %T", fn.ReturnType)
	}
}

func TestParseGenericStructDeclaration(t *testing.T) {
	prog := parseProgramNoErrors(t, "struct Pair<A, B>\n    left: A,\n    right: B\nend")
	st := prog.Statements[0].(*ast.StructStatement)
	if len(st.TypeParams) != 2 || st.TypeParams[0] != "A" || st.TypeParams[1] != "B" {
		t.Fatalf("TypeParams = %v, quer [A B]", st.TypeParams)
	}
	if _, ok := st.FieldsList[0].Type.(*ast.TypeParamType); !ok {
		t.Fatalf("campo left deve ser TypeParamType, veio %T", st.FieldsList[0].Type)
	}
}

func TestTypeParamScopeEndsWithDeclaration(t *testing.T) {
	// Depois do template, T fora de escopo volta a ser tipo comum (struct/nome)
	prog := parseProgramNoErrors(t, "func id<T>(x: T) -> T\n    return x\nend\nlet a: int = 1")
	if len(prog.Statements) != 2 {
		t.Fatalf("esperava 2 statements, veio %d", len(prog.Statements))
	}
}

// TestGenericSelfReferenceInStruct exige GenericType em posicao de tipo
// (Stack<T> como anotacao). Relocado da Task 3 para a Task 4 por decisao do
// brief da Task 3 (§ "mover para Task 4 se ainda falhar ao fim desta"), pois
// so a Task 4 implementa GenericType em posicao de anotacao.
func TestGenericSelfReferenceInStruct(t *testing.T) {
	prog := parseProgramNoErrors(t, "struct Node<T>\n    value: T,\n    next: ref Node<T>\nend")
	st := prog.Statements[0].(*ast.StructStatement)
	next := st.FieldsList[1].Type.(*ast.RefType)
	g := next.ElementType.(*ast.GenericType)
	if g.Name != "Node" {
		t.Fatalf("auto-referencia Node<T>, veio %s", g.String())
	}
	if _, ok := g.Args[0].(*ast.TypeParamType); !ok {
		t.Fatalf("arg de Node<T> deve ser TypeParamType, veio %T", g.Args[0])
	}
}

func TestParseGenericTypeAnnotation(t *testing.T) {
	prog := parseProgramNoErrors(t, "struct Stack<T>\n    items: T[]\nend\nlet s: Stack<int> = null")
	let := prog.Statements[1].(*ast.LetStmt)
	g := let.Type.(*ast.GenericType)
	if g.String() != "Stack<int>" {
		t.Fatalf("tipo = %s, quer Stack<int>", g.String())
	}
}

func TestParseNestedGenericTypeSplitsShiftRight(t *testing.T) {
	prog := parseProgramNoErrors(t, "struct Stack<T>\n    items: T[]\nend\nlet s: Stack<Stack<int>> = null")
	let := prog.Statements[1].(*ast.LetStmt)
	if got := let.Type.String(); got != "Stack<Stack<int>>" {
		t.Fatalf("tipo = %s", got)
	}
}

func TestParseGenericTypeSplitsGTEBeforeAssign(t *testing.T) {
	// sem espaco entre > e = : lexa GTE e o parser precisa dividir
	prog := parseProgramNoErrors(t, "struct Stack<T>\n    items: T[]\nend\nlet s: Stack<int>= null")
	let := prog.Statements[1].(*ast.LetStmt)
	if got := let.Type.String(); got != "Stack<int>" {
		t.Fatalf("tipo = %s", got)
	}
	if let.Value == nil {
		t.Fatal("valor do let perdido no split de GTE")
	}
}

func TestGenericTypeComposesWithArrayAndRef(t *testing.T) {
	prog := parseProgramNoErrors(t, "struct Stack<T>\n    items: T[]\nend\nlet a: Stack<int>[] = []\nlet r: ref Stack<int> = null")
	arr := prog.Statements[1].(*ast.LetStmt).Type.(*ast.ArrayType)
	if arr.ElementType.String() != "Stack<int>" {
		t.Fatalf("elemento = %s", arr.ElementType.String())
	}
	ref := prog.Statements[2].(*ast.LetStmt).Type.(*ast.RefType)
	if ref.ElementType.String() != "Stack<int>" {
		t.Fatalf("ref elemento = %s", ref.ElementType.String())
	}
}
