package compiler

// Task 11: cadeia de instanciacao (wrap de erros de corpo) + catalogo
// negativo restante do §9. A maior parte do catalogo ja tem teste das Tasks
// 8-10 (ver generics_test.go, generics_target_test.go, generics_struct_test.go,
// generics_twopass_test.go, generics_unify_test.go); este arquivo cobre:
//
//   - o exemplo LITERAL do §9 para "erro de corpo por instanciacao"
//     (operador nao definido para struct, nao so mismatch de tipo de argumento);
//   - a mensagem dedicada de inferencia so-com-null;
//   - "T fora de escopo" no `let` de topo (variante do que
//     generics_struct_test.go ja cobre para campo de struct);
//   - `any` explicito como argumento de tipo continua legal (§7);
//   - conflito de unificacao com atribuicao POR ARGUMENTO ("argumento 1"/"argumento 2");
//   - identificador de template (funcao e struct) em posicao de valor NAO
//     interceptada por nenhum hook (valor de map literal, expression
//     statement bare) — mesma mensagem de "precisa de tipo concreto";
//   - T bindando ref (ou prova de que isso nunca acontece silenciosamente).
//
// As linhas do catalogo cross-modulo (namespace, shadowing, escopo de
// definicao, dependencia nao importada) sao escopo da Task 12
// (.superpowers/sdd/2026-08-18-generics/task-12-brief.md) — cross-modulo
// generico ainda nao existe nesta base, entao nao ha comportamento real para
// testar aqui.

import (
	"strings"
	"testing"
)

// §9, ultima linha do catalogo, com o exemplo LITERAL da spec: operador
// aritmetico sem sentido para struct. Isto e DIFERENTE do que
// TestInstanceBodyErrorCarriesInstantiationChain (generics_twopass_test.go)
// ja cobre (mismatch de tipo em chamada de funcao dentro do corpo) — aqui o
// erro vem de um operador que a VM nunca soube executar sobre struct
// (executor.go OP_ADD cai em "operands must be numbers, strings or bytes").
// Sem checagem em tempo de compilacao isso e um crash de RUNTIME, fora do
// alcance de instantiationChainError (que so envolve erros de Compile) — daí
// a checagem nova em compiler.go (structOperandName/arithmeticOperators).
func TestBodyErrorCarriesInstantiationChain(t *testing.T) {
	src := `struct Ponto
    x: int
end
func soma<T>(a: T, b: T) -> T
    return a + b
end
let p1: Ponto = Ponto(1)
let p2: Ponto = Ponto(2)
soma(p1, p2)`
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("soma<Ponto> com + deve falhar")
	}
	for _, want := range []string{"soma<Ponto>", "instanciado na linha"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro sem %q: %v", want, err)
		}
	}
}

// §9: "inferência só com null" tem mensagem DEDICADA, distinta da generica
// "não foi possível inferir T em 'f'" — só dispara quando null é a ÚNICA
// razão da falha (nenhum outro argumento ancorou nada).
func TestNullOnlyInferenceError(t *testing.T) {
	_, _, err := New().Compile(parse("func id<T>(x: T) -> T\n    return x\nend\nid(null)"))
	if err == nil || !strings.Contains(err.Error(), "não foi possível inferir T de null") {
		t.Fatalf("esperava erro de inferencia com null, veio %v", err)
	}
}

// §9 "Param de tipo fora de escopo". Comportamento real verificado
// empiricamente (e documentado no comentário de TestTypeParamAnnotationIsRejected
// em generics_struct_test.go, que já cobre a mensagem exata do catálogo):
// parser.go só produz o nó *ast.TypeParamType DENTRO do escopo textual de uma
// declaração genérica, para os nomes efetivamente listados em `<...>`
// (p.activeTypeParams). Um `T` solto no `let` de TOPO nunca chega como
// TypeParamType — o parser o trata como um nome de tipo comum (e
// desconhecido), então resolveAnnotation nem entra em jogo (needsAnnotationResolution
// responde false para *ast.PrimitiveType) e "tipo 'T' não declarado" nunca é a
// mensagem aqui. O que ESTE teste prova é a outra metade da garantia da spec:
// `T` fora de escopo nunca vira um tipo "de mentirinha" que aceita qualquer
// valor silenciosamente — vira um nome de tipo comum e desconhecido, que a
// checagem de tipos do `let` rejeita do jeito de sempre (type mismatch,
// porque "T" nunca é "int"). Sem essa segunda garantia um usuário poderia
// escrever `T` por engano fora de um template e ver o programa compilar
// (aceitando qualquer valor como se T fosse `any`) em vez de falhar.
func TestTypeParamOutOfScopeError(t *testing.T) {
	_, _, err := New().Compile(parse("let x: T = 1"))
	if err == nil {
		t.Fatal("T fora de escopo deveria ser erro (nunca aceitar silenciosamente), veio nil")
	}
	if !strings.Contains(err.Error(), "type mismatch") {
		t.Fatalf("esperava erro de tipo (T tratado como nome de tipo comum e desconhecido), veio %v", err)
	}
}

// §7: any e legal como argumento de tipo explicito — incondicionalmente
// (não só quando nenhum argumento bindou nada): mesmo com `Caixa(1)`
// bindando T=int a partir do argumento, a anotação explícita `Caixa<any>`
// prevalece (applyStructHintBindings, generics_structs.go — ver comentário
// lá para o porquê de "any" ser um caso à parte de "argumento é âncora
// primária").
func TestStackAnyIsLegal(t *testing.T) {
	// §7: any e legal como argumento de tipo explicito
	_, _, err := New().Compile(parse("struct Caixa<T>\n    valor: T\nend\nlet c: Caixa<any> = Caixa(1)"))
	if err != nil {
		t.Fatalf("Caixa<any> deve compilar: %v", err)
	}
}

// §9 "conflito de unificação" com atribuição POR ARGUMENTO: a mensagem
// generica de TestInferenceConflictError (generics_twopass_test.go) só prova
// "inferido como X e Y"; o catálogo pede o argumento de origem de cada lado
// ("T inferido como int (argumento 1) e string (argumento 2)") — a mesma
// mensagem literal do exemplo `indice_de(idades, "30")` do §9.
func TestUnificationConflictAttributesArguments(t *testing.T) {
	src := `func indice_de<T>(arr: T[], alvo: T) -> int
    return 0
end
let idades: int[] = [30]
indice_de(idades, "30")`
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("esperava conflito de unificacao T=int vs T=string")
	}
	for _, want := range []string{
		"T inferido como int (argumento 1)",
		"string (argumento 2)",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro sem %q: %v", want, err)
		}
	}
}

// Mesmas duas mensagens (conflito com atribuição por argumento, inferência
// só com null), agora via o CONSTRUTOR de struct genérico
// (compileGenericConstructorSite, generics_structs.go) — a review da Task 11
// apontou que a primeira rodada só cobriu o call site de FUNÇÃO
// (compileGenericCallSite), embora os dois compartilhem a mesma máquina
// (unifyPositionalArguments/missingTypeParamNullError, generics.go).
func TestConstructorConflictAttributesArguments(t *testing.T) {
	src := `struct Par<T>
    a: T
    b: T
end
Par(1, "x")`
	_, _, err := New().Compile(parse(src))
	if err == nil {
		t.Fatal("esperava conflito de unificacao T=int vs T=string no construtor")
	}
	for _, want := range []string{
		"T inferido como int (argumento 1)",
		"string (argumento 2)",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("erro sem %q: %v", want, err)
		}
	}
}

func TestConstructorNullOnlyInferenceError(t *testing.T) {
	src := `struct Caixa<T>
    valor: T
end
Caixa(null)`
	_, _, err := New().Compile(parse(src))
	if err == nil || !strings.Contains(err.Error(), "não foi possível inferir T de null") {
		t.Fatalf("esperava erro de inferencia com null no construtor, veio %v", err)
	}
}

// §9 "Valor sem alvo concreto": TestGenericWithoutConcreteTargetIsError
// (generics_target_test.go) ja cobre as posicoes HOOKADAS (`let` anotado com
// `func`/`any`). Este teste cobre as posicoes que NENHUM hook intercepta —
// valor de map literal e expression statement solto — onde um identificador
// nomeando um template de FUNCAO alcancava o `case *ast.Identifier` generico
// de compiler.go sem nenhuma reescrita, lendo um global que nunca foi
// definido (mensagem confusa a jusante, tipicamente sobre global
// desconhecido). O fallback fica no proprio case, disparando so quando
// nenhum hook anterior ja rescreveu o identificador (resolveLocal/
// resolveUpvalue vazios — mesma regra de sombreamento dos outros hooks).
func TestUnhookedFunctionTemplateValuePositionIsError(t *testing.T) {
	for name, src := range map[string]string{
		"map literal value":    "func id<T>(x: T) -> T\n    return x\nend\nlet m: map[string, func] = {\"f\": id}",
		"expression statement": "func id<T>(x: T) -> T\n    return x\nend\nid",
	} {
		_, _, err := New().Compile(parse(src))
		if err == nil || !strings.Contains(err.Error(), "precisa de tipo concreto") {
			t.Fatalf("%s: esperava erro de tipo concreto, veio %v", name, err)
		}
	}
}

// Mesmo tratamento (item 4 da task): struct generico bare como valor, nas
// mesmas duas posicoes desprotegidas.
func TestUnhookedStructTemplateValuePositionIsError(t *testing.T) {
	for name, src := range map[string]string{
		"map literal value":    "struct Caixa<T>\n    valor: T\nend\nlet m: map[string, func] = {\"c\": Caixa}",
		"expression statement": "struct Caixa<T>\n    valor: T\nend\nCaixa",
	} {
		_, _, err := New().Compile(parse(src))
		if err == nil || !strings.Contains(err.Error(), "precisa de tipo concreto") {
			t.Fatalf("%s: esperava erro de tipo concreto, veio %v", name, err)
		}
	}
}

// As 5 posicoes HOOKADAS (§3/§4) e chamada direta continuam funcionando —
// prova de que o fallback do case Identifier so dispara quando NENHUM hook
// intercepta antes. `let` anotado, retorno, elemento de array, campo de
// struct e argumento de chamada (as 5 do §3) mais a chamada direta em si
// (callee de CallExpression).
func TestHookedPositionsStillWorkAfterUnhookedFallback(t *testing.T) {
	for name, src := range map[string]string{
		"let anotado":       "func id<T>(x: T) -> T\n    return x\nend\nlet f: func(int) -> int = id",
		"chamada direta":    "func id<T>(x: T) -> T\n    return x\nend\nid(1)",
		"elemento de array": "func id<T>(x: T) -> T\n    return x\nend\nlet fs: (func(int) -> int)[] = [id]",
		"argumento de chamada": `func aplica<A, B>(f: func(A) -> B, x: A) -> B
    return f(x)
end
func id<T>(x: T) -> T
    return x
end
aplica(id, 1)`,
		"construtor de struct direto": "struct Caixa<T>\n    valor: T\nend\nCaixa(1)",
	} {
		_, _, err := New().Compile(parse(src))
		if err != nil {
			t.Fatalf("%s: deveria compilar, veio %v", name, err)
		}
	}
	// Retorno e campo de struct: as duas posicoes restantes do §3/§4,
	// exercitadas em programas separados (retorno de func nao-generica
	// devolvendo o generico como valor; atribuicao a campo de struct
	// recebendo o generico como valor).
	returnSrc := `func id<T>(x: T) -> T
    return x
end
func devolve() -> func(int) -> int
    return id
end
devolve()`
	if _, _, err := New().Compile(parse(returnSrc)); err != nil {
		t.Fatalf("retorno hookado: deveria compilar, veio %v", err)
	}
	fieldSrc := `struct Handler
    callback: func(int) -> int
end
func id<T>(x: T) -> T
    return x
end
let h: Handler = Handler(func(v: int) -> int
    return v
end)
h.callback = id`
	if _, _, err := New().Compile(parse(fieldSrc)); err != nil {
		t.Fatalf("campo de struct hookado: deveria compilar, veio %v", err)
	}
}

// §9: "T bindando ref" — a spec exige que T=ref NUNCA aconteça
// silenciosamente. Comportamento real verificado empiricamente: o `let`
// alvo (`ref int`) e o VALOR (`r`, tipo `ref int`) batem sem generico
// nenhum no meio — rewriteIfGenericValue so intercepta identificador NU
// nomeando um TEMPLATE DE FUNCAO (aqui e `id`, que E chamado, entao o hook
// de call site e quem atua, nao o de target-typing). No call site,
// compileGenericCallSite chama typeOfDiscardedExpression(r), que retorna o
// tipo ESTATICO de `r` — `ref int` — sem auto-deref (auto-deref so acontece
// para PARAMETROS concretos tipados sem `ref`, e aqui o parametro e `x: T`,
// ainda generico, unify decide). unify(T, ref int, ...) cai exatamente no
// case `tp, ok := expected.(*ast.TypeParamType)` de generics_unify.go, que
// rejeita `ref` como alvo de binding ANTES de qualquer outra regra —
// exatamente a garantia que a spec pede.
func TestRefBindingError(t *testing.T) {
	_, _, err := New().Compile(parse("func id<T>(x: T) -> T\n    return x\nend\nlet r: ref int = null\nlet v: ref int = id(r)"))
	if err == nil || !strings.Contains(err.Error(), "não pode ser um tipo ref") {
		t.Fatalf("T=ref deve ser erro de unificacao, veio %v", err)
	}
}
