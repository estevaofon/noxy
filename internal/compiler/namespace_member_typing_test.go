package compiler

import (
	"strings"
	"testing"

	"noxy-vm/internal/ast"
	"noxy-vm/internal/lexer"
	"noxy-vm/internal/parser"
)

// Issue #126 item 2: `m.f(...)` e `m.x` pelo namespace carregam o tipo
// declarado pelo modulo (o mesmo que `select` registra), traduzido para a
// visao do programa pela regra da #58 item 1 (programViewType). Com isso a
// chamada por namespace ganha aridade, tipo de argumento e tipo de retorno
// — antes, tipo nil: nada conferido, `let v = m.f()` nao inferia.

const rollModule = `struct V
    x: float
    y: float
end
let total: int = 0
let limit = 10
func roll(n: int) -> int
    return n * 2
end
func norm(v: V) -> V
    return V(v.x, v.y)
end
func bump() -> void
    total = total + 1
end
`

func rollRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	return root
}

func TestNamespaceCallInfersReturnType(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v = m.roll(6)\nlet s: string = v\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceCallReturnTypeIsCheckedInAnnotatedLet(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet s: string = m.roll(6)\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceCallChecksArgumentTypes(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v: int = m.roll(\"x\")\n")
	requireErrorMentions(t, err, "argument 1 to 'm.roll': expected int, got string")
}

func TestNamespaceCallChecksArity(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v: int = m.roll(1, 2)\n")
	requireErrorMentions(t, err, "function 'm.roll' expects 1 arguments, got 2")
}

func TestNamespaceCallReturningModuleStructIsNamedByAlias(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m as vec\nlet p = vec.norm(vec.V(1.0, 2.0))\nlet s: string = p\n")
	requireErrorMentions(t, err, "expected string, got vec.V")
}

func TestNamespaceConstructorChecksFieldTypes(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet p: m.V = m.V(1, 2.0)\n")
	requireErrorMentions(t, err, "argument 1 to 'm.V': expected float, got int")
}

func TestNamespaceStructReturnPrefersSelectedName(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nuse m select V\nlet p = m.norm(V(1.0, 2.0))\nlet s: string = p\n")
	requireErrorMentions(t, err, "expected string, got V")
}

func TestNamespaceStructReturnUsesFirstDeclaredAliasButMatchesBoth(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m as a\nuse m as b\nlet p = b.norm(b.V(1.0, 2.0))\nlet q: b.V = p\nlet r: a.V = p\nlet s: string = p\n")
	requireErrorMentions(t, err, "expected string, got a.V")
}

func TestNamespaceModuleVariableIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet n = m.total\nlet k = m.limit\nlet s: string = n\n")
	requireErrorMentions(t, err, "expected string, got int")
	err = compileSourceAtRoot(t, rollRoot(t), "use m\nlet s: string = m.limit\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceVoidCallCannotBeBound(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet v = m.bump()\n")
	requireErrorMentions(t, err, "cannot infer type for 'v'")
}

func TestNamespaceFunctionAsValueIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nlet g = m.roll\nlet s: string = g(2)\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceMemberIsTypedWhenStructIsUnnameable(t *testing.T) {
	// n.nx usa m pela forma de NAMESPACE: V nao entra nos exports de n, e o
	// programa que so faz `use n` nao tem grafia para o retorno de n.make().
	// Issue #133: o valor e tipado mesmo assim; a mensagem exibe `m.V`
	// (caminho canonico: modulo declarante + nome).
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "n.nx", "use m\nfunc make() -> m.V\n    return m.norm(m.V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, "use n\nlet s: string = n.make()\n")
	requireErrorMentions(t, err, "expected string, got m.V")
	requireNoError(t, compileSourceAtRoot(t, root, "use n\nlet p = n.make()\nlet x: float = p.x\n"))
	err = compileSourceAtRoot(t, root, "use n\nlet p = n.make()\nlet x: string = p.x\n")
	requireErrorMentions(t, err, "expected string, got float")
}

func TestNamespaceMemberIsNameableWhenModuleReexportsTheStruct(t *testing.T) {
	// Contraste com o teste acima: `use m select V, norm` REEXPORTA V por w
	// (discoverModuleStructs segue os selectors), entao `w.V` e um nome que
	// o programa consegue escrever e o retorno de w.make() e tipado.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "w.nx", "use m select V, norm\nfunc make() -> V\n    return norm(V(0.0, 0.0))\nend\n")
	requireNoError(t, compileSourceAtRoot(t, root, "use w\nlet p: w.V = w.make()\n"))
	err := compileSourceAtRoot(t, root, "use w\nlet s: string = w.make()\n")
	requireErrorMentions(t, err, "expected string, got w.V")
}

func TestLocalShadowingNamespaceAliasIsNotTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    roll: int
end
func f(m: Box) -> int
    let s: string = m.roll
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got int")
	// e o alias sombreado por um parametro sem campo `roll` continua
	// dinamico (tipo do struct local, nao do modulo):
	err = compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    v: int
end
func f(m: Box) -> int
    let s: string = m.v
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got int")
}

func TestUpvalueShadowingNamespaceAliasIsNotTyped(t *testing.T) {
	// O alias capturado como UPVALUE por um closure vence o namespace: `m.roll`
	// la dentro e o campo do Box capturado, int, nao a funcao do modulo. A base
	// tem tipo estatico, entao quem decide e memberType — namespaceMemberType
	// nem chega a ser consultado (so roda com base de tipo nil).
	err := compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    roll: int
end
func f(b: Box) -> func
    let m: Box = b
    return func() -> string
        let s: string = m.roll
        return s
    end
end
`)
	requireErrorMentions(t, err, "expected string, got int")
}

func TestNamespaceGenericTemplateStillRejected(t *testing.T) {
	root := t.TempDir()
	writeModuleFile(t, root, "g.nx", "func id<T>(x: T) -> T\n    return x\nend\n")
	err := compileSourceAtRoot(t, root, "use g\nlet v = g.id(1)\n")
	requireErrorMentions(t, err, "não é acessível via namespace")
}

func TestNamespaceCallInsideFunctionBodyIsTyped(t *testing.T) {
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nfunc f() -> string\n    let v = m.roll(1)\n    return v\nend\n")
	requireErrorMentions(t, err, "expected string, got int")
}

func TestReplNamespaceCallIsTypedOnLaterLine(t *testing.T) {
	// REPL: cada linha e um compilador novo; `use m` numa linha e
	// `m.roll(1)` na seguinte tem de ver o tipo — namespaceImports e
	// namespaceOrder viajam em ModuleState.
	root := rollRoot(t)
	globals := make(map[string]ast.NoxyType)
	structs := make(map[string]*ast.StructStatement)
	var modules *ModuleState
	for _, line := range []string{
		"use m\n",
		"let v = m.roll(1)\n",
		"let s: string = v\n",
		"let p = m.norm(m.V(1.0, 2.0))\n",
		"let bad: string = p\n",
	} {
		program := parser.New(lexer.New(line)).ParseProgram()
		c := NewWithStateAndRoot(globals, structs, "REPL", root)
		c.SetModuleState(modules)
		_, _, err := c.Compile(program)
		modules = c.ModuleState()
		switch {
		case strings.HasPrefix(line, "let s"):
			requireErrorMentions(t, err, "expected string, got int")
		case strings.HasPrefix(line, "let bad"):
			requireErrorMentions(t, err, "expected string, got m.V")
		default:
			requireNoError(t, err)
		}
	}
}

func TestNamespaceCallInsideGenericFunctionBodyIsTyped(t *testing.T) {
	// Corpo de template generico: o tipo do namespace tem de sobreviver a
	// instanciacao (`wrap<int>`), senao `return m.roll(1)` num `-> string`
	// passaria batido. A mensagem vem da checagem do corpo instanciado.
	err := compileSourceAtRoot(t, rollRoot(t), "use m\nfunc wrap<T>(x: T) -> string\n    return m.roll(1)\nend\nlet s: string = wrap(1)\n")
	requireErrorMentions(t, err, "return type mismatch in 'main::wrap<int>': expected string, got int")
}

func TestNamespaceNullableMemberNarrows(t *testing.T) {
	// Interacao com a spec de nullable (§2): um `let` de topo `V?` lido pelo
	// namespace entra no fluxo de narrowing como qualquer outro valor —
	// dentro do `if m.opt != null` o tipo e `o.V` (nao-nulo), e fora dele o
	// erro traz o hint de `may be null`.
	root := t.TempDir()
	writeModuleFile(t, root, "o.nx", "struct V\n    x: int\nend\nlet opt: V? = null\nfunc get() -> V?\n    return null\nend\n")

	// Os requireErrorLacks sao essenciais aqui: `got o.V` e prefixo de
	// `got o.V?`, entao sem eles uma regressao no narrowing passaria batida.
	err := compileSourceAtRoot(t, root, "use o\nif o.opt != null then\n    let s: string = o.opt\nend\n")
	requireErrorMentions(t, err, "expected string, got o.V")
	requireErrorLacks(t, err, "o.V?", "may be null")

	err = compileSourceAtRoot(t, root, "use o\nlet s: string = o.opt\n")
	requireErrorMentions(t, err, "expected string, got o.V?", "may be null")

	// e o retorno `V?` de uma chamada por namespace narrowa igual:
	err = compileSourceAtRoot(t, root, "use o\nlet v = o.get()\nif v != null then\n    let s: string = v\nend\n")
	requireErrorMentions(t, err, "expected string, got o.V")
	requireErrorLacks(t, err, "o.V?", "may be null")
}

func TestNamespaceMemberOfUnloadableModuleStaysDynamic(t *testing.T) {
	// `use nope` de um modulo que nao existe ao lado nao e erro de
	// compilacao (a resolucao real fica para o runtime; cf.
	// TestFunctionBodyOnlyWildcardDoesNotAffectModuleLoadability, que exige
	// que uma dependencia ausente nao invalide o modulo). A tipagem por
	// namespace nao pode introduzir erro NOVO ai: sem exports descobertos,
	// importedBindingType falha e o membro fica dinamico (tipo nil).
	root := rollRoot(t)
	requireNoError(t, compileSourceAtRoot(t, root, "use nope\nfunc f() -> int\n    let v: int = nope.f(1)\n    return v\nend\n"))
	requireNoError(t, compileSourceAtRoot(t, root, "use nope\nlet v: int = nope.f(1)\n"))
}

func TestNamespaceMemberReexportedByWildcardIsTyped(t *testing.T) {
	// Issue #133: `g` chega a m so por REEXPORTACAO (`use x select *`);
	// namespaceMemberType segue reexportSource ate o modulo declarante, como
	// `use m select g` ja fazia.
	root := t.TempDir()
	writeModuleFile(t, root, "x.nx", "func g(n: int) -> int\n    return n\nend\n")
	writeModuleFile(t, root, "m.nx", "use x select *\n")
	err := compileSourceAtRoot(t, root, "use m\nlet s: string = m.g(1)\n")
	requireErrorMentions(t, err, "expected string, got int")
	err = compileSourceAtRoot(t, root, "use m\nlet v: int = m.g(\"x\")\n")
	requireErrorMentions(t, err, "argument 1 to 'm.g': expected int, got string")
	err = compileSourceAtRoot(t, root, "use m\nlet v: int = m.g(1, 2)\n")
	requireErrorMentions(t, err, "function 'm.g' expects 1 arguments, got 2")
}

func TestReexportedStructReturnIsTypedThroughNamespaceAndSelect(t *testing.T) {
	// Caso (a) da issue: mid reexporta base por `select *`; o valor e tipado
	// nos dois caminhos (namespace e select) mesmo sem `struct V` local.
	//
	// A grafia exibida difere dos dois lados por uma razao real, nao um
	// capricho de implementacao: `use mid` (namespace) da ao programa uma
	// grafia PROPRIA para V — `mid.V` — porque discoverModuleStructs("mid")
	// segue o `select *` de mid.nx e enxerga V; programStructName prefere
	// esse alias visivel sobre o caminho canonico (regra 2, member_types.go).
	// `use mid select mkv` (so a FUNCAO, sem struct nenhum) nao registra
	// nenhum alias de namespace para V, entao cai no caminho canonico
	// `base.V`.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc mkv() -> V\n    return V(1)\nend\nfunc usa(v: V) -> int\n    return v.x\nend\n")
	writeModuleFile(t, root, "mid.nx", "use base select *\n")
	requireNoError(t, compileSourceAtRoot(t, root, "use mid\nlet v = mid.mkv()\nlet n: int = v.x\nlet k: int = mid.usa(v)\n"))
	err := compileSourceAtRoot(t, root, "use mid\nlet v = mid.mkv()\nlet s: string = v\n")
	requireErrorMentions(t, err, "expected string, got mid.V")
	err = compileSourceAtRoot(t, root, "use mid select mkv\nlet v = mkv()\nlet s: string = v\n")
	requireErrorMentions(t, err, "expected string, got base.V")
	requireNoError(t, compileSourceAtRoot(t, root, "use mid select mkv, usa\nlet v = mkv()\nlet k: int = usa(v)\n"))
}

func TestSelectedFunctionSignatureDoesNotCaptureLocalHomonym(t *testing.T) {
	// O buraco documentado em vm/module_exports_test.go (~641): a assinatura
	// importada por select carregava o nome CRU `V`, que um `struct V` local
	// capturava. Com Decl, `use m select norm` + `struct V` local recusa
	// passar o V local para norm.
	root := rollRoot(t)
	err := compileSourceAtRoot(t, root, "use m select norm\nstruct V\n    x: float\n    y: float\nend\nlet p: V = norm(V(1.0, 2.0))\n")
	requireErrorMentions(t, err, "argument 1 to 'norm': expected m.V, got V")
}

func TestSelectDisplayNameIsOrderIndependentAtTopLevel(t *testing.T) {
	// Caracterizacao adversarial (spec §1.5, achado 4 previa sensibilidade a
	// ordem textual; investigado e refutado para o programa de topo): um
	// `use` do TOPO passa por importBindingFrom DUAS vezes — no predeclare
	// (predeclareGlobalBindings) e de novo no compile-pass (o case
	// *ast.UseStmt que emite bytecode, compiler.go). O predeclare roda
	// INTEIRO — para TODOS os `use` do topo — antes do compile-pass tocar o
	// primeiro. Como e o compile-pass que fixa o valor final em c.globals, a
	// segunda traducao de `mkv` sempre enxerga o namespaceOrder JA COMPLETO
	// do programa inteiro — a alias `b` aparece no nome exibido nos DOIS
	// sentidos, mesmo quando `use base as b` vem DEPOIS de `use mid select
	// mkv` no texto fonte.
	root := t.TempDir()
	writeModuleFile(t, root, "base.nx", "struct V\n    x: int\nend\nfunc mkv() -> V\n    return V(1)\nend\n")
	writeModuleFile(t, root, "mid.nx", "use base select *\n")
	err := compileSourceAtRoot(t, root, "use mid select mkv\nuse base as b\nlet s: string = mkv()\n")
	requireErrorMentions(t, err, "expected string, got b.V")
	err = compileSourceAtRoot(t, root, "use base as b\nuse mid select mkv\nlet s: string = mkv()\n")
	requireErrorMentions(t, err, "expected string, got b.V")
}

func TestNamespaceStructNameSkipsAliasShadowedWhereTypeIsInferred(t *testing.T) {
	// Item (c) da issue: dentro de f, `m` e o parametro Box; a grafia do
	// tipo de w.make() e escolhida no ponto em que o tipo e PRODUZIDO, entao
	// pula o alias sombreado e exibe `w.V`.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "w.nx", "use m select V, norm\nfunc make() -> V\n    return norm(V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, `use m
use w
struct Box
    q: int
end
func f(m: Box) -> int
    let p = w.make()
    let s: string = p
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got w.V")
	requireErrorLacks(t, err, "got m.V")
}

func TestNamespaceStructNameIsFixedWhereInferredNotWherePrinted(t *testing.T) {
	// Caracterizacao (spec §1.5): o `let` de topo grava `m.V`; a mensagem
	// dentro de f, onde `m` esta sombreado, ainda imprime `m.V` — nao ha
	// re-traducao na impressao.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "w.nx", "use m select V, norm\nfunc make() -> V\n    return norm(V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, `use m
use w
struct Box
    q: int
end
let v = w.make()
func f(m: Box) -> int
    let s: string = v
    return 0
end
`)
	requireErrorMentions(t, err, "expected string, got m.V")
}

func TestQualifiedAnnotationStillResolvesWhenAliasIsShadowedByParameter(t *testing.T) {
	// Anotacao e espaco de tipos: `m.V` dentro de f(m: Box) continua o struct
	// do modulo (comportamento atual, preservado).
	requireNoError(t, compileSourceAtRoot(t, rollRoot(t), `use m
struct Box
    q: int
end
func mk() -> m.V
    return m.V(1.0, 2.0)
end
func f(m: Box) -> int
    let q: m.V = mk()
    return 0
end
`))
}
