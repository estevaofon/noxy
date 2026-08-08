package compiler

import (
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
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
