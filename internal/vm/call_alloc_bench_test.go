package vm

import (
	"testing"

	"github.com/estevaofon/noxy/internal/chunk"
	"github.com/estevaofon/noxy/internal/compiler"
	"github.com/estevaofon/noxy/internal/lexer"
	"github.com/estevaofon/noxy/internal/parser"
)

func compileVMSourceForBench(b *testing.B, source string) *chunk.Chunk {
	b.Helper()
	l := lexer.New(source)
	p := parser.New(l)
	program := p.ParseProgram()
	if len(p.Errors()) != 0 {
		b.Fatalf("parser errors: %v", p.Errors())
	}
	code, _, err := compiler.New().Compile(program)
	if err != nil {
		b.Fatalf("compiler error: %v", err)
	}
	return code
}

// Mede alocações por chamada de função Noxy. leaf(n: int) recebe um escalar,
// então value.Retain nunca "pega" nele e Owned não chega a crescer neste
// benchmark — o que ele isola é só o &CallFrame{} por chamada. Antes da
// Task 4: ~1 alloc/op de CallFrame por chamada de leaf (1000/op) + setup fixo
// por Interpret. Depois: 0 allocs/op de CallFrame em regime estacionário,
// restando só o setup fixo por Interpret.
func BenchmarkNoxyCallOverhead(b *testing.B) {
	machine := New()
	code := compileVMSourceForBench(b, `
func leaf(n: int) -> int
    return n
end
func run(times: int) -> int
    let i: int = 0
    let acc: int = 0
    while i < times do
        acc = leaf(i)
        i = i + 1
    end
    return acc
end
run(1000)
`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := machine.Interpret(code); err != nil {
			b.Fatalf("vm error: %v", err)
		}
	}
}
