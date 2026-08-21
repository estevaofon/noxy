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

	// ensureCallCapacity (calls.go) e chamada em call()/callPreparedClosure, que
	// NAO sao "big function" (calls.go fica bem abaixo de 5000 nos de AST), entao
	// o orcamento de inline aqui e o normal (80), nao os 20 de dentro de run().
	// O corpo custa EXATAMENTE 80 hoje — sem folga, de proposito (ver o
	// comentario "sem folga" em calls.go): qualquer no a mais desinlina esta
	// funcao, e toda chamada Noxy passaria a pagar uma call/ret que o desenho
	// atual evita (ensureCallCapacity roda na ENTRADA de toda chamada).
	ensureCallCapacityCostPattern := regexp.MustCompile(`can inline \(\*VM\)\.ensureCallCapacity with cost (\d+)`)
	ensureMatch := ensureCallCapacityCostPattern.FindStringSubmatch(report)
	if ensureMatch == nil {
		t.Fatalf("o compilador nao inlina (*VM).ensureCallCapacity de jeito nenhum — procure por 'cannot inline (*VM).ensureCallCapacity' na saida de `go build -gcflags=-m=2 ./internal/vm`")
	}
	ensureCost, ensureConvErr := strconv.Atoi(ensureMatch[1])
	if ensureConvErr != nil {
		t.Fatalf("custo de inline ilegivel em %q: %v", ensureMatch[0], ensureConvErr)
	}
	if ensureCost > inlineNormalMaxCost {
		t.Errorf("ensureCallCapacity tem custo de inline %d, maximo %d — tire nos do corpo (ver o comentario \"sem folga\" em calls.go)", ensureCost, inlineNormalMaxCost)
	}

	ensureInlinedInCalls := 0
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "calls.go") && strings.Contains(line, "inlining call to (*VM).ensureCallCapacity") {
			ensureInlinedInCalls++
		}
	}
	if ensureInlinedInCalls < minEnsureCallCapacityInlineSitesInCalls {
		t.Errorf("ensureCallCapacity foi inlinada em %d call sites de calls.go, esperado >= %d (custo reportado: %d)", ensureInlinedInCalls, minEnsureCallCapacityInlineSitesInCalls, ensureCost)
	}
}

// inlineBigFunctionMaxCost espelha a constante homonima do compilador
// (cmd/compile/internal/inline): o orcamento por callee dentro de uma funcao
// grande como run().
const inlineBigFunctionMaxCost = 20

// inlineNormalMaxCost e o orcamento padrao do inliner (cmd/compile/internal/
// inline.inlineMaxBudget) para callees dentro de uma funcao "normal" (<= 5000
// nos de AST) — o caso de ensureCallCapacity, chamada de dentro de calls.go.
const inlineNormalMaxCost = 80

// minPushInlineSitesInRun e uma margem sob os 117 call sites de hoje — o teste
// e sobre "push continua inlinada dentro de run()", nao sobre a contagem
// exata, que muda quando opcodes sao acrescentados ou removidos.
const minPushInlineSitesInRun = 100

// minEnsureCallCapacityInlineSitesInCalls e uma margem sob o unico call site
// de hoje (call(), calls.go).
const minEnsureCallCapacityInlineSitesInCalls = 1
