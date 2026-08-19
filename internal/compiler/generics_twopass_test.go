package compiler

// Contratos do two-pass (spec §4/§5): memoizacao por tupla de tipos,
// igualdade de bytecode com a versao escrita a mao (§11) e erros de
// inferencia (§9).

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
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

// M6 da revisao final de branch: o teste acima compara SO Code, e sobre um
// corpo (`return arr[0]`) que nao emite nenhum opcode tipado nem povoa a
// tabela de constantes — ou seja, nao prova a manchete da spec §1 ("o codigo
// dentro da generica e tao especializado quanto o escrito a mao"). Este par
// fecha as duas lacunas:
//
//   - corpo com aritmetica de int, e a asserção de que OP_ADD_INT (a
//     especializacao que o compilador so emite quando SABE que os dois lados
//     sao int) aparece DENTRO do corpo monomorfizado. Se a instancia
//     compilasse com o tipo cru do template, `a + b` cairia no OP_ADD
//     generico;
//   - comparacao da tabela de CONSTANTES alem do Code: dois chunks podem ter
//     bytes identicos e indices apontando para constantes diferentes.
func TestMonomorphizedBytecodeMatchesHandwrittenWithTypedOpcodes(t *testing.T) {
	const body = `
    let rotulo: string = "soma"
    print(rotulo)
    return a + b + 7
end
`
	generic := compiledFunction(t,
		"func soma<T>(a: T, b: T) -> T"+body+"let total: int = soma(2, 3)",
		"main::soma<int>")
	hand := compiledFunction(t,
		"func soma_int(a: int, b: int) -> int"+body+"let total: int = soma_int(2, 3)",
		"soma_int")

	genericChunk := generic.Chunk.(*chunk.Chunk)
	handChunk := hand.Chunk.(*chunk.Chunk)

	if !containsOpcode(handChunk.Code, chunk.OP_ADD_INT) {
		t.Fatal("a versao a mao nao emitiu OP_ADD_INT — o teste perdeu o alvo")
	}
	if !containsOpcode(genericChunk.Code, chunk.OP_ADD_INT) {
		t.Fatal("instancia sem OP_ADD_INT: `a + b` compilou com o tipo cru do template (§1)")
	}
	// containsOpcode e uma varredura de bytes crua (pode casar com um operando).
	// O contraprova abaixo mostra que, para ESTE formato de corpo, a presenca
	// do byte discrimina de verdade: a mesma funcao instanciada com float
	// (que nao tem opcode aritmetico especializado) nao o contem.
	floatInstance := compiledFunction(t,
		"func somaf<T>(a: T, b: T) -> T\n    let rotulo: string = \"soma\"\n    print(rotulo)\n    return a + b + 7.0\nend\nlet total: float = somaf(2.0, 3.0)",
		"main::somaf<float>")
	if containsOpcode(floatInstance.Chunk.(*chunk.Chunk).Code, chunk.OP_ADD_INT) {
		t.Fatal("a instancia float contem o byte de OP_ADD_INT — a sonda nao discrimina neste corpo")
	}

	if len(genericChunk.Code) != len(handChunk.Code) {
		t.Fatalf("tamanhos de Code diferem: generico %d, a mao %d", len(genericChunk.Code), len(handChunk.Code))
	}
	for index := range genericChunk.Code {
		if genericChunk.Code[index] != handChunk.Code[index] {
			t.Fatalf("Code diverge no offset %d: %d vs %d", index, genericChunk.Code[index], handChunk.Code[index])
		}
	}

	if len(genericChunk.Constants) != len(handChunk.Constants) {
		t.Fatalf("tabelas de constantes diferem em tamanho: generico %d, a mao %d",
			len(genericChunk.Constants), len(handChunk.Constants))
	}
	for index := range genericChunk.Constants {
		got, want := genericChunk.Constants[index], handChunk.Constants[index]
		if got.Type != want.Type || got.String() != want.String() {
			t.Fatalf("constante %d diverge: generico %v (%s), a mao %v (%s)",
				index, got.Type, got.String(), want.Type, want.String())
		}
	}
	if len(genericChunk.Constants) == 0 {
		t.Fatal("corpo nao povoou a tabela de constantes — a comparacao seria vazia")
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

// §5: programa sem genericos nao paga pass 1 — o AST sai intocado e nenhuma
// fila de instancias sobra no compilador.
func TestNonGenericProgramSkipsPassOne(t *testing.T) {
	c := New()
	program := parse("func f(x: int) -> int\n    return x\nend\nf(1)")
	before := len(program.Statements)
	if _, _, err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	if c.hasGenerics() {
		t.Fatal("programa sem genericos populou o registry (gate do pass 1)")
	}
	if len(program.Statements) != before {
		t.Fatalf("statements = %d, quer %d (pass 1 prependou algo)", len(program.Statements), before)
	}
	if c.instances != nil {
		t.Fatal("fila de instancias sobrou no compilador")
	}
}

// instanceDeclNames lista, na ordem, as declaracoes sinteticas que o pass 1
// prependou a um Program (instancias sao as unicas funcoes com "::" no nome —
// identificador de usuario nunca contem o qualificador de modulo, §4).
func instanceDeclNames(program *ast.Program) []string {
	names := []string{}
	for _, statement := range program.Statements {
		if fn, ok := statement.(*ast.FunctionStatement); ok && strings.Contains(fn.Name, "::") {
			names = append(names, fn.Name)
		}
	}
	return names
}

func countInstanceFunctions(code *chunk.Chunk, name string) int {
	count := 0
	for _, constant := range code.Constants {
		if constant.Type == value.VAL_FUNCTION && constant.Obj.(*value.ObjFunction).Name == name {
			count++
		}
	}
	return count
}

// A fila (memo + ordered) vive por COMPILACAO DE PROGRAMA, nao por compilador.
// O registry persiste entre compilacoes (§5: REPL guarda templates na sessao),
// entao um segundo Compile no mesmo compilador precisa: (a) NAO herdar as
// declaracoes sinteticas do programa anterior, e (b) re-instanciar do zero uma
// tupla que o memo do programa anterior ja tinha visto.
func TestInstanceQueueIsScopedPerProgramCompile(t *testing.T) {
	c := New()
	first := parse("func id<T>(x: T) -> T\n    return x\nend\nid(1)\nid(\"a\")")
	firstCode, _, err := c.Compile(first)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(instanceDeclNames(first), " "); got != "main::id<int> main::id<string>" {
		t.Fatalf("programa 1 prependou %q", got)
	}
	// O chunk do compilador e cumulativo entre Compile: medimos o DELTA que o
	// programa 2 emite, nao o total.
	stringsBefore := countInstanceFunctions(firstCode, "main::id<string>")

	second := parse("id(2)")
	secondCode, _, err := c.Compile(second)
	if err != nil {
		t.Fatal(err)
	}
	// (a) nada do programa 1 vaza para o programa 2 — nem no AST, nem no
	// bytecode emitido pela segunda compilacao.
	if got := strings.Join(instanceDeclNames(second), " "); got != "main::id<int>" {
		t.Fatalf("programa 2 prependou %q, quer apenas main::id<int>", got)
	}
	if delta := countInstanceFunctions(secondCode, "main::id<string>") - stringsBefore; delta != 0 {
		t.Fatalf("programa 2 emitiu %d closures de main::id<string> (vazamento do programa 1)", delta)
	}
	// (b) a tupla usada nos dois programas e re-instanciada no segundo, apesar
	// de o memo do primeiro ja te-la visto.
	if c.instances != nil {
		t.Fatal("fila de instancias sobrou no compilador depois do merge")
	}
}

// Finding 2: templates genericos nunca entram em c.globals/c.structs pelo
// predeclare — o tipo deles carrega TypeParamType e applyProgramBindings o
// injetaria em c.globals durante TODO corpo de funcao. A identidade deles vive
// so no GenericRegistry.
func TestPredeclareSkipsGenericTemplates(t *testing.T) {
	c := New()
	program := parse("func id<T>(x: T) -> T\n    return x\nend\nstruct Caixa<T>\n    item: T\nend\nid(1)")
	if _, _, err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	if _, leaked := c.programBindings["id"]; leaked {
		t.Fatal("template de funcao 'id' vazou para programBindings (tipo cru com TypeParamType)")
	}
	if _, leaked := c.programBindings["Caixa"]; leaked {
		t.Fatal("template de struct 'Caixa' vazou para programBindings")
	}
	if _, leaked := c.structs["Caixa"]; leaked {
		t.Fatal("template de struct 'Caixa' vazou para c.structs")
	}
	// A instancia, essa sim, e uma declaracao comum e predeclara normalmente.
	if _, ok := c.programBindings["main::id<int>"]; !ok {
		t.Fatal("instancia main::id<int> nao chegou a programBindings")
	}
}
