package compiler

import (
	"github.com/estevaofon/noxy/internal/ast"
	"testing"
)

func TestInstanceName(t *testing.T) {
	got := instanceName("main", "first", []ast.NoxyType{tInt()})
	if got != "main::first<int>" {
		t.Fatalf("instanceName = %q", got)
	}
	got = instanceName("colecoes", "map", []ast.NoxyType{tInt(), tString()})
	if got != "colecoes::map<int,string>" {
		t.Fatalf("instanceName = %q", got)
	}
}

func TestSubstituteFunctionRewritesAllTypePositions(t *testing.T) {
	prog := parse("func first<T>(arr: T[]) -> T\n    let tmp: T = arr[0]\n    return tmp\nend")
	tpl := &FuncTemplate{Decl: prog.Statements[0].(*ast.FunctionStatement), Module: "main"}
	inst := substituteFunction(tpl, map[string]ast.NoxyType{"T": tInt()}, "main::first<int>")
	if inst.Name != "main::first<int>" || len(inst.TypeParams) != 0 {
		t.Fatalf("instancia mal formada: %s %v", inst.Name, inst.TypeParams)
	}
	if inst.Parameters[0].Type.String() != "int[]" {
		t.Fatalf("param = %s", inst.Parameters[0].Type.String())
	}
	if inst.ReturnType.String() != "int" {
		t.Fatalf("retorno = %s", inst.ReturnType.String())
	}
	let := inst.Body.Statements[0].(*ast.LetStmt)
	if let.Type.String() != "int" {
		t.Fatalf("let interno = %s (anotacao do corpo nao substituida)", let.Type.String())
	}
	// original intacto (clone, nao mutacao)
	if tpl.Decl.Parameters[0].Type.String() != "T[]" {
		t.Fatal("template original foi mutado")
	}
}

func TestSubstituteStructBothMirrors(t *testing.T) {
	prog := parse("struct Node<T>\n    value: T,\n    next: ref Node<T>\nend")
	tpl := &StructTemplate{Decl: prog.Statements[0].(*ast.StructStatement), Module: "main"}
	inst := substituteStruct(tpl, map[string]ast.NoxyType{"T": tInt()}, "main::Node<int>")
	if inst.FieldsList[0].Type.String() != "int" {
		t.Fatalf("value = %s", inst.FieldsList[0].Type.String())
	}
	if inst.Fields["value"].String() != "int" {
		t.Fatalf("espelho Fields nao substituido: %s", inst.Fields["value"].String())
	}
	// auto-referencia: ref Node<T> vira ref Node<int> (GenericType com arg concreto)
	if inst.FieldsList[1].Type.String() != "ref Node<int>" {
		t.Fatalf("next = %s", inst.FieldsList[1].Type.String())
	}
}
