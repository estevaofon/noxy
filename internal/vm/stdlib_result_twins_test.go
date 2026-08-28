package vm

import (
	"path/filepath"
	"testing"
)

// tempFilePath devolve um caminho temporario com barras normais (uma barra
// invertida seria escape dentro da string Noxy).
func tempFilePath(t *testing.T) string {
	t.Helper()
	return filepath.ToSlash(filepath.Join(t.TempDir(), "twins.txt"))
}

// Issue #105 item 2: as gemeas _result da stdlib devolvem errors.Result<T>.

func TestConvertResultTwinsReturnResult(t *testing.T) {
	src := `use convert select *
let a = to_int_result("12")
let b = to_int_result("x")
let f = to_float_result("2.5")
let av: int = 0
if a.ok then
    av = a.value
end
let fv: float = 0.0
if f.ok then
    fv = f.value
end
test_report(to_str(a.ok) + "|" + to_str(av) + "|" + to_str(b.ok) + "|" + to_str(b.failure.message != "") + "|" + to_str(fv) + "|" + fmt("%T", a))
`
	got := captureVMSource(t, src).String()
	want := "true|12|false|true|2.5"
	if len(got) < len(want) || got[:len(want)] != want || !containsSubstring(got, "Result<int>") {
		t.Fatalf("got %s", got)
	}
}

func TestJSONDumpsResultReturnsResult(t *testing.T) {
	src := `use json select *
let r = dumps_result({"a": 1})
let s: string = ""
if r.ok then
    s = r.value
end
test_report(to_str(r.ok) + "|" + s)
`
	if got := captureVMSource(t, src).String(); got != "true|{\"a\":1}" {
		t.Fatalf("got %s", got)
	}
}

func TestIOCloseResultReturnsResult(t *testing.T) {
	src := `use io select *
let f: File = open("` + tempFilePath(t) + `", "w")
let w = write_result(f, "abc")
let c = close_result(f)
let again = close_result(f)
let n: int = 0
if w.ok then
    n = w.value
end
test_report(to_str(w.ok) + "|" + to_str(n) + "|" + to_str(c.ok) + "|" + to_str(again.ok))
`
	if got := captureVMSource(t, src).String(); got != "true|3|true|false" {
		t.Fatalf("got %s", got)
	}
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
