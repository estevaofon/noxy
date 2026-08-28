package vm

import "testing"

// Issue #105 item 2: `try` em runtime — valor no sucesso, early return com a
// mesma Failure na falha, defer rodando nos dois caminhos.

func TestTryReturnsFailureAndRunsDefer(t *testing.T) {
	src := `use errors select *
use convert select *
let log: string[] = []
func parse(s: string) -> Result<int>
    defer append(ref log, "defer " + s)
    let n: int = try to_int_result(s)
    return Ok(n * 2)
end
let a = parse("21")
let b = parse("x")
let av: int = 0
if a.ok then
    av = a.value
end
test_report(to_str(a.ok) + "|" + to_str(av) + "|" + to_str(b.ok) + "|" + to_str(b.failure.message != "") + "|" + to_str(length(log)))
`
	got := captureVMSource(t, src).String()
	if got != "true|42|false|true|2" {
		t.Fatalf("got %s", got)
	}
}

func TestTryAsStatementPropagatesVoidResult(t *testing.T) {
	src := `use errors select *
func falha() -> Result<bool>
    return Err("nope")
end
func usa() -> Result<int>
    try falha()
    return Ok(1)
end
let r = usa()
test_report(to_str(r.ok) + "|" + r.failure.message)
`
	if got := captureVMSource(t, src).String(); got != "false|nope" {
		t.Fatalf("got %s", got)
	}
}

func TestTryInsideExpressionDiscardsPartialOperands(t *testing.T) {
	src := `use errors select *
func meio(x: int) -> Result<int>
    if x < 0 then
        return Err("negativo")
    end
    return Ok(x)
end
func soma(a: int, b: int) -> Result<int>
    return Ok(1 + try meio(a) + try meio(b))
end
let ok_r = soma(2, 3)
let bad = soma(2, -1)
let v: int = 0
if ok_r.ok then
    v = ok_r.value
end
test_report(to_str(v) + "|" + to_str(bad.ok) + "|" + bad.failure.message)
`
	if got := captureVMSource(t, src).String(); got != "6|false|negativo" {
		t.Fatalf("got %s", got)
	}
}
