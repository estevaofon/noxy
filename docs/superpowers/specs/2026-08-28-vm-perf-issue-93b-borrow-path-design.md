# VM Perf — Custo por nível do empréstimo aninhado (issue #93, parte b)

**Data:** 2026-08-28 · **Issue:** [#93](https://github.com/estevaofon/noxy/issues/93) (b) · **Branch:** `perf/issue-93b-borrow-path` · **Base:** `develop` (b4eca8a, pós-#96/#98)

## 1. Contexto (medido nesta sessão)

BST por posse (`esquerda: TreeNode` + `insert(ref node.esquerda, v)`), 50k
chaves aleatórias: **2,26 s** na base, contra 0,69 s da versão com `ref`. O
#96 não mudou nada aqui (2,29 s): o caminho do empréstimo resolve o campo por
NOME em cada nível. Perfil (head do #96):

| símbolo | flat | cum |
|---|---|---|
| `borrowContainer` | 10,2 % | 53,7 % |
| `descend` | 12,4 % | 32,5 % |
| `referenceStorageMode` (+ `defer` com `validateReferencedValue` → `reflect`) | 3,3 % | 59,5 % |
| `derefPlace` | 6,9 % | 6,9 % |
| `(*ObjInstance).Get` → `FieldIndex` → `mapaccess2_faststr`/`aeshashbody` | — | **16,4 %** |
| GC (`scanSpan`, `tryDeferToSpanScan`, `scanObjectsSmall`) | — | ~25 % |

Por que O(profundidade²): `insert` no nível k cria `ref node.esquerda` com
`Base` encadeado k vezes; cada acesso (`*node`, `node.valor`, criação do ref
seguinte) re-resolve os k níveis. É o modelo de lugar da #83 (correto: o
caminho é a identidade do empréstimo) e este PR **não** o muda.

## 2. Por que "fast path quando `Owners == 1`" não serve como está

A direção sugerida na issue ("contêiner único → não precisa unicizar nem
regravar") é insegura sem validar a cadeia: com `r = ref a.inner`, depois
`let b = a` e `a.inner = Inner(9)` (que clona `a` para `a'` na célula), o
`Container` congelado de `r` — o `a` velho — fica com `Owners == 1` (só `b` o
possui) e **fora do lugar** `a.inner`: escrever por `r` iria parar em `b`.
Unicidade do objeto não prova que ele ainda está no lugar; só a caminhada
prova. Uma versão segura ("cada nível da cadeia ainda aponta para o filho
congelado, todos únicos") continua O(profundidade), só com constante menor.

Matar o quadrático exige memoizar a resolução entre acessos, com um sinal de
invalidação global (época que sobe a cada gravação de composto em qualquer
lugar — SET_LOCAL/GLOBAL/upvalue/campo/elemento/map, natives que trocam
elementos, gravação de volta do CoW). É uma mudança de arquitetura com cauda
longa de sites (esquecer um = cache obsoleta = escrita no objeto errado).
Fica documentada como follow-up (§6), não entra aqui.

## 3. O que este PR faz (constante por nível)

1. **Slot no `ObjRef`.** `OP_REF_PROPERTY` resolve `FieldIndex(nome)` UMA vez,
   na criação (o contêiner já é resolvido ali para as checagens), e guarda em
   `ObjRef.Slot`. `descend` e `referenceStorageMode` (caso `REF_PROPERTY`) usam
   `Slots[Slot]` quando `Slot < len(Fields) && Fields[Slot] == Name` — a mesma
   guarda do #96 (definições de JSON em ordem alfabética, ObjRef montado à mão
   por natives/testes com `Slot` zero). Falhou → `Get(nome)` como hoje.
   O `referenceSetter` de propriedade recebe o slot já validado e grava
   `Slots[slot]` em vez de `MustSet(nome)`.
2. **`validateReferencedValue` sem `reflect` no caso comum.** Type switch nos
   payloads concretos (`*ObjInstance`, `*ObjArray`, `*ObjMap`, `string`,
   `*ObjRef`…) antes de cair no `reflect.ValueOf` genérico — mesma semântica
   (nil → "invalid referenced object").
3. **`descend` sem chamada quando o contêiner não é ref**: o teste
   `container.Type == VAL_REF` fica inline antes de `derefPlace`.

Semântica, mensagens, linhas e contagem RC idênticas; os sete repros da #83
(`borrow_place_test.go`) continuam o critério.

## 4. Verificação

- `go test ./internal/vm -run 'Borrow|Ref'` e `go test ./...` verdes; `-race` no
  pacote `vm`.
- Teste novo: `ObjRef{REF_PROPERTY, Slot: errado}` montado à mão resolve pelo
  nome (a guarda); ref a campo de instância JSON reordenada lê e escreve o
  campo certo.
- Benches novos na suíte: `bench_bst_owned.nx` / `bench_bst_ref.nx` (as
  fixtures da issue, com `CHECKSUM:`); `bench_borrow_path.nx` ganha `CHECKSUM:`
  (imprimia três números soltos e o guard de equivalência o pulava).
- A/B intercalado base × head, mediana de 9; corpus 180/180; `compare_examples`.

## 5. Riscos

| risco | mitigação |
|---|---|
| `Slot` errado (ObjRef de native/teste, JSON) | guarda `Fields[Slot] == Name`; zero value é seguro |
| `validateReferencedValue` deixar passar payload nil | switch cobre os tipos que a VM guarda em `Obj`; o `default` continua no `reflect` |

## 6. Follow-up (não incluído): cache por época

`ObjRef` guardaria `(container resolvido, época, modo)`; `vm`/`value` uma
época global atômica que sobe em toda gravação de composto em lugar e em toda
gravação de volta do CoW; acesso com época igual reusa o contêiner (leitura) ou
o caminho já unicizado (escrita). Transforma O(profundidade) por acesso em
O(1) amortizado; exige inventário completo dos sites de gravação (VM, natives
`append/pop/sort/remove`, `json_loads`, canais).
