package vm

import (
	"strings"
	"testing"
)

// ctorErrorBody devolve a mensagem sem o prefixo de posicao "[...:line N] ",
// para comparar erros emitidos em linhas diferentes.
func ctorErrorBody(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatalf("esperava erro de construtor, veio nil")
	}
	text := err.Error()
	if i := strings.Index(text, "] "); i >= 0 {
		return text[i+2:]
	}
	return text
}

// O cache do schema validado do construtor (issue #40 item 1 / #66 item 4)
// nao pode mudar o texto nem o momento do erro. O checker rejeita P(a) com
// a: any em compilacao; a fronteira dinamica e chamar o CONSTRUTOR atraves de
// any. Cache frio (1a construcao do programa) e cache quente (depois de uma
// construcao valida do mesmo struct) tem de dar a mesma mensagem.
func TestStructConstructorErrorsUnchangedWithCache(t *testing.T) {
	cold := ctorErrorBody(t, interpretVMSource(t, New(),
		"struct P\n    x: int\nend\nlet ctor: any = P\nlet p: any = ctor(\"s\")\n"))
	warm := ctorErrorBody(t, interpretVMSource(t, New(),
		"struct P\n    x: int\nend\nlet ok: P = P(1)\nlet ctor: any = P\nlet p: any = ctor(\"s\")\n"))
	if !strings.HasPrefix(cold, "function 'P' argument 1: expected int, got ") {
		t.Fatalf("cold = %q, want o erro de tipo do construtor", cold)
	}
	if cold != warm {
		t.Fatalf("cache mudou a mensagem: frio %q, quente %q", cold, warm)
	}
	// Aridade errada tambem igual nos dois estados.
	coldArity := ctorErrorBody(t, interpretVMSource(t, New(),
		"struct P\n    x: int\nend\nlet ctor: any = P\nlet p: any = ctor(1, 2)\n"))
	warmArity := ctorErrorBody(t, interpretVMSource(t, New(),
		"struct P\n    x: int\nend\nlet ok: P = P(1)\nlet ctor: any = P\nlet p: any = ctor(1, 2)\n"))
	if coldArity != warmArity || !strings.Contains(coldArity, "expected 1 arguments for struct P but got 2") {
		t.Fatalf("aridade: frio %q, quente %q", coldArity, warmArity)
	}
}

// Construcao valida repetida: o cache serve a partir da 2a e o resultado e o
// mesmo.
func TestStructConstructorCacheKeepsResults(t *testing.T) {
	got := semArray(t, captureVMSource(t, "struct P\n    x: int\n    y: string\nend\nlet i: int = 0\nlet s: int = 0\nwhile i < 1000 do\n    let p: P = P(i, \"a\")\n    s = s + p.x\n    i = i + 1\nend\ntest_report([s])\n"))
	if got[0].Int() != 499500 {
		t.Fatalf("got %s, want 499500", got[0].String())
	}
}
