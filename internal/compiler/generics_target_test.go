package compiler

// §9: catalogo de erros do target-typing (§3) — identificador de template de
// funcao em posicao de valor sem alvo concreto (`func` nu, `any`) e corrente
// de genericos sem ancora (`compose(id, id)`, nada fixa o tipo do meio).

import (
	"errors"
	"strings"
	"testing"

	"noxy-vm/internal/ast"
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

// I5 (1) da revisao final de branch: as duas rotas de target-typing que
// unificam uma assinatura de TEMPLATE contra um tipo CONCRETO precisam da
// mesma ponte de nomes de instancia que o caminho principal
// (unifyAnnotation/expandInstanceNames) — o template escreve `Caixa<T>` e o
// mundo escreve `main::Caixa<int>`. Antes, um parametro de struct generico
// fazia as duas rotas falharem com "esperava main::Caixa<int>, encontrado
// Caixa<T>" (ou o inverso): uma comparacao entre duas grafias do MESMO tipo.
func TestGenericStructParamUnifiesThroughValuePositions(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			// Rota unifyBidirectional (posicao 5 do §3: argumento de chamada
			// de OUTRA generica).
			name: "argumento de chamada generica",
			src: `struct Caixa<T>
    valor: T
end
func pega<T>(c: Caixa<T>) -> T
    return c.valor
end
func aplica<A, B>(x: A, fn: func(A) -> B) -> B
    return fn(x)
end
let c: Caixa<int> = Caixa(42)
aplica(c, pega)`,
		},
		{
			// Rota instantiateForTarget (posicao 1 do §3: `let` anotado).
			name: "let anotado com tipo de funcao",
			src: `struct Caixa<T>
    valor: T
end
func pega<T>(c: Caixa<T>) -> T
    return c.valor
end
let cx: Caixa<int> = Caixa(7)
let f: func(Caixa<int>) -> int = pega`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := New().Compile(parse(tt.src)); err != nil {
				t.Fatalf("deveria instanciar pega<int>, veio %v", err)
			}
		})
	}
}

// I5 (2a): bindTypeParam devolve *conflictError, o mesmo tipo estruturado que
// unify devolve — nao um fmt.Errorf com texto igual. Sem o tipo, nenhum
// chamador consegue extrair Param/Existing/New via errors.As para compor a
// atribuicao por argumento do §9.
func TestBindTypeParamConflictIsStructured(t *testing.T) {
	bindings := map[string]ast.NoxyType{"T": &ast.PrimitiveType{Name: "int"}}
	err := bindTypeParam(bindings, "T", &ast.PrimitiveType{Name: "string"})
	if err == nil {
		t.Fatal("binding divergente deveria conflitar")
	}
	var conflict *conflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("erro = %T (%v), quer *conflictError", err, err)
	}
	if conflict.Param != "T" || conflict.Existing.String() != "int" || conflict.New.String() != "string" {
		t.Fatalf("conflito = {%s, %s, %s}", conflict.Param, conflict.Existing, conflict.New)
	}
	if !strings.Contains(err.Error(), "T inferido como int e string") {
		t.Fatalf("mensagem mudou: %v", err)
	}
}

// Issue #44 (1): a anotacao de retorno da funcao envolvente e ancora de
// target-typing para chamada generica em posicao de `return` — simetrica a
// ancora do `let` anotado (§6.2). Cobre T que so aparece no retorno do
// template, tanto funcao quanto construtor de struct generico.
func TestReturnPositionAnchorsGenericCall(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{
			name: "funcao com T so no retorno",
			src: `func vazia<T>() -> T[]
    let xs: T[] = []
    return xs
end
func faz() -> int[]
    return vazia()
end`,
		},
		{
			name: "construtor generico sem ancora nos argumentos",
			src: `struct Pilha<T>
    itens: T[]
end
func nova() -> Pilha<int>
    return Pilha([])
end`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, err := New().Compile(parse(tt.src)); err != nil {
				t.Fatalf("retorno anotado deveria ancorar o tipo, veio %v", err)
			}
		})
	}
}

// Issue #44 (1), caso de erro: quando a anotacao de retorno NAO casa com o
// retorno do template, a falha e a mesma do caminho do `let` ("retorno de
// 'f': ..."), nao a generica "não foi possível inferir T".
func TestReturnPositionHintMismatchIsError(t *testing.T) {
	src := `func vazia<T>() -> T[]
    let xs: T[] = []
    return xs
end
func faz() -> int
    return vazia()
end`
	_, _, err := New().Compile(parse(src))
	if err == nil || !strings.Contains(err.Error(), "retorno de 'vazia'") {
		t.Fatalf("mismatch de retorno deveria unificar e falhar com atribuicao, veio %v", err)
	}
}

// I5 (2b): erro vindo da passada BIDIRECIONAL carrega a atribuicao por
// argumento — antes o unico contexto era o indice do argumento-template, sem
// nenhum caminho para a mensagem "(argumento N) e (argumento M)" do §9.
func TestBidirectionalArgumentErrorCarriesArgumentAttribution(t *testing.T) {
	src := `func par<T>(a: T, b: T) -> T
    return a
end
func aplica<A, B>(x: A, fn: func(A, string) -> B) -> B
    return fn(x, "s")
end
aplica(1, par)`
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("par<T> nao pode casar com func(int, string) -> B")
	}
	if !strings.Contains(err.Error(), "argumento 2 de 'aplica'") {
		t.Fatalf("erro %v sem atribuicao de argumento", err)
	}
}
