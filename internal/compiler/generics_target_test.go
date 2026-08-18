package compiler

// §9: catalogo de erros do target-typing (§3) — identificador de template de
// funcao em posicao de valor sem alvo concreto (`func` nu, `any`) e corrente
// de genericos sem ancora (`compose(id, id)`, nada fixa o tipo do meio).

import (
	"strings"
	"testing"
)

func TestGenericWithoutConcreteTargetIsError(t *testing.T) {
	for _, src := range []string{
		"func id<T>(x: T) -> T\n    return x\nend\nlet g: func = id",
		"func id<T>(x: T) -> T\n    return x\nend\nlet h: any = id",
	} {
		_, _, err := New().Compile(parse(src))
		if err == nil || !strings.Contains(err.Error(), "precisa de tipo concreto") {
			t.Fatalf("fonte %q: esperava erro de alvo, veio %v", src, err)
		}
	}
}

func TestUnanchoredGenericChainIsError(t *testing.T) {
	src := `func id<T>(x: T) -> T
    return x
end
func compose<A, B, C>(f: func(A) -> B, g: func(B) -> C) -> int
    return 0
end
compose(id, id)`
	_, _, err := New().Compile(parse(src))
	if err == nil || !strings.Contains(err.Error(), "anote o tipo") {
		t.Fatalf("corrente sem ancora deve pedir anotacao, veio %v", err)
	}
}
