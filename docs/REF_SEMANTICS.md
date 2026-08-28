# Referências em Noxy

`ref T` é uma referência de primeira classe a um slot que contém um `T`. A
regra inteira cabe em uma frase: **uma referência nunca é criada nem lida
sem `ref` ou `*` no código** — e `.`/`[]` são o único atalho. A
especificação completa (R1–R9, diagnósticos) está em
[`NOXY_LANGUAGE_SPEC.md` §2.3](NOXY_LANGUAGE_SPEC.md#23-references-ref).

## As três formas

| Escrita | Significado |
|---|---|
| `ref x` | cria uma referência para o slot de `x` (variável, campo, índice, entrada de map, capturada) |
| `r` | a referência em si — para onde aponta |
| `*r` | o valor apontado: lê em expressão, escreve com `*r = v` |
| `r.f`, `r[i]` | atalho para `(*r).f`, `(*r)[i]` — leitura e escrita |

```noxy
let x: int = 10
let y: int = 20
let r: ref int = ref x

let n: int = *r        // le: 10          (`let n: int = r` e erro)
*r = 20                // escreve em x
r = ref y              // rebind: r passa a apontar para y
print(r)               // <ref ...>: a referencia
print(*r)              // o valor
```

## Em chamadas

Um parâmetro `ref T` recebe `ref x`, uma expressão que já é `ref T`, ou
`null`. Nunca um `T` cru — o call site mostra o que pode mudar:

```noxy
func increment(value: ref int) -> void
    *value = *value + 1
end

let answer: int = 41
increment(ref answer)   // answer passa a ser 42
increment(answer)       // erro: expected ref int, got int — hint: use 'ref answer'

append(ref xs, 1)       // builtins seguem a mesma regra
pop(ref xs)
delete(ref m, "k")
json_loads(texto, ref alvo)

func push(p: ref int[]) -> void
    append(p, 9)        // p ja e ref int[]: passa direto, sem `ref`
end
```

Não há distinção entre função tipada, `func` bare, construtor, generic ou
nativo. `ref` sobre algo que já é referência é erro (`'p' is already a
reference`); não existe `ref ref T`.

## Update × rebind

| LHS | RHS | Escrita | Ação |
|---|---|---|---|
| `ref T` | `T` | `*r = v` | escreve no slot apontado |
| `ref T` | `ref T` | `r = ref y` | troca para onde `r` aponta |
| `T` | `ref T` | `x = *r` | lê (`x = r` é erro) |

`*r = ref y` é erro com hint: `r = ref y` para rebind, ou `*r = y` para
escrever o valor.

## Comparação

Dois refs comparam **identidade de slot**; `r == null` pergunta se a
referência é nula; `r == 1` é erro — `*r == 1` compara valores.

## `null`

`null` é valor válido de `ref T?` — e só dela (spec §2.4, 0.22.0). Um `ref T`
nu nunca é `null`: `let r: ref int = null`, `f(null)` para `n: ref Node` e
`Node(1, null)` para um campo `next: ref Node` são erros de compilação, com o
hint `declare it as 'ref T?' to allow null`. Uma `ref T?` pode ser guardada,
passada, retornada, comparada com `null` e substituída por rebind; ler ou
escrever através dela (`*r`, `r.f`, `*r = v`) exige o teste antes —
`if r != null then … end` estreita `r` para `ref T` dentro do bloco
(`'r' may be null; test it first`). O erro de runtime "cannot dereference
null reference" fica só para referências que chegam por `any`.

`ref (T?)` é a outra forma: referência não-nula a um slot cujo *valor* pode
ser `null` (`ref raiz` com `raiz: TreeNode?`); `*r` é `T?` e é testado do
mesmo jeito.

## Tempo de vida de uma local referenciada

`ref x` sobre uma local promove o slot de `x` a uma célula no heap. A célula
vive enquanto houver qualquer referência para ela — inclusive depois de a
função retornar. É assim que se alocam nós; não há `new`:

```noxy
let novo: Node = Node(v, null)   // uma variavel: `ref` exige um l-value
node.next = ref novo             // `novo` vira celula; sobrevive a funcao
```

Custo: uma alocação por local referenciada; locais nunca referenciadas ficam
na pilha. Uma closure que captura `ref` a uma local e vai para `spawn`
compartilha a célula entre routines — coordene, como com globais
([concurrency.md](concurrency.md)).

## Parâmetros comuns e semântica de valor

Sem `ref`, arrays, maps e structs são passados por **valor** — independentes
em qualquer profundidade através dos campos de *valor* (copy-on-write). A
assinatura sem `ref` garante que o chamador não é tocado através desses
campos; o call site sem `ref` garante o mesmo.

A fronteira é um campo declarado `ref`: ele é uma aresta compartilhada, e a
cópia carrega a mesma aresta — `f(caixa)` com `caixa.inner: ref Node` deixa o
callee escrever em `*caixa.inner`. Isso está escrito no **tipo** (`struct`),
não na chamada (spec §2.2 regra 6). Para estrutura de dono único (lista,
árvore), declare o campo sem `ref` — `next: Node?` aceita `null` — e mute pelo
slot: `insert(ref node.next, v)` com `node: ref (Node?)` (spec §5 *Self-Reference*,
`noxy_examples/bst_owned.nx`). Reserve campo `ref` para compartilhamento de
verdade: nó com dois pais, `prev`, ponteiro de pai, grafo.
