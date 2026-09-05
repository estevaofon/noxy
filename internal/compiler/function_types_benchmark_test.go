package compiler

import (
	"fmt"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
	"strings"
	"testing"
)

func BenchmarkCompileTypedFunctionCalls(b *testing.B) {
	var source strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&source, "func f%d(a: int, b: int) -> int\nreturn a + b\nend\n", i)
	}
	source.WriteString("func main() -> int\nlet total: int = 0\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&source, "total = total + f%d(1, 2)\n", i)
	}
	source.WriteString("return total\nend\nmain()\n")
	input := source.String()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(input)
		p := parser.New(l)
		program := p.ParseProgram()
		if len(p.Errors()) != 0 {
			b.Fatalf("parser errors: %v", p.Errors())
		}
		if _, _, err := New().Compile(program); err != nil {
			b.Fatal(err)
		}
	}
}
