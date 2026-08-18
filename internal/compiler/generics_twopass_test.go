package compiler

// Contratos do two-pass (spec §4/§5): memoizacao por tupla de tipos,
// igualdade de bytecode com a versao escrita a mao (§11) e erros de
// inferencia (§9).

import (
	"strings"
	"testing"

	"noxy-vm/internal/chunk"
	"noxy-vm/internal/value"
)

func TestMemoizationSingleInstance(t *testing.T) {
	c := New()
	code, _, err := c.Compile(parse("func id<T>(x: T) -> T\n    return x\nend\nid(1)\nid(2)"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, k := range code.Constants {
		if k.Type == value.VAL_FUNCTION && k.Obj.(*value.ObjFunction).Name == "main::id<int>" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("main::id<int> aparece %d vezes nos constants, quer 1 (memo)", count)
	}
}

// O teste mais forte da spec (§11): bytecode da instancia == bytecode da
// versao escrita a mao (modulo nome). Prova executavel do custo-zero.
func TestMonomorphizedBytecodeEqualsHandwritten(t *testing.T) {
	generic := compiledFunction(t, "func first<T>(arr: T[]) -> T\n    return arr[0]\nend\nlet xs: int[] = [1]\nfirst(xs)", "main::first<int>")
	hand := compiledFunction(t, "func first_int(arr: int[]) -> int\n    return arr[0]\nend\nlet xs: int[] = [1]\nfirst_int(xs)", "first_int")
	genericCode := generic.Chunk.(*chunk.Chunk).Code
	handCode := hand.Chunk.(*chunk.Chunk).Code
	if len(genericCode) != len(handCode) {
		t.Fatalf("tamanhos diferem: generico %d, a mao %d", len(genericCode), len(handCode))
	}
	for i := range genericCode {
		if genericCode[i] != handCode[i] {
			t.Fatalf("bytecode diverge no offset %d: %d vs %d", i, genericCode[i], handCode[i])
		}
	}
}

func TestInferenceConflictError(t *testing.T) {
	_, _, err := New().Compile(parse("func pick<T>(a: T, b: T) -> T\n    return a\nend\npick(1, \"x\")"))
	if err == nil || !strings.Contains(err.Error(), "inferido como") {
		t.Fatalf("esperava conflito T=int vs T=string, veio %v", err)
	}
}

// §9: T que nao aparece em nenhum parametro e nao tem hint do `let` e erro de
// inferencia com a mensagem exata do catalogo — nunca `any` implicito.
func TestInferenceWithoutAnchorError(t *testing.T) {
	_, _, err := New().Compile(parse("func vazio<T>() -> T[]\n    let out: T[] = []\n    return out\nend\nvazio()"))
	want := "[line 5] não foi possível inferir T em 'vazio' — anote o tipo"
	if err == nil || err.Error() != want {
		t.Fatalf("erro = %v, quer %q", err, want)
	}
}

// §7 uso 1: a anotacao do `let` envolvente ancora o T que so aparece no
// retorno. O mesmo programa sem anotacao e o erro do teste acima.
func TestLetAnnotationAnchorsReturnOnlyTypeParam(t *testing.T) {
	fn := compiledFunction(t,
		"func vazio<T>() -> T[]\n    let out: T[] = []\n    return out\nend\nlet xs: int[] = vazio()",
		"main::vazio<int>")
	if fn.Arity != 0 {
		t.Fatalf("aridade = %d", fn.Arity)
	}
}

// §9: erro dentro do corpo de uma instancia carrega a cadeia de
// instanciacao — linha do erro no template + linha do site de chamada.
func TestInstanceBodyErrorCarriesInstantiationChain(t *testing.T) {
	_, _, err := New().Compile(parse(`func precisa_int(x: int) -> int
    return x
end
func f<T>(v: T) -> int
    return precisa_int(v)
end
f("abc")`))
	if err == nil {
		t.Fatal("esperava erro de corpo da instancia")
	}
	message := err.Error()
	if !strings.HasPrefix(message, "[line ") {
		t.Fatalf("erro %q nao comeca com a linha do corpo", message)
	}
	for _, fragment := range []string{"em f<string> (instanciado na linha 7):", "expected int, got string"} {
		if !strings.Contains(message, fragment) {
			t.Fatalf("erro %q nao contem %q", message, fragment)
		}
	}
	// O prefixo "[line N]" do erro interno e levantado para o prefixo da
	// cadeia, nao duplicado no meio da mensagem.
	if count := strings.Count(message, "[line "); count != 1 {
		t.Fatalf("erro %q tem %d prefixos de linha, quer 1", message, count)
	}
}

// §5: programa sem genericos nao paga pass 1 — nenhuma fila de instancias
// chega a ser criada.
func TestNonGenericProgramSkipsPassOne(t *testing.T) {
	c := New()
	if _, _, err := c.Compile(parse("func f(x: int) -> int\n    return x\nend\nf(1)")); err != nil {
		t.Fatal(err)
	}
	if c.instances != nil {
		t.Fatal("programa sem genericos criou fila de instancias (pass 1 rodou)")
	}
}
