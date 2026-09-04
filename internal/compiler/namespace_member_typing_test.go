package compiler

import "testing"

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

func TestNamespaceMemberStaysDynamicWhenStructIsUnnameable(t *testing.T) {
	// n.nx usa m pela forma de NAMESPACE: V nao entra nos exports de n, e um
	// programa que so faz `use n` nao tem nome para o retorno de n.make().
	// Tipo inteiro dinamico (nunca meio-tipado): o `let` anotado nao e
	// conferido (como antes da #126) e o inferido erra por falta de tipo.
	root := t.TempDir()
	writeModuleFile(t, root, "m.nx", rollModule)
	writeModuleFile(t, root, "n.nx", "use m\nfunc make() -> m.V\n    return m.norm(m.V(0.0, 0.0))\nend\n")
	err := compileSourceAtRoot(t, root, "use n\nlet s: string = n.make()\n")
	requireNoError(t, err)
	err = compileSourceAtRoot(t, root, "use n\nlet p = n.make()\n")
	requireErrorMentions(t, err, "cannot infer type for 'p'")
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
