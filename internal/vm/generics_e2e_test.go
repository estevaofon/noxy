package vm

// E2E de genericos por monomorfizacao (spec §4/§5): o programa inteiro passa
// pelo two-pass do compilador e roda na VM sem qualquer mudanca de runtime.

import "testing"

func TestGenericFunctionEndToEnd(t *testing.T) {
	got := captureVMSource(t, `
func first<T>(arr: T[]) -> T
    return arr[0]
end
let nums: int[] = [7, 8]
test_report(first(nums))
`)
	expectInt(t, got, 7, "first<int> deve devolver 7")
}

func TestGenericTwoInstantiations(t *testing.T) {
	got := captureVMSource(t, `
func size<T>(arr: T[]) -> int
    return length(arr)
end
let nums: int[] = [1, 2, 3]
let names: string[] = ["a"]
test_report(size(nums) + size(names))
`)
	expectInt(t, got, 4, "size<int> + size<string>")
}

func TestGenericRecursion(t *testing.T) {
	got := captureVMSource(t, `
func soma_rec<T>(arr: T[], i: int) -> int
    if i >= length(arr) then
        return 0
    end
    return 1 + soma_rec(arr, i + 1)
end
let xs: string[] = ["a", "b", "c"]
test_report(soma_rec(xs, 0))
`)
	expectInt(t, got, 3, "recursao generica")
}

// Issue #44 (1): target-typing em posicao de return — a anotacao de retorno
// da funcao envolvente ancora o T que so aparece no retorno do template, e o
// valor flui correto em runtime.
func TestGenericReturnPositionEndToEnd(t *testing.T) {
	got := captureVMSource(t, `
func vazia<T>() -> T[]
    let xs: T[] = []
    return xs
end
func prepara() -> int[]
    return vazia()
end
let r: int[] = prepara()
append(ref r, 41)
test_report(r[0] + length(r))
`)
	expectInt(t, got, 42, "vazia<int> via anotacao de retorno")
}

// Chamada generica de dentro de um corpo NAO-generico: o predeclare nao
// registra mais o template em globals, entao a interceptacao (via registry) e
// a unica coisa que faz este programa compilar.
func TestGenericCalledFromNonGenericBody(t *testing.T) {
	got := captureVMSource(t, `
func first<T>(arr: T[]) -> T
    return arr[0]
end
func run() -> int
    let xs: int[] = [42]
    return first(xs)
end
test_report(run())
`)
	expectInt(t, got, 42, "chamada generica dentro de corpo nao-generico")
}

// §4 terceira familia de hooks: struct generico instanciado por construtor
// posicional; a instancia e um struct comum no pass 2 (member access, mutacao
// de campo, validacao CoW por identidade nominal do nome qualificado).
func TestGenericStructConstructorAndAccess(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let c: Caixa<int> = Caixa(41)
c.valor = c.valor + 1
test_report(c.valor)
`)
	expectInt(t, got, 42, "Caixa<int> construida e mutada")
}

func TestGenericStructAnnotationOnlyPositions(t *testing.T) {
	// §11: instanciacao por anotacao pura — sem call site de construtor
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let vazias: Caixa<int>[] = []
let semInit: Caixa<int>
func conta(cs: Caixa<int>[]) -> int
    return length(cs)
end
test_report(conta(vazias))
`)
	expectInt(t, got, 0, "anotacoes puras instanciam o struct")
}

func TestGenericStructSelfReference(t *testing.T) {
	got := captureVMSource(t, `
struct Node<T>
    value: T,
    next: ref Node<T>
end
let n2: Node<int> = Node(2, null)
let n1: Node<int> = Node(1, ref n2)
test_report(n1.next.value)
`)
	expectInt(t, got, 2, "lista ligada generica")
}

func TestNestedGenericStruct(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let dupla: Caixa<Caixa<int>> = Caixa(Caixa(9))
test_report(dupla.valor.valor)
`)
	expectInt(t, got, 9, "Caixa<Caixa<int>>")
}

// §10: a instancia e um tipo nominal comum, entao a semantica de valor (CoW) e
// a validacao de tipo em runtime valem para ela sem nenhuma mudanca de VM — a
// copia nao alcanca o original.
func TestGenericStructValueSemantics(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let a: Caixa<int> = Caixa(1)
let b: Caixa<int> = a
b.valor = 99
test_report(a.valor)
`)
	expectInt(t, got, 1, "mutacao da copia nao alcanca o original")
}

// §11: Stack<T> manipulada por funcoes genericas. Duas coisas de uma vez: o
// construtor sem ancora nos argumentos (`Pilha([])`, resolvido pelo hint da
// anotacao do `let`) e a unificacao de `Pilha<T>` — a anotacao do template —
// contra o tipo concreto `main::Pilha<int>` do argumento.
func TestGenericStructWithGenericFunctions(t *testing.T) {
	got := captureVMSource(t, `
struct Pilha<T>
    itens: T[]
end
func empilha<T>(p: ref Pilha<T>, v: T)
    append(ref p.itens, v)
end
func topo<T>(p: Pilha<T>) -> T
    return p.itens[length(p.itens) - 1]
end
let p: Pilha<int> = Pilha([])
empilha(ref p, 7)
empilha(ref p, 42)
test_report(topo(p))
`)
	expectInt(t, got, 42, "Pilha<int> empilhada e lida por funcoes genericas")
}

// Instancia pedida SO dentro de um corpo de funcao: a declaracao sintetica e
// prependada no topo do programa, entao o construtor ja e um global definido
// quando a funcao roda.
func TestGenericStructInstanceFromFunctionBody(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
func cria() -> int
    let local: Caixa<string> = Caixa("abc")
    return length(local.valor)
end
test_report(cria())
`)
	expectInt(t, got, 3, "instancia criada dentro de corpo nao-generico")
}

// Instancia como elemento de array e valor de map: as anotacoes compostas
// preservam a estrutura externa e a runtime type info do container e completa.
func TestGenericStructInsideContainers(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
let cs: Caixa<int>[] = [Caixa(1), Caixa(2)]
let m: map[string, Caixa<int>] = {"a": Caixa(39)}
test_report(cs[1].valor + m["a"].valor)
`)
	expectInt(t, got, 41, "instancia dentro de array e de map")
}

// R5: a variavel de for-each ganha o tipo do elemento da colecao quando ele e
// estaticamente conhecido — sem isso a variavel entra com tipo nil e nenhuma
// chamada generica ancorada nela consegue inferir T.
func TestGenericCallAnchoredOnLoopVariable(t *testing.T) {
	got := captureVMSource(t, `
func identity<T>(x: T) -> T
    return x
end
let nums: int[] = [10, 20, 12]
let total: int = 0
for v in nums do
    total = total + identity(v)
end
test_report(total)
`)
	expectInt(t, got, 42, "T=int inferido da variavel do for-each")
}

func TestGenericCallsGeneric(t *testing.T) {
	got := captureVMSource(t, `
func first<T>(arr: T[]) -> T
    return arr[0]
end
func head_twice<T>(a: T[], b: T[]) -> T
    let x: T = first(a)
    return first(b)
end
let xs: int[] = [1]
let ys: int[] = [2]
test_report(head_twice(xs, ys))
`)
	expectInt(t, got, 2, "cascata generico->generico")
}

// Regressao (C2 da revisao final de branch): um nome de template CAPTURADO
// por uma closure (upvalue) tem de perder para o binding capturado, nao para
// o template. Os guards de call site em compileCallExpression checavam so
// resolveLocal — o upvalue escapava, a chamada era interceptada como
// generica e o programa devolvia 21 (id<int>(21)) em vez de 42 (dobro(21)),
// silenciosamente. isShadowedByLocal (locais E upvalues) e a regra que todos
// os outros hooks do §3/§4 ja usavam.
func TestGenericTemplateShadowedByUpvalue(t *testing.T) {
	got := captureVMSource(t, `
func dobro(x: int) -> int
    return x * 2
end
func id<T>(x: T) -> T
    return x
end
func run() -> int
    let id: func(int) -> int = dobro
    let f: func(int) -> int = func(v: int) -> int
        return id(v)
    end
    return f(21)
end
test_report(run())
`)
	expectInt(t, got, 42, "upvalue sombreia o template generico homonimo")
}

// Par do teste acima para CONSTRUTOR de struct generico: o mesmo guard
// (segunda metade do C2) protege `Caixa(...)` quando `Caixa` foi capturado
// como upvalue por uma closure.
func TestGenericStructTemplateShadowedByUpvalue(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
func incrementa(x: int) -> int
    return x + 1
end
func run() -> int
    let Caixa: func(int) -> int = incrementa
    let f: func(int) -> int = func(v: int) -> int
        return Caixa(v)
    end
    return f(41)
end
test_report(run())
`)
	expectInt(t, got, 42, "upvalue sombreia o template de struct homonimo")
}

// §3 target-typing: `let` anotado com um tipo de funcao concreto e o alvo que
// instancia identity<int> — a genérica vira closure comum sem call site
// nenhum.
func TestTargetTypingLetAnnotation(t *testing.T) {
	got := captureVMSource(t, `
func identity<T>(x: T) -> T
    return x
end
let f: func(int) -> int = identity
test_report(f(5))
`)
	expectInt(t, got, 5, "identity instanciada por anotacao de let")
}

// §3 unificação bidirecional: aplica(nums, identity) — A=int ancora pelo
// argumento nao-generico primeiro; o parametro esperado func(A)->B (ja com
// A=int) unifica contra func(T)->T do template do argumento, propagando
// T=int => B=int. As duas instancias (aplica<int,int> e identity<int>) nascem
// do mesmo call site.
func TestTargetTypingBidirectionalArgument(t *testing.T) {
	got := captureVMSource(t, `
func identity<T>(x: T) -> T
    return x
end
func aplica<A, B>(arr: A[], fn: func(A) -> B) -> B[]
    let out: B[] = []
    for item in arr do
        append(ref out, fn(item))
    end
    return out
end
let nums: int[] = [1, 2, 3]
let mesmos: int[] = aplica(nums, identity)
test_report(mesmos[2])
`)
	expectInt(t, got, 3, "unificacao bidirecional")
}

// I5 da revisao final de branch, E2E: uma generica cujo PARAMETRO e um struct
// generico (`pega<T>(c: Caixa<T>)`) passada como valor-argumento. O template
// escreve `Caixa<T>` e o tipo observado no site e `main::Caixa<int>` — sem a
// ponte de expandInstanceNames na unificacao bidirecional, o programa nao
// compilava ("esperava main::Caixa<int>, encontrado Caixa<T>").
func TestBidirectionalArgumentWithGenericStructParam(t *testing.T) {
	got := captureVMSource(t, `
struct Caixa<T>
    valor: T
end
func pega<T>(c: Caixa<T>) -> T
    return c.valor
end
func aplica<A, B>(x: A, fn: func(A) -> B) -> B
    return fn(x)
end
let c: Caixa<int> = Caixa(42)
test_report(aplica(c, pega))
`)
	expectInt(t, got, 42, "struct generico como parametro do argumento-template")
}

// §3: elemento de array literal (tipo do elemento vem da anotacao do `let`
// envolvente) e posicao de retorno (tipo de retorno declarado da funcao
// corrente). A sintaxe de array-de-funcao exige parenteses
// (`(func(int) -> int)[]`) porque `func(int) -> int[]` parseia como "retorna
// int[]" — precedencia de `[]` documentada em parseType/parseAtomicType.
func TestTargetTypingArrayElementAndReturn(t *testing.T) {
	got := captureVMSource(t, `
func dobro(x: int) -> int
    return x * 2
end
func identity<T>(x: T) -> T
    return x
end
func escolhe() -> func(int) -> int
    return identity
end
let fs: (func(int) -> int)[] = [dobro, identity]
let g: func(int) -> int = escolhe()
test_report(fs[1](10) + g(1))
`)
	expectInt(t, got, 11, "array de funcoes e return position")
}
