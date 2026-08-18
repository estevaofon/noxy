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
// (Stack<T> como anotacao), que e' implementado na Task 4. Movido para
// internal/parser/generics_parser_test.go da Task 4 por decisao do brief da
// Task 3 (§ "mover para Task 4 se ainda falhar ao fim desta").
