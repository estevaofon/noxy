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

	// pop e a segunda operacao mais quente; na forma de sempre custava 22 e
	// nao era inlinada em NENHUM dos ~84 sites de run(). A atribuicao dupla
	// com resultado nomeado (stack.go) faz o mesmo trabalho em 18 nos —
	// issue #37, "extra barato".
	popCostPattern := regexp.MustCompile(`can inline \(\*VM\)\.pop with cost (\d+)`)
	popMatch := popCostPattern.FindStringSubmatch(report)
	if popMatch == nil {
		t.Fatalf("o compilador nao inlina (*VM).pop de jeito nenhum — procure por 'cannot inline (*VM).pop' na saida de `go build -gcflags=-m=2 ./internal/vm`")
	}
	popCost, popConvErr := strconv.Atoi(popMatch[1])
	if popConvErr != nil {
		t.Fatalf("custo de inline ilegivel em %q: %v", popMatch[0], popConvErr)
	}
	if popCost > inlineBigFunctionMaxCost {
		t.Errorf("pop tem custo de inline %d, maximo %d para ser inlinada dentro de run() — tire nos do corpo de pop (ver o comentario em stack.go)", popCost, inlineBigFunctionMaxCost)
	}
	popInlined := 0
	for _, line := range strings.Split(report, "\n") {
		if strings.Contains(line, "executor.go") && strings.Contains(line, "inlining call to (*VM).pop") {
			popInlined++
		}
	}
	if popInlined < minPopInlineSitesInRun {
		t.Errorf("pop foi inlinada em %d call sites de executor.go, esperado >= %d (custo reportado: %d)", popInlined, minPopInlineSitesInRun, popCost)
	}

	// Indexacao tipada (issue #66): value.NeverTracked e arrayTagIsRefSlot
	// (ref_slots.go) ficam no caminho quente dos tres opcodes NORC; se
	// sairem do inline, cada escrita tipada paga uma chamada. arrayTagIsRefSlot
	// custa EXATAMENTE 20 hoje — sem folga.
	neverTrackedInlined := 0
	tagInlined := 0
	for _, line := range strings.Split(report, "\n") {
		if !strings.Contains(line, "executor.go") {
			continue
		}
		if strings.Contains(line, "inlining call to value.NeverTracked") {
			neverTrackedInlined++
		}
		if strings.Contains(line, "inlining call to arrayTagIsRefSlot") {
			tagInlined++
		}
	}
	if neverTrackedInlined < 6 {
		t.Errorf("value.NeverTracked foi inlinada em %d sites de executor.go, esperado >= 6 (dois por opcode NORC)", neverTrackedInlined)
	}
	if tagInlined < 3 {
		t.Errorf("arrayTagIsRefSlot foi inlinada em %d sites de executor.go, esperado >= 3 (um por opcode NORC) — confira o custo em `go build -gcflags=-m=2 ./internal/vm | grep arrayTagIsRefSlot`", tagInlined)
	}
	tagCostPattern := regexp.MustCompile(`can inline arrayTagIsRefSlot with cost (\d+)`)
	if tagMatch := tagCostPattern.FindStringSubmatch(report); tagMatch == nil {
		t.Errorf("o compilador nao inlina arrayTagIsRefSlot (ref_slots.go)")
	} else if tagCost, convErr := strconv.Atoi(tagMatch[1]); convErr != nil || tagCost > inlineBigFunctionMaxCost {
		t.Errorf("arrayTagIsRefSlot tem custo de inline %s, maximo %d para ser inlinada dentro de run()", tagMatch[1], inlineBigFunctionMaxCost)
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

// Retain e Release (internal/value/cow.go) embutem ownersOf e sao inlinados
// nos sites de internal/vm fora de run() (ownSlot, bindOwnedSlot, calls.go…)
// com o orcamento normal de 80. A fase 2 de perf (issue #37) reescreveu
// ownersOf com caminho rapido pela dica kind; este teste garante que o corpo
// novo nao tirou os dois do inline.
func TestRetainReleaseStayInlinable(t *testing.T) {
	build := exec.Command("go", "build", "-gcflags=-m=2", "../value")
	output, err := build.CombinedOutput()
	if err != nil {
		t.Fatalf("go build -gcflags=-m=2 ../value failed: %v\n%s", err, output)
	}
	report := string(output)
	for _, name := range []string{"Retain", "Release"} {
		pattern := regexp.MustCompile(`can inline ` + name + ` with cost (\d+)`)
		match := pattern.FindStringSubmatch(report)
		if match == nil {
			t.Errorf("o compilador nao inlina value.%s — procure por 'cannot inline %s' em `go build -gcflags=-m=2 ./internal/value`", name, name)
			continue
		}
		cost, convErr := strconv.Atoi(match[1])
		if convErr != nil {
			t.Fatalf("custo ilegivel em %q: %v", match[0], convErr)
		}
		if cost > inlineNormalMaxCost {
			t.Errorf("value.%s tem custo de inline %d, maximo %d — enxugue ownersOf (ver cow.go)", name, cost, inlineNormalMaxCost)
		}
	}

	// NeverTracked (cow.go) e chamada de DENTRO de run() pelas escritas NORC da
	// indexacao tipada (issue #66): orcamento de 20, nao 80.
	neverTrackedPattern := regexp.MustCompile(`can inline NeverTracked with cost (\d+)`)
	ntMatch := neverTrackedPattern.FindStringSubmatch(report)
	if ntMatch == nil {
		t.Fatalf("o compilador nao inlina value.NeverTracked — procure por 'cannot inline NeverTracked' em `go build -gcflags=-m=2 ./internal/value`")
	}
	ntCost, ntErr := strconv.Atoi(ntMatch[1])
	if ntErr != nil {
		t.Fatalf("custo ilegivel em %q: %v", ntMatch[0], ntErr)
	}
	if ntCost > inlineBigFunctionMaxCost {
		t.Errorf("value.NeverTracked tem custo de inline %d, maximo %d para ser inlinada dentro de run()", ntCost, inlineBigFunctionMaxCost)
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

// minPopInlineSitesInRun e uma margem sob os ~84 call sites de pop em
// executor.go (mesma logica de minPushInlineSitesInRun).
const minPopInlineSitesInRun = 70

// minEnsureCallCapacityInlineSitesInCalls e uma margem sob o unico call site
// de hoje (call(), calls.go).
const minEnsureCallCapacityInlineSitesInCalls = 1
