package vm

import (
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/compiler"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
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

// Mede alocações por chamada de função Noxy. Antes da Task 4: >=2 allocs/op
// no caminho de chamada (CallFrame + Owned). Depois: 0 allocs/op de frame em
// regime estacionário (capacidades de Owned/Deferred reusadas).
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
