package vm

import (
	"strings"
	"testing"
)

// Achado 4 do Noxy-Editor: `use strings select *` traz strings.contains
// para o escopo e sombreia o builtin contains(arr, v); a chamada com um
// array vira um erro de tipo que nao explica de onde veio o `contains`.
// O erro ganha um hint nomeando o sombreamento e a saida (importar por
// nome). Nao e aviso no `use`: quem importa strings inteiro e nunca chama
// contains com array nao tem nada a corrigir.
func TestWildcardImportShadowingABuiltinExplainsTheTypeError(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
use strings select *
let keywords: string[] = ["let", "func"]
let found: bool = contains(keywords, "let")`)
	want := "argument 1 to 'contains': expected string, got string[]\n" +
		"  hint: 'contains' here is strings.contains (imported by 'use strings select *'), which shadows the builtin 'contains'; import strings by name ('use strings select ...') to keep the builtin"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%v\nwant %q", err, want)
	}
}

func TestWildcardImportShadowingABuiltinExplainsTheArityError(t *testing.T) {
	err := interpretOrCompileErr(t, New(), `
use http_client select *
let m: map[string, int] = {"a": 1}
delete(ref m, "a")`)
	want := "function 'delete' expects 1 arguments, got 2\n" +
		"  hint: 'delete' here is http_client.delete (imported by 'use http_client select *'), which shadows the builtin 'delete'; import http_client by name ('use http_client select ...') to keep the builtin"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("error=%v\nwant %q", err, want)
	}
}

func TestImportingByNameKeepsTheBuiltinContains(t *testing.T) {
	got := captureVMSource(t, `
use strings select substring, trim
let keywords: string[] = ["let", "func"]
test_report(to_str(contains(keywords, "let")) + "|" + to_str(contains("banana", "nan")))`)
	// contains(string, string) continua sendo o builtin: false para
	// substring (o builtin compara elementos, nao procura substring).
	if s, _ := got.Obj.(string); s != "true|false" {
		t.Fatalf("got %q, want %q", s, "true|false")
	}
}
