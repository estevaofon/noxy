package vm

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
	"noxy-vm/internal/value"
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

func TestSpaceInvadersScriptedInteractiveInput(t *testing.T) {
	source, err := os.ReadFile("../../noxy_examples/space_invaders.nx")
	if err != nil {
		t.Fatal(err)
	}

	previous := os.Args
	os.Args = []string{"noxy", "space_invaders.nx"}
	t.Cleanup(func() { os.Args = previous })

	code := compileVMSourceWithRoot(t, string(source), "../..")
	driver := &fakeTerminalDriver{terminal: true}
	machine := NewWithConfig(VMConfig{RootPath: "../.."})
	machine.shared.Terminal = &terminalRuntime{
		driver: driver,
		input:  bufio.NewReader(strings.NewReader("ad q")),
		fd:     42,
	}
	var rendered strings.Builder
	machine.SetGlobal("iprint", value.NewNative("iprint", func(args []value.Value) value.Value {
		for i, arg := range args {
			if i > 0 {
				rendered.WriteString(" ")
			}
			rendered.WriteString(arg.String())
		}
		return value.NewNull()
	}))

	completed := make(chan error, 1)
	go func() {
		completed <- machine.Interpret(code)
	}()

	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("Interpret() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("interactive game did not complete within 2s")
	}

	if driver.made != 1 {
		t.Errorf("makeRaw calls = %d, want 1", driver.made)
	}
	if driver.restored != 1 {
		t.Errorf("restore calls = %d, want 1", driver.restored)
	}
	for _, sequence := range []string{"\x1b[?25l", "\x1b[2J", "\x1b[H", "\x1b[?25h"} {
		if !strings.Contains(rendered.String(), sequence) {
			t.Errorf("rendered output does not contain ANSI sequence %q", sequence)
		}
	}
}
