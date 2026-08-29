package compiler

import (
	"strings"
	"testing"
)

// Revisao do PR #119 — regressoes e lacunas do narrowing.

// rootSharing chamava resolveUpvalue, que MUTA (marca IsCaptured e cria o
// upvalue): uma chave morta `p` deixada por um `let p` num laco dentro da
// closure capturava o `p` homonimo da funcao externa e derrubava o narrowing
// dela. Tambem: fatos de um local que sai de escopo nao podem sobreviver.
func TestNarrowingDeadFactDoesNotCaptureOuterLocal(t *testing.T) {
	src := "struct P\n    x: int\nend\nfunc toca() -> void\n    return\nend\nfunc f(p: P?) -> int\n    let g = func() -> void\n        for i in range(1) do\n            let p: P? = P(1)\n            if p == null then\n                continue\n            end\n        end\n        toca()\n    end\n    g()\n    if p != null then\n        g()\n        return p.x\n    end\n    return 2\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

// loopEffects decidia a pureza pelo nome ANTES de compilar o corpo: `let
// print = zera` dentro do laco passava como builtin puro e o fato sobrevivia.
func TestNarrowingLoopShadowedBuiltinIsImpure(t *testing.T) {
	src := narrowingGlobalMapPrelude + "func zera(v: any) -> void\n    m = null\nend\nif m != null then\n    let i: int = 0\n    while i < 2 do\n        let print = zera\n        print(m[\"nome\"])\n        i = i + 1\n    end\nend\n"
	_, err := compileFunctionSource(t, src)
	if err == nil || !strings.Contains(err.Error(), "'m' may be null") {
		t.Fatalf("want 'm' may be null, got %v", err)
	}
}

// Construtor de struct nao roda codigo do programa: e puro como um builtin.
func TestNarrowingStructConstructorIsPureInCondition(t *testing.T) {
	src := narrowingGlobalMapPrelude + "struct P\n    x: int\nend\nif m != null && P(1).x == 1 then\n    print(m[\"nome\"])\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestNarrowingStructConstructorIsPureBetweenUses(t *testing.T) {
	src := narrowingGlobalMapPrelude + "struct P\n    x: int\nend\nif m != null then\n    let p: P = P(1)\n    print(m[\"nome\"])\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

// eprint/iprint sao builtins centrais (spec §10) e nao rodam codigo Noxy.
func TestNarrowingGlobalSurvivesEprint(t *testing.T) {
	src := narrowingGlobalMapPrelude + "if m != null then\n    eprint(m[\"nome\"])\n    eprint(m[\"idade\"])\nend\n"
	if _, err := compileFunctionSource(t, src); err != nil {
		t.Fatal(err)
	}
}

// O registro do fato perdido na condicao vale so dentro do ramo.
func TestNarrowingLostInConditionDoesNotLeakOutsideTheBranch(t *testing.T) {
	src := narrowingCondPrelude + "if m != null && toca() then\n    print(\"x\")\nend\nprint(m[\"nome\"])\n"
	_, err := compileFunctionSource(t, src)
	want := "'m' may be null; test it first\n  hint: use 'if m != null then ... end'"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}

// O hint nao renderiza a chamada inteira (call.String() e um renderer de
// debug que perde aspas): so o nome do callee.
func TestNarrowingLostInConditionHintNamesTheCallee(t *testing.T) {
	src := narrowingGlobalMapPrelude + "func toca2(k: string) -> bool\n    m = null\n    return true\nend\nif m != null && toca2(\"k\") then\n    print(m[\"nome\"])\nend\n"
	_, err := compileFunctionSource(t, src)
	want := "hint: put the call before the test ('toca2(...) && m != null')"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("want %q, got %v", want, err)
	}
}
