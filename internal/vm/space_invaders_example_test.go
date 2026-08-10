package vm

import (
	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"os"
	"path/filepath"
	"testing"
)

func compileVMSourceWithRoot(t *testing.T, source, root string) *chunk.Chunk {
	t.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		t.Fatalf("parser errors: %v", p.Errors())
	}
	fileName := filepath.Join(root, "noxy_examples", "space_invaders.nx")
	code, _, err := compiler.NewWithStateAndRoot(
		make(map[string]ast.NoxyType),
		make(map[string]*ast.StructStatement),
		fileName,
		root,
	).Compile(program)
	if err != nil {
		t.Fatalf("compiler error: %v", err)
	}
	return code
}

func TestSpaceInvadersSmokeMode(t *testing.T) {
	source, err := os.ReadFile("../../noxy_examples/space_invaders.nx")
	if err != nil {
		t.Fatal(err)
	}

	previous := os.Args
	os.Args = []string{"noxy", "space_invaders.nx", "--smoke"}
	t.Cleanup(func() { os.Args = previous })

	code := compileVMSourceWithRoot(t, string(source), "../..")
	machine := NewWithConfig(VMConfig{RootPath: "../.."})
	if err := machine.Interpret(code); err != nil {
		t.Fatal(err)
	}
}
