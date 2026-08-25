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

`null` é valor válido de `ref T`: pode ser guardado, passado, retornado,
comparado e substituído por rebind. Escrever através de `null` é erro de
runtime.

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
em qualquer profundidade (copy-on-write). A assinatura sem `ref` garante que
o chamador não é tocado; o call site sem `ref` garante o mesmo.
