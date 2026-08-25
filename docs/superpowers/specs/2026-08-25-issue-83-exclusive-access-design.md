# Acesso exclusivo a contêiner emprestado — o modelo do Swift (SE-0176) aplicado ao Noxy (issue #83)

**Data:** 2026-08-25 · **Branch:** `fix/issue-83-exclusive-access`, a partir de `develop` limpa (@ `20c207c`, v0.19.0)
**Status:** design proposto · **Issue:** [#83](https://github.com/estevaofon/noxy/issues/83)
**Releases:** v0.20.0 (P1+P2, estático, BREAKING) → v0.21.0 (P3, dinâmico)
**Relação:** completa a §2.3 escrita por `specs/2026-08-24-explicit-ref-design.md`, que registrou este caso como fora de escopo ("é bug de CoW na VM, não de sintaxe"). **Não é bug de VM nem de sintaxe:** é a ausência de *exclusividade de acesso*, o mecanismo que toda linguagem com value semantics + copy-on-write + referência mutável precisa ter. Não toca `specs/2026-08-20-ref-slot-invariant-design.md` nem `specs/2026-08-20-container-owners-design.md`.
**Prioridade sugerida:** depois de #47 e #75 — §9.1.

> **Esta spec muda o critério de aceitação da issue.** A issue propõe *pin* (clone
> eager) e espera que o programa do "Critério" rode e imprima `1`. Aqui ele vira erro
> de compilação. A comparação com a proposta original está na §8.

## 0. Decisão

> ## ⚠️ ESTA SPEC FOI SUPERADA PELA IMPLEMENTAÇÃO (2026-08-25)
>
> O desenho abaixo — SE-0176 em três peças (P1 convenção na assinatura, P2 ordem
> de avaliação, P3 exclusividade dinâmica) — **não foi o que resolveu o bug**. Ele
> foi abandonado depois que a validação adversarial da H4 (§7.5) encontrou o
> **repro G**, em que o alias é adquirido num *ancestral* do contêiner e nenhuma
> checagem centrada no contêiner o enxerga.
>
> **O que foi implementado:** o empréstimo passa a denotar um **LUGAR**, e o
> caminho raiz→contêiner é re-resolvido **na escrita**, unicizando e gravando o
> clone de volta em cada nível. Fecha os sete repros, incluindo G, sem nenhuma
> regra nova na linguagem e sem nenhum erro novo de compilação ou de runtime nos
> programas que já funcionavam.
>
> - Implementação: `internal/compiler/borrow_place.go`, `borrowContainer` em
>   `internal/vm/references.go`, `ObjRef.Base` em `internal/value/value.go`.
> - Testes: `internal/vm/borrow_place_test.go` (os sete repros, cada um com a
>   contraprova da §1.3).
> - **A §8 desta spec estava errada** ao descartar o empréstimo path-based como
>   "invenção sem precedente": é como qualquer linguagem modela um *lugar*, e a
>   caminhada com gravação de volta **já estava implementada** na família `_MUT`
>   (§1.4). A correção reusa essa caminhada mudando só o INSTANTE em que ela roda.
> - **P1 e P3 não existem mais.** R11, R12 e `own ref` foram implementados e
>   depois revertidos: passaram a avisar sobre código correto. P2 (§3) continua
>   sendo uma melhoria válida e independente, não implementada.
>
> O que segue é o desenho original, mantido porque a análise do problema (§1.1,
> §1.2, §1.3, §1.4) continua correta e é o que explica a correção.

O Noxy tem exatamente a combinação do Swift: tipos de valor com **copy-on-write**,
unicidade decidida por **contagem de referências** (`Owners`/`Retain`, série #66), e
referência mutável de primeira classe (`ref`). O Swift enfrentou este mesmo bug e o
resolveu em **SE-0176 — Enforce Exclusive Access to Memory**, em produção desde o
Swift 4 e ligado por padrão em builds otimizados desde o Swift 5.0.

Adotamos esse modelo. Três peças, nenhuma inventada aqui:

| | Peça | Precedente | Fecha | Onde |
|---|---|---|---|---|
| **P1** | Convenção na assinatura: `ref T` é empréstimo e não escapa; `own ref T` declara o parâmetro que guarda | Swift `inout` / `@escaping`; C# `scoped`; Hylo `let/inout/sink/set`; Nim `var` × `ref` | **A, B** | compilador |
| **P2** | Ordem de avaliação: o acesso ao empréstimo começa **depois** de todos os argumentos não-`ref` serem avaliados; nesse ponto o chamador copia storage não-única defensivamente | SE-0176; JOT §6.3 | **C** | compilador |
| **P3** | Exclusividade dinâmica: enquanto o acesso vive, acesso conflitante ao mesmo contêiner é erro de runtime | SE-0176 (enforcement dinâmico) | **D, E, F** | VM |

**Critério de pronto:** os **sete** repros da §1.1 deixam de vazar — A, B e C como erro
de compilação, D, E, F e G como erro de runtime — e o teste da §1.3 garante que a escrita
através do empréstimo **continua chegando** no original.

> **Nota de 2026-08-25:** G foi acrescentado pela validação adversarial da H4 (§7.5),
> depois de esta spec estar escrita, e **P3 como desenhado na §4.2 não o fecha**. P1
> (§2) está implementada em modo aviso e não é afetada. P3 precisa de redesenho antes
> de começar.

Rotas descartadas, ambas por serem invenção sem precedente: *pin* (nenhuma linguagem
estabelecida marca um contêiner para sempre porque alguém tomou uma referência) e
*empréstimo path-based* (referência guardando slot+índice em vez do objeto). §8.

## 1. O problema

### 1.1 Os seis repros

Todos verificados no binário v0.19.0 (`20c207c`), 2026-08-25. **São o critério de
aceitação**; a issue traz só o primeiro.

**A — o empréstimo é ligado a um nome** (o "Critério" da issue):

```noxy
let arr: int[] = [1, 2, 3]
let r: ref int = ref arr[0]
let copia: int[] = arr        // cópia DEPOIS do ref
*r = 999
print(copia)                  // [999, 2, 3] — deveria ser [1, 2, 3]
```

**B — o empréstimo escapa por dentro do chamado:**

```noxy
let g: ref int = null
func keep(r: ref int) -> void
    g = r
end
let arr: int[] = [1, 2, 3]
keep(ref arr[0])
let copia: int[] = arr
*g = 999                      // arr e copia ficam ambos [999, 2, 3]
```

**C — outro argumento da mesma chamada copia a raiz:**

```noxy
func f(r: ref int, xs: int[]) -> void
    *r = 999
    print(xs)                 // [999, 2, 3] — xs deveria ser cópia independente
end
f(ref arr[0], arr)
```

**D — o callee alcança a raiz por um global:**

```noxy
let arr: int[] = [1, 2, 3]
let copia: int[] = []
func f(r: ref int) -> void
    copia = arr               // Owners++ com o empréstimo vivo
    *r = 999
end
f(ref arr[0])
```

**E — outro argumento passa a raiz como referência de célula:**

```noxy
func f(r: ref int, xs: ref int[]) -> void
    let copia: int[] = *xs
    *r = 999
    print(copia)              // [999, 2, 3]
end
f(ref arr[0], ref arr)
```

**F — o alias viaja dentro de um valor; o call site não menciona a raiz:**

```noxy
struct Holder
    r: ref int[]
end
func f(r: ref int, h: Holder) -> void
    let copia: int[] = *h.r
    *r = 999
    print(copia)              // [999, 2, 3]
end
let h: Holder = Holder(ref arr)
f(ref arr[0], h)
```

**G — o alias é adquirido num ANCESTRAL do contêiner do empréstimo:**

```noxy
struct H
    xs: int[]
end
let h: H = H([1, 2, 3])
let copia: H = H([])
func f(r: ref int) -> void
    copia = h                 // 2º dono da INSTÂNCIA h; o array xs não é tocado
    *r = 999
end
f(ref h.xs[0])                // empréstimo em posição de argumento: R11/R12 legal
print(copia.xs)               // [999, 2, 3] — deveria ser [1, 2, 3]
```

Encontrado pela validação adversarial da H4 (§7.5) em 2026-08-25, **depois** de esta
spec estar escrita. Vale também com o ancestral sendo array externo (`ref a[0][0]`,
`copia = a`), map (`ref m["k"][0]`, `copia = m`), `REF_PROPERTY` (`ref o.inn.x`,
`copia = o`), e combinado com F — o alias dentro de um valor **e** um nível acima.
Testes em `internal/vm/borrow_ancestor_alias_test.go`.

A e B são **escape**. C, D, E, F e G são **acesso conflitante** — o empréstimo nunca
escapa; o contêiner ganha um segundo dono enquanto ele vive. F é o que decide o
desenho: o alias está dentro de um struct montado em outra linha, e nenhuma checagem
local no call site pode enxergá-lo. **Fechar C–F estaticamente exige enumerar todos os
canais pelos quais um alias pode chegar, e essa enumeração é aberta.** Foi por isso que
o Swift fiscaliza dinamicamente.

**G é o que decide o MECANISMO, e ele derruba a §4.2 como escrita.** A propriedade que
A–F compartilham sem que ninguém tivesse notado: em todos, o contêiner do empréstimo
**é** a raiz — `ref arr[0]` sobre um `int[]` plano é um objeto só, um `Owners` só. G tem
caminho raiz→contêiner de dois níveis, e o segundo dono entra no nível de cima.
`Retain` é **por objeto**: `copia = h` incrementa o `*ObjInstance`, e o `*ObjArray` de
`h.xs` — o contêiner que o empréstimo marcaria — fica em `owners=1` durante toda a
janela. Consequências:

1. **P3 como especificado nunca dispara em G.** Abrir o acesso no contêiner e detectar
   em `Retain` não vê um retain que acontece noutro objeto.
2. **Consultar `IsShared` no ramo `REF_INDEX` também não veria.** No instante da escrita
   o contêiner é, para o RC, perfeitamente único. O compartilhamento mora um nível acima.
3. **A exclusividade tem de cobrir o CAMINHO raiz→contêiner inteiro, não um objeto** — o
   que a proposta de codificar o estado na palavra de `Owners` de um objeto (§4.2) não
   comporta. A cadeia `_MUT` já visita todos os nós do caminho na criação do empréstimo
   (§1.4); é o lugar natural para abrir o acesso em cada nível.

**A contraprova da §1.3 já é falsa hoje em contêiner aninhado.** Com `copia = h` seguido
de `h.xs[1] = 7`, o MUT clona a instância, o clone retém `xs`, `xs` vira compartilhado,
`GET_PROP_MUT` clona `xs` — e o empréstimo escreve no array **velho**. Saem os dois modos
de falha juntos: `h.xs` fica `[1, 7, 3]` (**escrita perdida**) e `copia.xs` fica
`[999, 2, 3]` (**vazamento**), com um empréstimo 100% legal sob R11/R12. Antes de servir
de critério para qualquer correção, a contraprova precisa ser reescrita em contêiner
aninhado. `TestBorrowAncestorAliasLosesTheWrite`.

**`Retain` também não é funil suficiente.** Há violações de exclusividade que não
adquirem dono nenhum, logo sem nenhum `Retain` na janela:

- `delete(ref m, "a")` durante um empréstimo em `ref m["a"]` — a escrita **ressuscita** a
  chave apagada, porque o ramo `REF_INDEX` de `referenceStorage` usa `mapping.Set`, que
  insere se a chave não existe.
- `arr = [7, 7, 7]` durante um empréstimo em `ref arr[0]` — a escrita some em silêncio.
  No Swift isso é exatamente um conflito de acesso simultâneo, e o programa trapa.

E na direção oposta: o `Retain` interno do `copyValue` (o clone retém os filhos,
`internal/vm/calls.go:164`) dispararia o detector em **bookkeeping de CoW**, não em
compartilhamento do usuário. P3 precisa distinguir os dois.
`TestBorrowConflictWithoutAnyRetain`.

### 1.2 A causa

`ref arr[0]` produz `ObjRef{RefType: REF_INDEX}` cujo campo `Container Value`
(`internal/value/value.go:585`) congela o `*ObjArray` do instante da criação. A cópia
posterior só incrementa `Owners` — o CoW é preguiçoso. A escrita entra por
`storeReferenceValue` → `referenceStorage` → ramo `REF_INDEX`
(`internal/vm/references.go:113-120`) e grava direto, sem consultar `IsShared`.

`REF_PTR` / `REF_GLOBAL` / `REF_UPVALUE` endereçam um **slot**, e o CoW troca o
conteúdo de um slot, nunca o slot — são sólidos. `REF_INDEX` / `REF_PROPERTY`
endereçam um **offset dentro de um objeto que o CoW pode bifurcar**.

### 1.3 A armadilha: o conserto óbvio é pior que o bug

O reflexo é chamar `unicize` no contêiner no momento da escrita. **Não faça.** O
contêiner está compartilhado, a escrita iria para um clone anônimo — nenhum slot aponta
para ele — e `arr` **e** `copia` ficariam `[1, 2, 3]`. Troca-se um vazamento visível e
testável por uma escrita perdida em silêncio.

Um `REF_INDEX` sabe o objeto, não o nome. Quando o CoW bifurca, não há informação na
referência para decidir qual lado a herda. **Todo teste desta spec inclui a
contraprova:** a escrita através do empréstimo continua alcançando o original.

### 1.4 Metade da estratégia já está implementada

`compileReferenceArgumentValue` chama `compileLValueBase(target.Left)` nos casos
`MemberAccess` e `IndexExpression` (`compiler.go:2637,2650`), e `compileLValueBase`
emite a família **MUT** (`cow_lowering.go:29,58`), que unicíza a base ao longo do
caminho. Isso é literalmente a prescrição do JOT §6.3 — *"the caller is compelled to
copy non-unique storage defensively"* — escrita de forma independente, e é por isso
que uma cópia feita **antes** do `ref` já fica corretamente isolada (verificado).

O que falta é a precondição do JOT §5.3 — *"the language disallows the pointer to
escape in any way"* (P1) — e a exclusividade do SE-0176 (P2, P3).

## 2. P1 — Convenção na assinatura (estático)

Fecha **A** e **B**.

### 2.1 Regras (§2.3, novas R10–R12)

Texto proposto para `docs/NOXY_LANGUAGE_SPEC.md` §2.3, no estilo das R1–R9 (a spec da
linguagem é em inglês).

---

#### R10. Every reference denotes a cell

`ref` over a **name** — local, parameter, global, captured variable — promotes that slot
to a heap cell (R9) and produces a first-class `ref T`: it can be bound, stored,
returned, sent over a channel, and it outlives the function that created it. Unchanged,
and it is how linked structures are built.

Copy-on-write replaces the *contents* of a slot, never the slot itself, so a cell
survives every copy of every value around it.

#### R11. A reference into a container is a borrow

`ref` over a **field, an index, or a map entry** — `ref p.x`, `ref a[i]`, `ref m[k]` —
cannot denote a cell: the referent lives inside a composite that copy-on-write may
duplicate. Such a reference is a **borrow**, valid only for the duration of the call it
is written in. It has no type that can be written down, and appears **only as a call
argument**: never bound to a name, returned, stored, sent over a channel, or captured.

#### R12. `ref` parameters are borrows; `own ref` declares one that is kept

A `ref T` **parameter** is a borrow: the callee may read it, write through it and pass
it on, but **may not keep it** — no storing in a field, element, map entry or global, no
struct constructor, no `return`, no closure capture. Reported **inside the callee**, at
the offending line.

A parameter that outlives the call is declared `own ref T`. It accepts only a cell
reference (R10) — never a borrow — checked **at the call site, from the signature
alone**.

```noxy
func incrementa(n: ref int) -> void        // borrow: used, not kept
    *n = *n + 1
end

func liga(pai: ref No, filho: own ref No) -> void
    pai.prox = filho                        // ✓ declared as kept
end

liga(ref a, ref b)                          // ✓ 'b' is a variable: a cell
liga(ref a, ref lista[0])                   // ✗ a borrow cannot be kept
```

Every check is one level deep: signature against signature. The compiler never inspects
a callee's body to decide whether a call is legal. `own` is a parameter modifier only —
storing `ref novo` for a local `novo` in a field is R10 and needs no annotation.

---

### 2.2 Por que declarar, e não inferir

Uma alternativa considerada era inferir o escape com um pré-passe de ponto fixo sobre o
grafo de chamadas. **Descartada por desenho:**

```noxy
func salvar() -> void
    backup = dados            // compartilha o global 'dados'
end
func aux(r: ref int) -> void
    salvar()                  // não menciona 'dados'
end
topo(ref dados[0])            // recusado por causa de salvar(), duas chamadas abaixo
```

A legalidade de uma chamada passaria a depender de um corpo invisível ao autor, com
falso positivo garantido (análise insensível a fluxo). **Nenhuma das quatro referências
infere** — Swift, C# (`scoped`, introduzido exatamente para *"say this parameter does
not escape the call"*), Hylo e Nim declaram.

### 2.3 Compilador

**R11.** `OP_REF_PROPERTY` e `OP_REF_INDEX` só são emitidos em
`compileReferenceArgumentValue`, casos `*ast.MemberAccessExpression`
(`compiler.go:2646`) e `*ast.IndexExpression` (`compiler.go:2682`). Os dois caminhos já
são disjuntos: argumento de parâmetro `ref T` vai por `compileRefArgument` →
`compileReferenceArgument`; **todo o resto** passa pelo `case *ast.PrefixExpression`
(`compiler.go:1464`). A checagem entra nesse único ponto, com a posição sintática (let,
rebind, return, store, canal, captura) levada até lá para escolher a mensagem.

**R12, lado do callee.** Ao compilar o corpo, um parâmetro `ref T` sem `own` em posição
de guarda — RHS de atribuição a global/campo/índice/entrada de map, argumento de
construtor, `append`/`chan_send`, `return`, captura por literal de closure — é erro
**naquela linha**. Varredura de um corpo, sem propagação.

**R12, lado do chamador.** Em `compileCallExpression`, ramo de parâmetro `ref T`
(`compiler.go:2477`): parâmetro `own` recebendo `ref <container>` é erro. Uma consulta
ao `*ast.Parameter`.

**Parser/AST.** `own` é modificador de parâmetro. Não é tipo: `own ref T` e `ref T` são
o mesmo `*ast.RefType`, com um booleano `Owned` no `*ast.Parameter`. Não existe `own` em
`let`, campo de struct, retorno ou `ast.FunctionType` — logo função de ordem superior
nunca guarda, e isso é limitação declarada, não omissão.

## 3. P2 — Ordem de avaliação (estático)

Fecha **C**, sem nenhuma checagem nova.

### 3.1 A regra do SE-0176

> "A function has long-term write access to all of its in-out parameters, with the write
> access **starting after all non-in-out parameters have been evaluated** and lasting for
> the entire duration of the function call."

Combinada com a cópia defensiva do JOT §6.3 (§1.4), isso fecha C por ordenação. Em
`f(ref arr[0], arr)`:

| | Hoje (ordem de fonte) | Com a regra |
|---|---|---|
| 1º | empréstimo criado: `arr` único, `OP_REF_INDEX` fixa o objeto | `arr` avaliado como valor → `Owners` = 2 |
| 2º | `arr` avaliado → `Owners` = 2, com o empréstimo já apontando o objeto | empréstimo criado: `_MUT` vê `IsShared` → **clona no slot de `arr`** |
| resultado | os dois nomes veem a escrita | `arr` = objeto novo (recebe a escrita); o argumento por valor = objeto velho, intacto |

A unicização defensiva **já existe**; só precisa rodar depois. R13 (a checagem de
exclusividade no call site que uma versão anterior desta spec propunha) desaparece.

### 3.2 Regra (§2.3, nova R13)

---

#### R13. Borrow access begins after the other arguments

In a call, all arguments that are not borrows are evaluated first, in source order. Only
then are the borrows created, and the containers they are rooted in are made unique at
that point. The borrow's access lasts for the whole call.

---

### 3.3 O desafio de implementação

Hoje `compileCallExpression` compila argumentos em ordem de fonte
(`compiler.go:2482`), e a VM espera os argumentos empilhados nessa ordem. Avaliar
não-empréstimos primeiro exige dissociar **ordem de avaliação** de **ordem na pilha**.

Abordagem proposta: compilar os argumentos não-empréstimo primeiro, cada um guardado num
local temporário oculto; depois compilar os empréstimos; então empilhar tudo na ordem
declarada. Custo: alguns slots de pilha, **apenas em chamadas que misturam empréstimo com
argumento por valor** — forma rara (no corpus, zero ocorrências: os 14 empréstimos em
posição de argumento estão em chamadas onde os demais argumentos são escalares ou
outras referências).

Esta é a parte de maior risco técnico de P2 e deve ser prototipada antes de a spec ser
dada como aprovada. Alternativas a avaliar no protótipo: emitir uma permutação de pilha
no fim, ou restringir a reordenação aos casos em que algum argumento é empréstimo
(deixando o codegen de toda outra chamada byte a byte idêntico — é o guardrail da §7).

## 4. P3 — Exclusividade dinâmica (runtime)

Fecha **D**, **E** e **F**.

### 4.1 Por que dinâmico

O Swift também não fecha esses casos estaticamente. Do SE-0176: o compilador pega muitas
violações estaticamente, mas *"other cases can only be dealt with at runtime, including
exclusivity violations with escaping closures, class type properties, static properties,
and global variables"* — e desde o Swift 5.0 isso trapa em runtime por padrão, em builds
otimizados.

D (global), E (referência de célula em outro argumento) e F (alias dentro de um valor)
são exatamente essa família. F é a prova: o call site não menciona a raiz.

### 4.2 Mecanismo

> ⚠️ **Esta seção está DESATUALIZADA desde a falsificação da H4 (2026-08-25).** O repro
> G mostra que abrir o acesso no contêiner e detectar em `Retain` não fecha o caso em que
> o alias entra num ancestral, e que `Retain` não é funil suficiente. Ler os três pontos
> da §1.1 antes de implementar qualquer coisa daqui. O que segue é o desenho original,
> mantido para registro.

Enquanto um empréstimo enraizado em `X` está vivo, `X` está em **acesso exclusivo**.
Qualquer tentativa de dar a `X` um segundo dono durável durante essa janela é erro de
runtime — não importa o canal.

- **Abre** na criação do empréstimo (`OP_REF_INDEX` / `OP_REF_PROPERTY`), depois da
  unicização de P2.
- **Fecha** no retorno da chamada, junto do teardown do frame (`popSimpleFrame`,
  `internal/vm/unwind.go`, que já é o funil terminal por guarda de arquitetura).
- **Detecta** em `Retain`, que é o funil único de aquisição de dono durável — as 41
  chamadas a `value.Retain(` em `internal/vm` passam todas por lá, então a cobertura é
  completa por construção, e não por enumeração.

**Por que isso cabe em `Retain` e o pin não cabia.** O pin precisa **substituir** o valor
(entregar um clone ao chamador), e `func Retain(v Value) bool` não tem como. A
exclusividade só precisa **detectar e falhar** — o que cabe na assinatura atual.

**Onde guardar o estado.** `internal/value/cow.go` documenta que `Retain`/`Release`
precisam caber em ≤80 nós de custo de inline, e `internal/vm/inline_guard_test.go` trava
essa propriedade perguntando ao próprio compilador. Um campo novo no `ObjHeader` custa um
segundo acesso à memória dentro do `Retain`. Proposta a prototipar: **codificar o estado
de acesso na própria palavra de `Owners`**, como `ownersSaturation` já faz com a parte
alta do range — `Retain` já carrega aquele atômico e já compara contra a saturação, então
a detecção sai de uma comparação que em boa parte já existe.

**Mensagem**, no espírito do Swift e do Zen ("a failure that indicates a bug raises and
stops the program"):

```
Runtime error: [arquivo:linha] simultaneous accesses to 'arr': it is borrowed by the
call at line N and cannot be copied or shared until that call returns
```

### 4.3 Riscos de P3

- **Orçamento de inline.** É a propriedade que some sem aviso e que nenhum teste
  funcional pega. `inline_guard_test.go` precisa ganhar `Retain`/`Release` além de
  `push`, **antes** de a implementação começar.
- **Fechamento do acesso.** Empréstimo criado e chamada abortada por `raise`/`defer`/
  unwind precisa liberar o acesso; `popSimpleFrame` é o funil, mas o caminho de unwind
  tem de ser coberto por teste.
- **Concorrência.** O estado é por contêiner e vive em `Owners`, que já é
  `atomic.Int32`. Routines diferentes com empréstimos no mesmo contêiner: comportamento
  a definir no protótipo (provavelmente contador, não booleano).
- **Custo.** Medir com os benchmarks que tocam esse caminho — `bench_share_mutate`,
  `bench_path_update`, `bench_conway` — e com `CloneCountValue()` (`vm/cow.go:13`), que
  **não pode subir**: esta spec não introduz clone novo.

## 5. Diagnósticos

| Situação | Mensagem | Hint |
|---|---|---|
| R11 — ligado a nome / rebind | `a reference into a container cannot be bound to a name: it is only valid for the duration of a call` | `use 'a[i]' directly, or pass 'ref a[i]' as a call argument` |
| R11 — retornado | `a reference into a container cannot be returned` | `return the index and let the caller index` |
| R11 — guardado | `a reference into a container cannot be stored` | `store a reference to a variable ('ref x'), or store the index` |
| R11 — canal | `a reference into a container cannot be sent over a channel` | `send a copy, or send the index` |
| R11 — captura | `a reference into a container cannot be captured by a closure` | `capture the container, or a reference to a variable` |
| R12 — callee guarda um `ref` | `'<param>' is a borrow and cannot be stored: it is only valid during the call` | `declare 'own ref <T>' if the function needs to keep it` |
| R12 — empréstimo para `own ref` | `argument N to '<func>': '<param>' is declared 'own ref <T>' and keeps the reference; a reference into a container cannot be kept` | `pass 'ref <name>' of a variable` |
| P3 (runtime) | `simultaneous accesses to '<raiz>': it is borrowed by the call at line N and cannot be copied or shared until that call returns` | — |

## 6. Corpus e migração

### 6.1 Medição de R11 (feita)

Um protótipo de R11 em modo warning foi construído e medido sobre `noxy_examples/`,
`internal/stdlib/` e `tests/` (código de referência no branch `fix/issue-83-borrow-scope`;
esta spec parte de `develop` limpa e reimplementa):

**386 arquivos `.nx` · 15 com erro de parse · 5 arquivos com aviso · 7 sites.**

| Site | Forma | Regra |
|---|---|---|
| `KandR_in_noxy/ch05_arrays.nx:4` | `let pa: ref int = ref a[0]` | R11 |
| `KandR_in_noxy/ch05_ref_basics.nx:10` | `ip = ref z[0]` | R11 |
| `language_semantics_test.nx:284` | `let rel: ref int = ref nums[1]` | R11 |
| `language_semantics_test.nx:289` | `let rp: ref int = ref p.x` | R11 |
| `language_semantics_test2.nx:438` | `let r: ref int = ref m["k"]` | R11 |
| `test_addr_struct.nx:26,27` | `addr(ref p1.x)` | callee sem contrato |

**Nenhum site em `internal/stdlib/`.** Os 14 empréstimos em posição de argumento
(`append(ref s.items, x)`, `pre_order(ref node.left, …)`) ficam legais — é o idioma que
R11 preserva.

Achado do protótipo: `ch06_keycount_ref.nx:46` (`return ref keytab[mid]`) não aparece
porque o arquivo **já não compila hoje** — `[line 22] type mismatch in 'i' declaration:
expected int, got ref int`, resíduo da migração do #82. Bug de corpus anterior a esta
spec; vale issue própria.

**Não medido:** R12 (quais funções guardam parâmetro `ref`) e P2. Medir antes de
promover avisos a erro.

### 6.2 Migração

**R11** — os dois sites do K&R:

```noxy
// ch05_arrays.nx:4 — antes: let pa: ref int = ref a[0]
let pa: int = 0                  // índice, não referência
print(a[pa])

// ch05_ref_basics.nx:10 — ip = ref z[0]   // C: ip = &z[0]
// sem tradução: é ponteiro para dentro de array. O capítulo vira nota
// explicando por que o Noxy não tem isso (JOT §2.5: índices, não referências).
```

Os `language_semantics_test*.nx` viram fixtures de erro.

**R12** — funções que guardam parâmetro `ref` ganham `own`. Conhecida no corpus:
`create_dir` (`noxy_examples/fs_test.nx:71`) guarda `parent` num construtor. Todos os
call sites (`fs_test.nx:168-176`) passam referência de célula: **compilam sem mudança**.

**Lista encadeada, árvore e grafo não mudam.** Verificado rodando: `push`, `soma` e
`imprime` sobre `struct No { valor: int, prox: ref No }` não disparam nada, porque
`ref novo` sobre variável local é R10 e `own` só marca parâmetro.

## 7. Testes

**Regressão (§1.1).** Os seis repros, nas três formas de contêiner (array, map, struct) e
nas duas ordens (`ref` antes e depois da cópia), mais o caso aninhado (`ref` para dentro
de um filho, cópia do pai). A–C como erro de compilação; D–F como erro de runtime.

**A contraprova da §1.3.** Para cada repro: a escrita através do empréstimo **continua
chegando** no original. Um teste de "a cópia está isolada" passa numa implementação que
perde a escrita; este não. É o teste mais importante desta spec.

**Não-regressão de R10.** `ref` a local, a global guardado em campo, retornado, e
empréstimo em posição de argumento (builtin, função com parâmetro `ref`, aninhado).
Mais a **lista encadeada completa sem um `own`**, compilando e rodando — se ela
quebrar, a regra vazou para o idioma da linguagem, e isso importa mais que fechar o
repro.

**R12 nos dois lados.** Callee guardando sem `own` → erro no corpo; com `own` → legal;
célula para `own ref` → legal; empréstimo para `own ref` → erro no call site;
`create_dir` real com `own` → compila.

**P2.** `f(ref a[0], a)` correto; `swap(ref a[i], ref a[j])` legal; e **igualdade de
bytecode** para toda chamada que não mistura empréstimo com argumento por valor.

**P3.** `inline_guard_test.go` estendido a `Retain`/`Release` **antes** da
implementação; unwind por `raise`/`defer` libera o acesso; `-race` com routines
compartilhando contêiner.

**Guardrails.** `go test ./...` verde; `go test -race ./internal/value ./internal/vm`
verde (descontado o achado da §10); corpus e diff de saída sem diferença fora dos
arquivos migrados; `CloneCountValue()` inalterado.

## 7.5 Hipóteses e como falsificá-las

Esta spec é desenho, não demonstração. Cada peça repousa numa hipótese verificável.
Abaixo, cada uma com **experimento**, **o que confirma** e **o que falsifica** — para
que a validação não dependa de convicção de quem escreveu.

### H1 — R11 + R12 fecham A e B sem quebrar o idioma de célula

**Status:** parcial. R11 prototipado e medido (§6.1); `own ref` não existe.
**Experimento:** implementar R12; rodar A e B; rodar a lista encadeada completa; rodar
o gate do corpus.
**Confirma:** A e B viram erro; o gate não cresce além dos 7 sites conhecidos.
**Falsifica:** qualquer idioma de célula (lista, árvore, grafo, `node.next = ref novo`,
`return ref novo`) passar a reclamar. Isso é mais grave que não fechar o repro.

### H2 — a ordem do SE-0176 fecha C reusando o `_MUT` existente ✅ VALIDADA

**Experimento executado** (2026-08-25, binário v0.19.0): escrever o fonte na ordem que a
reordenação produziria, sem implementá-la.

```noxy
func f(r: ref int, xs: int[]) -> void
    *r = 999
    print(xs)
end
let arr: int[] = [1, 2, 3]
let tmp: int[] = arr          // argumento não-ref avaliado ANTES → Owners = 2
f(ref arr[0], tmp)            // o _MUT do empréstimo vê IsShared e clona no slot
print(arr)
```

**Saída:** `[1, 2, 3]` e `[999, 2, 3]`. As duas propriedades valem: a cópia é
independente **e** a escrita através do empréstimo chega no original (contraprova da
§1.3). O mecanismo semântico está confirmado; o que resta em P2 é codegen —
dissociar ordem de avaliação de ordem de pilha (§3.3) —, que é engenharia, não
semântica.
**Falsificaria:** a escrita deixar de alcançar `arr`, ou o bytecode mudar em chamada
sem empréstimo.

### H3 — a checagem de exclusividade cabe dentro do `Retain` 🟡 MEDIDA, apertada

**Experimento executado:** `go build -gcflags=-m=2 ./internal/value`.

| | custo | orçamento |
|---|---|---|
| `ownersOf` | 35 | — |
| `IsShared` | 55 | — |
| **`Retain`** | **67** | 80 → **13 nós de folga** |
| `Release` | 80 | 80 → **zero folga** |

A checagem entra no `Retain`, que tem folga; `Release` está no limite mas não é o lugar.
Uma comparação extra custa poucos nós — `cow.go` registra que "duas comparações custam
os 3 nós que a dica `kind` acrescentou". **Não medido ainda:** o custo de mudar a
assinatura para sinalizar conflito (`Retain(v) bool` → algo que reporte violação).
**Falsifica:** `Retain` passar de 80 com a checagem e a sinalização. Nesse caso a rota é
codificar o estado na palavra de `Owners` (§4.2) antes de desistir.
**Guardrail obrigatório:** estender `inline_guard_test.go` a `Retain`/`Release`
**antes** de escrever a implementação — a propriedade some sem aviso e nenhum teste
funcional a pega.

### H4 — os seis repros esgotam o problema ❌ FALSIFICADA (2026-08-25)

**A validação adversarial rodou e encontrou o sétimo repro: G (§1.1).** Quarta falha
consecutiva desta mesma hipótese. O canal que faltava não era mais um tipo nem mais um
lugar de onde o alias vem — era a **profundidade do caminho raiz→contêiner**, que A–F
mantinham fixa em um nível sem que ninguém tivesse notado.

O custo foi o previsto: **G muda o desenho de P3**, não só a lista de testes. Ver os
três pontos na §1.1 — a exclusividade precisa cobrir o caminho, `Retain` não é funil
suficiente, e a contraprova da §1.3 já é falsa hoje.

P1 (R11/R12) **não** é afetada: G é acesso conflitante, família C–F, e sempre foi
território de P3. A implementação de P1 seguiu.

Registro do que a hipótese era, para o próximo que escrever uma igual:

Esta é a hipótese que já falhou **três vezes** nesta spec: R11 sozinha não pegava C e D;
a exclusividade estática não pegava E; o conserto de E não pegava F. Em todas, o autor
tinha testado — e testado só os casos que tinha imaginado.

**Não existe experimento que o autor possa rodar para confirmá-la.** O protocolo é
adversarial:

- revisão independente, sem o contexto de quem escreveu, com a instrução explícita de
  **procurar um sétimo repro**;
- encontrar um repro novo é **sucesso da validação**, não falha da spec;
- todo repro novo entra na §1.1 e vira teste antes de qualquer implementação seguir.

Quem for validar deve começar pelo **repro F** (alias dentro de um valor, call site sem
menção à raiz): é o que derruba qualquer proposta de fechar C–F estaticamente, e é o
teste mais barato de "esta ideia nova já foi descartada?".

## 8. Alternativas descartadas

| Rota | O que é | Por que não |
|---|---|---|
| **Pin / clone eager** (proposta da issue) | `ref` para dentro de contêiner conta como owner; a próxima cópia materializa na hora | Sem precedente: nenhuma linguagem estabelecida marca um contêiner para sempre por causa de uma referência. Obstáculo estrutural: `Retain(v Value) bool` não pode **substituir** o valor, então a cópia ansiosa teria de ser replicada nos 41+ sites de ligação — enumeração aberta, o mesmo modo de falha que ela deveria evitar. E o pin é *sticky*: sem RC de referências, não há como saber quando o último `ref` morreu, então um contêiner que teve empréstimo copia ansioso para sempre — custo invisível na linha que o paga, ferindo "copies are cheap" do README |
| **Empréstimo path-based** | `ObjRef` guarda o LUGAR do pai e re-resolve na escrita | ~~Invenção sem precedente. Muda a representação de referência da VM para resolver um caso; o Swift não faz isso~~ — **ESTA REJEIÇÃO ESTAVA ERRADA, e é a rota que consertou o bug (2026-08-25).** Não é invenção: é como se modela um *lugar*, e a caminhada com unicização e gravação de volta **já estava implementada** na família `_MUT` (§1.4) para `a[i].x = v`. A correção reusa essa caminhada mudando só o INSTANTE em que roda — criação → escrita. O erro de análise foi ler "re-resolve na escrita" como sinônimo da armadilha da §1.3 (unicizar na escrita, que perde a escrita num clone anônimo); o que separa as duas é a GRAVAÇÃO DE VOLTA em cada nível do caminho |
| **Pré-passe de escape** (ponto fixo) | inferir quais parâmetros `ref` o callee guarda | §2.2: torna a legalidade de uma chamada dependente de corpo invisível. Nenhuma das quatro referências infere |
| **Exclusividade estática no call site** | proibir que outro argumento alcance a raiz | Enumeração aberta: repro F põe o alias dentro de um struct e o call site não menciona a raiz |

**O que a proposta da issue tem de certo:** o diagnóstico (é a contagem CoW que precisa
saber da referência) e a localização (`Owners`/`Retain` já existem desde a #66). P3 usa
exatamente essa infraestrutura — muda só o que se faz ao detectar: **falhar**, em vez de
copiar.

## 9. Riscos

### 9.1 Prioridade

Este bug exige sequência específica e aparece em 5 sites reais, todos em arquivos de
teste de semântica ou no port do K&R. Duas issues abertas têm custo/benefício melhor e
devem vir antes: **#47** (typo em nome global só explode em runtime — "o maior salto de
solidez disponível", palavras da issue) e **#75** (`int + string` só falha em runtime).

### 9.2 Rollout

**v0.20.0** — P1 e P2 em modo **warning** (canal `diagOut`, `--> arquivo:linha`, sem
tocar stdout nem exit code; `cmd/noxy/warnings_test.go` trava a propriedade). Ninguém
quebra, e a medição real substitui a estimativa.
**v0.21.0** — P1/P2 viram erro, corpus migrado, e P3 entra.

Se a medição acusar impacto muito além do previsto — sobretudo em `internal/stdlib/` — a
decisão volta para a mesa antes de qualquer quebra.

### 9.3 P2: incógnita reduzida a codegen

A semântica de P2 está **validada** (§7.5 H2). O que resta é a reordenação em si
(§3.3): dissociar ordem de avaliação de ordem de pilha sem mudar o bytecode de nenhuma
chamada que não misture empréstimo com argumento por valor. **Protótipo antes de
aprovação**, com igualdade de bytecode como critério.

### 9.4 Arrasto de escopo

O que mais provavelmente dá errado não é técnico: é esta spec crescer para "aderir a
mutable value semantics", removendo `ref` de primeira classe e reescrevendo estruturas
ligadas como arena de índices (JOT §2.5). Decisão de 1.0, não este fix.

### 9.5 Histórico de erros desta spec

Versões anteriores afirmaram cobertura completa **três vezes** e erraram nas três: R11
sozinha não pegava C e D; a exclusividade estática não pegava E; o conserto de E não
pegava F. O padrão — enumeração aberta de canais — é o que motivou adotar o enforcement
dinâmico do SE-0176. Qualquer proposta futura de fechar C–F estaticamente deve começar
pelo repro F.

## 10. Achado independente, não relacionado

`go test -race ./internal/vm -run TestServerFramesRequestDeliveredOneByteAtATime` acusa
`DATA RACE`. **Não vem desta spec:** reproduzido com `-count=5` em `git worktree` limpo
no `20c207c`.

```
Write  goroutine 18: executor.go:1520  instance.Fields[name] = val       (OP_SET_PROPERTY)
Read   goroutine 19: executor.go:1454  val, ok := instance.Fields[name]  (OP_GET_PROPERTY)
```

Goroutine 18 é o `Interpret` do servidor HTTP; a 19 é routine de `spawn`
(`builtins_concurrency.go:96`). As duas mexem no mesmo `map[string]Value` de um
`ObjInstance` — candidato a `fatal error: concurrent map read and map write`, não
recuperável. Encosta na §2.2 (*"An individual binding lookup/update or map operation is
safe from the Go runtime's concurrent-map crash"*): vale para `ObjMap`, **não** para os
`Fields` de `ObjInstance`.

**A CI não pega:** o job de rede roda `-race` com o filtro
`Test.*(Network|Net|Socket|Listener|Deadline|Timeout|Poll|Wake|WSA|PendingRead|PendingWrite|PendingAccept|BlockedWrite)`,
que não casa com esse teste (verificado por regex e por `go test -run`, 0 execuções); o
passo seguinte roda o teste **sem `-race`**. Buraco no filtro, não intermitência.
Conserto sugerido: `-race ./internal/vm` inteiro. **Issue própria.**

## 11. Referências

- **Swift SE-0176 — Enforce Exclusive Access to Memory** (a base desta spec).
  <https://github.com/swiftlang/swift-evolution/blob/main/proposals/0176-enforce-exclusive-access-to-memory.md>
- Exclusivity enforcement por padrão em release builds no Swift 5.
  <https://www.infoq.com/news/2019/02/swift-5-exclusive-memory-access/>
- Racordon, Shabalin, Zheng, Abrahams, Saeta. *Implementation Strategies for Mutable
  Value Semantics.* JOT 21(2), 2022.
  <https://www.jot.fm/contents/issue_2022_02/article2.html> — §2.5 (grafos por índice),
  §5.3 (por que o ponteiro interior é seguro), §6.3 Ex. 6.2 (este bug, e a cópia
  defensiva do chamador).
- Hylo specification — convenções `let`/`inout`/`sink`/`set` e projections.
  <https://github.com/hylo-lang/specification/blob/main/spec.md>
- C# span-safety e o modificador `scoped` — precedente de "declarar, não inferir".
  <https://github.com/dotnet/csharplang/blob/main/proposals/csharp-7.2/span-safety.md>
- Nim manual — `var T` (não escapa) × `ref T` (heap).
  <https://nim-lang.org/docs/manual.html>
- `specs/2026-08-24-explicit-ref-design.md` §2 "Fora", onde este caso foi registrado.
