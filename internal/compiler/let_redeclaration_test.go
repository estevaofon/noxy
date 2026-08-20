package compiler

// Redeclaracao de `let` no mesmo escopo e erro de compilacao (spec §3): o
// segundo `let` criava um binding novo silenciosamente — inclusive trocando
// o tipo, furando a regra nº1 da §2.0 ("o tipo e definido na declaracao e
// nao pode mudar"). Reatribuicao (`x = ...`) segue sendo o caminho para
// atualizar o valor. Shadowing em escopo INTERNO continua legal: for-var
// sobre let externo, let em bloco if, corpo sombreando parametro (params em
// depth 1, corpo BlockStatement em depth 2).

import (
	"strings"
	"testing"
)

const redeclaredText = "redeclared in this scope"

func requireRedeclarationError(t *testing.T, src string, wantPrev string) {
	t.Helper()
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("redeclaracao de let no mesmo escopo deveria falhar na compilacao")
	}
	if !strings.Contains(err.Error(), redeclaredText) {
		t.Fatalf("erro deveria citar %q: %v", redeclaredText, err)
	}
	if !strings.Contains(err.Error(), wantPrev) {
		t.Fatalf("erro deveria apontar a declaracao anterior (%q): %v", wantPrev, err)
	}
}

func requireCompiles(t *testing.T, src string) {
	t.Helper()
	if _, _, err := New().Compile(parse(src)); err != nil {
		t.Fatalf("programa valido nao deveria falhar: %v", err)
	}
}

func TestLocalLetRedeclarationSameScopeFails(t *testing.T) {
	requireRedeclarationError(t, `func f()
    let x: int = 1
    let x: string = "virei string"
end`, "previous declaration at line 2")
}

func TestLocalLetRedeclarationSameTypeFails(t *testing.T) {
	requireRedeclarationError(t, `func f()
    let x: int = 1
    let x: int = 2
end`, "previous declaration at line 2")
}

func TestLetShadowingInInnerBlockAllowed(t *testing.T) {
	requireCompiles(t, `func f()
    let x: int = 1
    if x > 0 then
        let x: string = "sombra interna"
        print(x)
    end
    print(x)
end`)
}

func TestLetAfterForLoopVarAllowed(t *testing.T) {
	requireCompiles(t, `func f()
    for i in [1, 2, 3] do
        print(i)
    end
    let i: int = 2
    print(i)
end`)
}

func TestLetShadowingParamAllowed(t *testing.T) {
	requireCompiles(t, `func f(x: int)
    let x: string = "corpo sombreia parametro"
    print(x)
end`)
}
