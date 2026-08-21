package vm

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// O inliner do Go trata run() (executor.go) como "big function": ela tem mais
// de 5000 nos de AST, e nesse regime o orcamento para inlinar um callee cai de
// 80 para 20 (inlineBigFunctionMaxCost). push() e a operacao mais quente do
// interpretador e aparece 117 vezes dentro de run(); com custo <= 20 ela e
// inlinada em todas, com 21 em NENHUMA — e o interpretador inteiro fica ~20 %
// mais lento (medido na issue #56: push a 77 custou +21 % em
// BenchmarkNoxyCallOverhead e +8..22 % no corpus de benchmarks/).
//
// A propriedade e invisivel em teste funcional e some sem aviso quando alguem
// acrescenta um no ao corpo de push (um literal composto no panic, uma chamada
// no ramo frio, comparar com len(vm.stack) em vez de vm.stackLimit). Este
// teste a trava, perguntando ao proprio compilador.
func TestPushStaysInlinedInsideRun(t *testing.T) {
	build := exec.Command("go", "build", "-gcflags=-m=2", "./")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build -gcflags=-m=2 failed: %v\n%s", err, output)
	}
	report := string(output)

	costPattern := regexp.MustCompile(`can inline \(\*VM\)\.push with cost (\d+)`)
	match := costPattern.FindStringSubmatch(report)
	if match == nil {
		t.Fatalf("o compilador nao inlina (*VM).push de jeito nenhum — procure por 'cannot inline (*VM).push' na saida de `go build -gcflags=-m=2 ./internal/vm`")
	}
	cost, convErr := strconv.Atoi(match[1])
	if convErr != nil {
		t.Fatalf("custo de inline ilegivel em %q: %v", match[0], convErr)
	}
	if cost > inlineBigFunctionMaxCost {
		t.Errorf("push tem custo de inline %d, maximo %d para ser inlinada dentro de run() — tire nos do corpo de push (ver o comentario em stack.go)", cost, inlineBigFunctionMaxCost)
	}

	inlined := 0
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "executor.go") && strings.Contains(line, "inlining call to (*VM).push") {
			inlined++
		}
	}
	if inlined < minPushInlineSitesInRun {
		t.Errorf("push foi inlinada em %d call sites de executor.go, esperado >= %d (custo reportado: %d)", inlined, minPushInlineSitesInRun, cost)
	}
}

// inlineBigFunctionMaxCost espelha a constante homonima do compilador
// (cmd/compile/internal/inline): o orcamento por callee dentro de uma funcao
// grande como run().
const inlineBigFunctionMaxCost = 20

// minPushInlineSitesInRun e uma margem sob os 117 call sites de hoje — o teste
// e sobre "push continua inlinada dentro de run()", nao sobre a contagem
// exata, que muda quando opcodes sao acrescentados ou removidos.
const minPushInlineSitesInRun = 100
