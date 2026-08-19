// internal/vm/builtins_call_result_test.go
package vm

import (
	"errors"
	"strings"
	"testing"

	"noxy-vm/internal/value"
)

func TestErrorsModuleShapes(t *testing.T) {
	source := `
use errors select *

let f: Failure = Failure("runtime", "boom", "st", [])
let nested: Failure = Failure("runtime", "outer", "st", [f])
let r: CallResult = CallResult(true, 42, null)
test_report(nested.causes[0].message + "|" + to_str(r.ok) + "|" + to_str(r.value))
`
	reported := captureVMSource(t, source)
	text, ok := reported.Obj.(string)
	if !ok || text != "boom|true|42" {
		t.Fatalf("unexpected report: %#v", reported)
	}
}

func TestCallResultOkPaths(t *testing.T) {
	source := `
use errors select *

func dobro(x: int) -> int
    return x * 2
end

func nada()
end

struct P
    x: int
end

let a: CallResult = call_result(dobro, 21)
let b: CallResult = call_result(to_int, "5")
let c: CallResult = call_result(P, 7)
let d: CallResult = call_result(nada)
let inst: any = c.value
test_report(to_str(a.ok) + "|" + to_str(a.value) + "|" + to_str(b.value) + "|" + to_str(inst.x) + "|" + to_str(d.ok) + "|" + to_str(d.value == null) + "|" + to_str(a.failure == null))
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	if text != "true|42|5|7|true|true|true" {
		t.Fatalf("unexpected report: %q", text)
	}
}

func TestCallResultEnvelopeIsMap(t *testing.T) {
	source := `
let r: any = call_result(to_int, "5")
test_report(fmt("%T", r))
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "map" {
		t.Fatalf("envelope should be a map at the dynamic boundary, got %q", text)
	}
}

func TestCallResultCapturesRuntimeError(t *testing.T) {
	source := `
use errors select *

func quebra(texto: string) -> int
    return to_int(texto)
end

let r: CallResult = call_result(quebra, "abc")
let depois: int = 40 + 2
test_report(to_str(r.ok) + "|" + r.failure.kind + "|" + r.failure.message + "|" + to_str(length(r.failure.causes)) + "|" + to_str(r.value == null) + "|" + to_str(depois))
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	parts := strings.Split(text, "|")
	if len(parts) != 6 || parts[0] != "false" || parts[1] != "runtime" || parts[3] != "0" || parts[4] != "true" || parts[5] != "42" {
		t.Fatalf("unexpected report: %q", text)
	}
	if !strings.Contains(parts[2], "cannot convert") || strings.Contains(parts[2], "use to_int_result") {
		t.Fatalf("message wrong or advisory suffix leaked: %q", parts[2])
	}
}

func TestCallResultFailureStackExcludesBoundary(t *testing.T) {
	source := `
func fundo() -> int
    return to_int("x")
end
func meio() -> int
    return fundo()
end
let r: any = call_result(meio)
test_report(r.failure.stack)
`
	reported := captureVMSource(t, source)
	stack, _ := reported.Obj.(string)
	if !strings.Contains(stack, "in fundo") || !strings.Contains(stack, "in meio") {
		t.Fatalf("stack missing inner frames: %q", stack)
	}
	if strings.Contains(stack, "call_result") {
		t.Fatalf("stack must stop before the boundary frame: %q", stack)
	}
}

func TestCallResultCapturesStackOverflow(t *testing.T) {
	source := `
func infinita() -> int
    return infinita()
end
let r: any = call_result(infinita)
test_report(to_str(r.ok) + "|" + r.failure.kind + "|" + r.failure.message)
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	if !strings.HasPrefix(text, "false|runtime|") || !strings.Contains(text, "stack overflow") {
		t.Fatalf("frame exhaustion should be captured: %q", text)
	}
}

// TestFailureMapMergesInnerCausesWithSiblingsOnPromotion cobre um bug real:
// no caso cleanup-first, deferredFailureMap promove o primeiro DeferredError
// a falha primaria. Se a Cause desse promovido ja e um *UnwindError com seu
// proprio Primary+Deferred (i.e. a falha promovida ja tinha causes proprias
// aninhadas), o merge com os siblings (falhas diferidas posteriores no mesmo
// frame) precisa PRESERVAR essas causes internas e so anexar os siblings por
// cima — nunca substituir. Constroi a arvore de erro diretamente em Go (via
// failureMap) porque replicar "defer cujo Cause e um UnwindError aninhado"
// de forma confiavel a partir de fonte Noxy exigiria coordenar timing de
// falhas concorrentes de defer; a arvore de erro e o dado relevante aqui, nao
// o caminho de execucao que a produz.
func TestFailureMapMergesInnerCausesWithSiblingsOnPromotion(t *testing.T) {
	innerSibling := DeferredError{
		Registration: SourceLocation{File: "inner.nx", Line: 3},
		Cause:        errors.New("inner sibling failure"),
	}
	promoted := DeferredError{
		Registration: SourceLocation{File: "outer.nx", Line: 5},
		Cause: &UnwindError{
			Primary:  errors.New("inner primary failure"),
			Deferred: []DeferredError{innerSibling},
		},
	}
	outerSibling := DeferredError{
		Registration: SourceLocation{File: "outer.nx", Line: 9},
		Cause:        errors.New("outer sibling failure"),
	}
	unwind := &UnwindError{
		Primary:  nil,
		Deferred: []DeferredError{promoted, outerSibling},
	}

	result := failureMap(unwind)
	mapping, ok := result.Obj.(*value.ObjMap)
	if !ok {
		t.Fatalf("failureMap did not return a map: %#v", result)
	}
	message, _ := mapping.Get("message")
	if text, _ := message.Obj.(string); text != "inner primary failure" {
		t.Fatalf("promoted primary should keep the inner UnwindError's message, got %q", text)
	}

	causesValue, ok := mapping.Get("causes")
	if !ok {
		t.Fatalf("promoted failure missing causes key")
	}
	array, ok := causesValue.Obj.(*value.ObjArray)
	if !ok {
		t.Fatalf("causes is not an array: %#v", causesValue)
	}
	if len(array.Elements) != 2 {
		t.Fatalf("want 2 causes (1 inner + 1 outer sibling), got %d: %#v", len(array.Elements), array.Elements)
	}

	causeMessage := func(v value.Value) string {
		m, ok := v.Obj.(*value.ObjMap)
		if !ok {
			t.Fatalf("cause is not a map: %#v", v)
		}
		msg, _ := m.Get("message")
		text, _ := msg.Obj.(string)
		return text
	}
	if got := causeMessage(array.Elements[0]); got != "inner sibling failure" {
		t.Fatalf("first cause should be the promoted failure's own inner cause, got %q", got)
	}
	if got := causeMessage(array.Elements[1]); got != "outer sibling failure" {
		t.Fatalf("second cause should be the outer sibling, got %q", got)
	}
}

// TestCallResultAggregatesDeferFailures cobre a agregacao ponta-a-ponta: o
// corpo falha (to_int com string invalida), o defer de limpeza registrado no
// corpo TAMBEM falha durante o unwind, e a falha diferida precisa aparecer em
// r.failure.causes[0] com kind "runtime", a mensagem original preservada, e o
// stack carregando "defer registration" como frame mais externo (a
// localizacao de REGISTRO do defer, nao a de falha).
func TestCallResultAggregatesDeferFailures(t *testing.T) {
	source := `
func limpeza_ruim()
    to_int("defer-quebrado")
end

func corpo() -> int
    defer limpeza_ruim()
    return to_int("primario")
end

let r: any = call_result(corpo)
let causa: any = r.failure.causes[0]
test_report(to_str(length(r.failure.causes)) + "|" + causa.kind + "|" + causa.message + "|" + causa.stack)
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	parts := strings.SplitN(text, "|", 4)
	if len(parts) != 4 || parts[0] != "1" || parts[1] != "runtime" {
		t.Fatalf("unexpected report: %q", text)
	}
	if !strings.Contains(parts[2], "defer-quebrado") {
		t.Fatalf("cause message should carry the deferred failure: %q", parts[2])
	}
	if !strings.Contains(parts[3], "defer registration") {
		t.Fatalf("cause stack must carry the registration location as outermost frame: %q", parts[3])
	}
}

// TestCallResultCleanupFirstFailure cobre o caso "cleanup as first failure"
// (design §2): o corpo TERMINA COM SUCESSO (return 42) mas o defer de
// limpeza falha durante o unwind — cleanup-first promove essa falha diferida
// a falha PRIMARIA do envelope (ok=false, value=null), descartando o valor
// computado, com zero causes (nao ha falha primaria original para aninhar
// sob ela).
func TestCallResultCleanupFirstFailure(t *testing.T) {
	source := `
func limpeza_ruim()
    to_int("so-o-defer-quebra")
end

func corpo() -> int
    defer limpeza_ruim()
    return 42
end

let r: any = call_result(corpo)
test_report(to_str(r.ok) + "|" + to_str(r.value == null) + "|" + r.failure.message + "|" + to_str(length(r.failure.causes)))
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	parts := strings.SplitN(text, "|", 4)
	if len(parts) != 4 || parts[0] != "false" || parts[1] != "true" || parts[3] != "0" {
		t.Fatalf("cleanup-first must discard the computed value and promote the deferred failure: %q", text)
	}
	if !strings.Contains(parts[2], "so-o-defer-quebra") {
		t.Fatalf("primary failure should be the deferred one: %q", parts[2])
	}
}

// TestCallResultCallerDefersUnaffected cobre isolamento do frame chamador: a
// captura de falha em call_result(corpo) NAO deve interromper a execucao do
// chamador nem disparar prematuramente o defer do PROPRIO chamador — a
// execucao continua apos a captura (append "depois-da-captura" roda) e o
// defer do chamador so dispara quando a FUNCAO CHAMADORA retorna (append
// "caller-defer" fica por ultimo). Vehicle sem adaptacao: `trilha` e o global
// mutado por append de dentro de func e closure exatamente como no brief —
// verificado a parte (fora do arquivo final de teste) que mutacao de global
// via append dentro de funcoes chega visivel ao `let` de nivel superior
// (mesmo padrao de TestGlobalPathMutationStillWorks em
// value_semantics_test.go, so que via chamada de native com parametro ref
// em vez de indexacao direta); a ressalva do brief sobre CoW/global nao se
// materializou aqui, entao a asserção original fica com o vetor de prova
// mais direto.
func TestCallResultCallerDefersUnaffected(t *testing.T) {
	source := `
let trilha: string[] = []

func marca(rotulo: string)
    append(trilha, rotulo)
end

func corpo() -> int
    return to_int("x")
end

func chamador() -> string
    defer marca("caller-defer")
    let r: any = call_result(corpo)
    append(trilha, "depois-da-captura")
    return to_str(r.ok)
end

let ok_text: string = chamador()
test_report(ok_text + "|" + trilha[0] + "|" + trilha[1])
`
	reported := captureVMSource(t, source)
	text, _ := reported.Obj.(string)
	if text != "false|depois-da-captura|caller-defer" {
		t.Fatalf("caller frame must be unaffected by the capture: %q", text)
	}
}

func TestCallResultCapturesGoPanic(t *testing.T) {
	machine := New()
	machine.DefineNative("explode", func(args []value.Value) value.Value {
		panic("boom-nativo")
	})
	source := `
func corpo() -> int
    explode()
    return 1
end
let r: any = call_result(corpo)
test_report(to_str(r.ok) + "|" + r.failure.kind + "|" + r.failure.message)
`
	captured := value.NewNull()
	machine.DefineNative("test_report", func(args []value.Value) value.Value {
		if len(args) != 0 {
			captured = args[0]
		}
		return value.NewNull()
	})
	if err := interpretVMSource(t, machine, source); err != nil {
		t.Fatalf("vm error: %v", err)
	}
	text, _ := captured.Obj.(string)
	if !strings.HasPrefix(text, "false|panic|") || !strings.Contains(text, "boom-nativo") {
		t.Fatalf("panic must be captured as kind=panic: %q", text)
	}
}

func TestCallResultNestedBoundaries(t *testing.T) {
	source := `
func interna() -> int
    return to_int("x")
end
func externa() -> string
    let r: any = call_result(interna)
    return "interna-capturou:" + to_str(r.ok)
end
let fora: any = call_result(externa)
test_report(to_str(fora.ok) + "|" + fora.value)
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "true|interna-capturou:false" {
		t.Fatalf("nearest boundary must capture: %q", text)
	}
}

func TestCallResultNoRollback(t *testing.T) {
	source := `
func muta_e_quebra(alvo: ref int) -> int
    *alvo = 99
    return to_int("x")
end
let caixa: int = 1
let r: any = call_result(muta_e_quebra, ref caixa)
test_report(to_str(r.ok) + "|" + to_str(caixa))
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "false|99" {
		t.Fatalf("mutations before the failure must remain (no rollback): %q", text)
	}
}

func TestCallResultValueSemantics(t *testing.T) {
	source := `
func faz_array() -> int[]
    return [1, 2, 3]
end
let r: any = call_result(faz_array)
let copia: int[] = r.value
copia[0] = 100
let original: any = r.value
test_report(to_str(original[0]) + "|" + to_str(copia[0]) + "|" + to_str(length(original)))
`
	reported := captureVMSource(t, source)
	if text, _ := reported.Obj.(string); text != "1|100|3" {
		t.Fatalf("composite value must obey CoW semantics without corruption: %q", text)
	}
}

func TestCallResultMisuseRaisesSynchronously(t *testing.T) {
	cases := []struct {
		name, source, wantErr string
	}{
		{"non-callable", `call_result(42)`, "call_result expects a callable"},
		{"closure arity", `
func soma(a: int, b: int) -> int
    return a + b
end
call_result(soma, 1)`, "expected 2 arguments but got 1"},
		{"constructor arity", `
struct P
    x: int
end
call_result(P, 1, 2)`, "expected 1 arguments for struct P"},
		{"no arguments at all", `call_result()`, "call_result expects a callable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machine := New()
			err := interpretVMSource(t, machine, tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("want synchronous error containing %q, got: %v", tc.wantErr, err)
			}
		})
	}
}
