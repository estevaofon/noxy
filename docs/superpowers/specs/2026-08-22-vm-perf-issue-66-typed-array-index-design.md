# VM Perf — Indexação tipada de array (issue #66, item 1)

**Data:** 2026-08-22 · **Issue:** [#66](https://github.com/estevaofon/noxy/issues/66) item 1 · **Branch:** `perf/issue-66-typed-array-index` · **Base:** `develop` (7eed082, v0.14.3)

## 1. Contexto (medido nesta sessão, binário de 7eed082)

**`cross_runtime/bubblesort.nx`** (`data: ref int[]`, 5,5x do CPython): o perfil
mostra `run` com 25 % flat e **`resolveReferenceValue`/`referenceStorage` com
42 % cumulativo**, `mallocgc` 15 %, `ObjUpvalue.Load` 10,6 %,
`unicizeThroughRefValue` 10,6 %. Cada `data[j]` é `OP_GET_LOCAL` + `OP_DEREF` →
`referenceStorage` (um `defer`, a closure do setter alocada no heap a cada
chamada, `validateReferencedValue` com `reflect`) + `OP_GET_INDEX`; cada
`data[j] = x` é `OP_GET_LOCAL_MUT_BORROW` + `OP_DEREF_MUT` (a mesma maquinaria
via `unicizeThroughRefValue`) + `OP_SET_INDEX` (guarda de slot ref, `Retain` e
`Release` — duas chamadas reais que são no-op para `int`) + `OP_POP`. Os anchors
da issue descrevem o local plano `arr: int[]`; o bench que ela cita passa por
`ref`, e ali o custo dominante é a resolução do ref, não o despacho do índice.

**Local plano `arr: int[]`** (`bench_call_readonly`, `s = s + data[i]`): leitura
= `OP_GET_LOCAL` (push do `Value` de 32 B do array) + `OP_GET_INDEX` (dois
pops, teste `VAL_OBJ`, assertion); escrita = `OP_GET_LOCAL_MUT` (`IsShared` —
custo 55, chamada real dentro de `run()`) + índice + valor + `OP_SET_INDEX` +
`OP_POP`. Achado colateral: 37 % desse bench é `callNative` — o `length(data)`
da condição do `while` é chamada de builtin genérica, não indexação (fora do
escopo; follow-up).

## 2. Objetivo e não-objetivos

**Objetivo:** quando o compilador sabe que a base é `T[]` (ou que um local é
`ref T[]`), indexar sem despacho dinâmico, sem empilhar o `Value` do array, sem
`IsShared`/`Retain`/`Release` para elemento sem contador RC, e — no caso
`ref T[]` — sem `referenceStorage`. Semântica, saída, mensagens de erro, linhas
de erro e contagem RC **idênticas**; opcodes só por append.

**Não-objetivos:** `length()` → `OP_LEN`; variante tipada de `OP_GET_INDEX_MUT`
(nível de fora de `a[i][j] = v` segue genérico); forma fundida para elemento
composto (`nodes[i] = n` segue `OP_SET_INDEX`); deref de upvalue sem o
`RWMutex`; strings (item 2); maps/structs (item 4).

## 3. Desenho

### 3.1 Opcodes (append ao fim de `internal/chunk/chunk.go`)

| opcode | operando | pilha | emitido quando |
|---|---|---|---|
| `OP_GET_INDEX_ARRAY` | – | `[arr, i] → [elem]` | base estática `T[]` em posição genérica (global, upvalue, `a[i][j]`, retorno de chamada, índice impuro) |
| `OP_SET_INDEX_ARRAY_NORC` | – | `[arr, i, v] → []` | idem, em escrita, com elemento `int/float/bool/string/bytes` |
| `OP_GET_LOCAL_INDEX_ARRAY` | `[slot]` | `[i] → [elem]` | base é local `T[]` com índice puro (§3.3); também o `$collection[$index]` do for-each sobre array |
| `OP_SET_LOCAL_INDEX_ARRAY_NORC` | `[slot]` | `[i, v] → []` | local `T[]` possuidor, elemento sem RC, índice e valor puros |
| `OP_GET_REF_LOCAL_INDEX_ARRAY` | `[slot]` | `[i] → [elem]` | base é local `ref T[]` (parâmetro `ref`), índice puro |
| `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC` | `[slot]` | `[i, v] → []` | local `ref T[]` (não-possuidor), elemento sem RC, índice e valor puros |

Os de escrita **não empilham** resultado (atribuição é statement, como
`OP_INC_LOCAL_INT`) e o compilador não emite `OP_POP` depois deles. `NORC` =
"sem Retain/Release": o compilador só os emite para elemento estático sem
contador, e o VM **confere em runtime** (§3.2) antes de pular o RC.

### 3.2 VM — caminho rápido e fallback exato

**Leituras.** Caminho rápido: container é `*ObjArray` (uma assertion — com
`Obj` sendo `*ObjArray` o `Type` é `VAL_OBJ` por construção, como em
`ownersOf`), índice `VAL_INT` ("array index must be integer"), bounds ("array
index out of bounds"), resultado gravado **no lugar** na pilha — sobre o slot
do operando mais fundo (`[arr, i] → [elem]` escreve onde estava `arr`; `[i] →
[elem]` onde estava `i`), sem pop/push. `OP_GET_REF_LOCAL_INDEX_ARRAY` resolve só `REF_UPVALUE` (a forma que
`OP_REF_LOCAL` cria para todo `ref` a local) via `ref.Upvalue.Load()` — uma
chamada com o `RWMutex`, em vez de `referenceStorage`. Fallback (container
não é array, ref de outro tipo, nulo, slot com valor plano pela tolerância
herdada): materializa a pilha do caminho genérico (`[container, i]`; para o
ref, com a semântica exata de `OP_DEREF`: nulo e não-ref passam, ref resolve
por `resolveReferenceValue`) e **re-despacha** `instruction = OP_GET_INDEX`
com `goto redispatch` (rótulo logo antes do `switch` de `run()`). Zero custo
no caminho genérico, zero duplicação, mesmas mensagens e mesma linha (`Lines`
da mesma statement).

**Escritas NORC.** Caminho rápido exige **todas**: container é `*ObjArray`;
(formas com slot) `arr.Owners.Load() <= 1` — o mesmo teste de `IsShared`, sem
a chamada; índice `VAL_INT` e em bounds; a tag do array **não** é `(ref T)[]`
(a guarda de slot ref do `OP_SET_INDEX`, com o mesmo `RuntimeType.Load()`);
`value.NeverTracked(val) && value.NeverTracked(old)` (§3.4) — só então
`arr.Elements[i] = val` sem `Retain`/`Release`. Qualquer condição falha →
fallback com a semântica exata da sequência genérica:

- `OP_SET_LOCAL_INDEX_ARRAY_NORC`: `vm.unicizeOwnedSlot(frame, idx)` (o corpo
  de `OP_GET_LOCAL_MUT` extraído em método; o `case` genérico passa a chamá-lo
  — mesma contagem de chamadas, pois `IsShared` já era chamada real), depois
  `push(arr, i, v)` e `vm.setIndexGeneric(c, ip)`, depois `LastPopped = pop()`
  (o `OP_POP` da sequência genérica).
- `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC`: semântica de `OP_GET_LOCAL_MUT_BORROW`
  (slot não-ref: `IsShared` → clone gravado no slot sem RC) + `OP_DEREF_MUT`
  (ref → `unicizeThroughRefValue`, que clona e grava de volta pelo setter se
  compartilhado; não-ref passa), depois `setIndexGeneric` + `pop`.
- `OP_SET_INDEX_ARRAY_NORC`: `setIndexGeneric` + `pop`.

`setIndexGeneric(c, ip) error` é o corpo de `OP_SET_INDEX` extraído em método
(pops, lógica array/map/erros, push do valor); o `case OP_SET_INDEX` passa a
ser `if err := vm.setIndexGeneric(c, ip); err != nil { return err }`. Custo:
**uma chamada a mais em cada `OP_SET_INDEX` genérico** (maps, `any`, elemento
composto) — medido em `bench_map_churn`; a alternativa (duplicar ~45 linhas de
lógica de erro nos três fallbacks) foi rejeitada em favor do funil único.

**Por que o fallback das escritas é alcançável:** `let a: int[] = x` com
`x: any` compila (`areTypesCompatible` aceita `any`) e o marcador
`OP_MARK_RUNTIME_VALUE_TYPE` recusa nulo e não-array no bind — então um slot
`T[]` plano guarda sempre array; mas parâmetro `ref T[]` aceita `null`
(`bubble(null)` → "cannot set index on non-array/map" / "cannot index
non-array/map/bytes"), `a[i] = v` com `v: any` pode trazer composto (o
genérico retém; o NORC vê `NeverTracked(val)` falso e cai no genérico), e o
array compartilhado (`let b = a` antes de `a[0] = 9`) tem de clonar.

### 3.3 Compilador

**Predicado `isSideEffectFree(expr)`** (decide a forma fundida): `Identifier`,
literais `Integer/Float/String/Bytes/Boolean/Null`, `PrefixExpression` com
`-`, `!`, `~`, `*` sobre operando puro, `InfixExpression` com ambos os lados
puros, `IndexExpression` e `MemberAccessExpression` com partes puras. Tudo o
mais (chamadas — inclusive builtins e f-strings que viram chamadas —,
literais de array/map, closures, `ref`, `zeros`) → impuro → forma não fundida.
Motivo: a forma fundida lê o slot **depois** de avaliar índice (e valor); com
operandos sem efeito colateral nenhum código roda no meio que possa rebindar
o local (closure, `ref`) ou compartilhá-lo, e a ordem observável é a mesma da
sequência genérica. Exceção teórica documentada: falha dupla com um `ref`
**obsoleto** como base (`REF_INDEX` para array que encolheu) **e** índice/valor
puro que também falha (`a[i / 0]`) — o genérico reporta o erro do ref, a forma
fundida o do índice; não há `ref` obsoleto no corpus.

**Leitura `X[i]`** (`case *ast.IndexExpression` em `Compile`): se `X` é
`Identifier` resolvido a local (slot ≤ 255) de tipo `ArrayType` e `i` é puro →
compila `i` (com o `OP_DEREF` de índice ref, como hoje) e emite
`OP_GET_LOCAL_INDEX_ARRAY slot`; se o tipo é `RefType{ArrayType}` e `i` puro →
`OP_GET_REF_LOCAL_INDEX_ARRAY slot`. Senão, o caminho de hoje (`Compile(X)`,
`OP_DEREF` se ref, índice) e `OP_GET_INDEX_ARRAY` quando o tipo de `X`
desembrulhado é `ArrayType`, `OP_GET_INDEX` caso contrário. O for-each sobre
`ArrayType` troca `GET_LOCAL $collection; GET_LOCAL $index; GET_INDEX` por
`GET_LOCAL $index; OP_GET_LOCAL_INDEX_ARRAY $collection`. Tipo do resultado e
checagens de compilação inalterados.

**Escrita `X[i] = v`** (alvo `IndexExpression` em `AssignStmt`): se `X` é local
`ArrayType` com elemento `int/float/bool/string/bytes`, `c.localOwns(slot)`, e
`i` e `v` puros → **não** chama `compileLValueBase`; compila `i` (+`OP_DEREF`
se ref), compila `v`, roda exatamente as checagens de tipo de hoje (mesmas
mensagens, mesma ordem — `compileLValueBase` nunca erra para identificador),
`emitRuntimeValueType(elem)` (no-op para esses tipos), emite
`OP_SET_LOCAL_INDEX_ARRAY_NORC slot`, sem `OP_POP`. Se `X` é local
`RefType{ArrayType}` com elemento sem RC, `!c.localOwns(slot)` e operandos
puros → `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC slot`. Senão, o caminho de hoje
(`compileLValueBase` etc.) e `OP_SET_INDEX_ARRAY_NORC` (sem `OP_POP`) quando o
tipo desembrulhado é `ArrayType` com elemento sem RC; `OP_SET_INDEX` + `OP_POP`
caso contrário. `Owns` continua a fonte de verdade (spec CoW-RC §4.2): flag
em estado inesperado → genérico.

### 3.4 `value.NeverTracked`

```go
// NeverTracked: v certamente nao tem contador RC (escalar, ou VAL_OBJ
// carimbado como sem dono — string/struct/RTI). Conservador: VAL_OBJ sem
// carimbo (kind zero) responde false e o chamador cai no caminho generico.
func NeverTracked(v Value) bool { return v.Type != VAL_OBJ || v.kind == objKindNoOwners }
```

Chamada de dentro de `run()` → precisa caber em 20 no inliner; guard em
`inline_guard_test.go`.

## 4. Invariantes e guards executáveis

- Opcodes só por append; sentinela de `TestEveryOpcodeHasASymbolicNameWithoutGaps`
  atualizada para o último; `disassemblyProgram` ganha leituras/escritas
  tipadas (local plano, `ref`, nested) para `TestDisassemblerDecodesEveryEmittedInstruction`.
- Bytecode (testes no pacote `compiler`, via disassembler capturado, não por
  varredura de bytes): cada regra de §3.3 com caso positivo e negativo
  (índice com chamada → não fundido; upvalue → `OP_GET_INDEX_ARRAY`; elemento
  composto → `OP_SET_INDEX`; map/`any` → genérico; `OP_POP` ausente após fundido).
- Comportamento (pacote `vm`): bubblesort via `ref` e via local; CoW — `let b =
  a; a[0] = 9` (b intacto, `CloneCountValue() == 1`), `ref` a array compartilhado
  (clone gravado de volta, chamador vê a mutação, cópia intacta); RC — elemento
  composto via `any` (`OwnersCount` do composto sobe 1 como no genérico);
  `string[]` escrita; tabela de erros idênticos (OOB leitura/escrita por local,
  por `ref`, `null` em parâmetro `ref`, índice não-int via `any`) comparando o
  texto com o mesmo programa de base `any`; for-each sobre array com mutação
  durante a iteração (comportamento de hoje preservado).
- Guards de inline: `push` ≥ 100 / `pop` ≥ 70 sites (novos sites só somam);
  `NeverTracked` ≤ 20; `Retain`/`Release` ≤ 80 inalterados.
- `go test ./...`, `go test -race ./internal/value ./internal/vm`, corpus
  `run_all_tests_concurrent.nx` 0 falhas, `compare_examples.ps1` 0 divergentes.

## 5. Medição (protocolo de `benchmarks/RESULTS.md`)

Binários em disco local (scratchpad): `noxy_base.exe` (7eed082), `noxy_s1.exe`
(opcodes não fundidos), `noxy_s2.exe` (+ fundidos de local plano e for-each),
`noxy_s3.exe` = head (+ fundidos `ref`). Máquina sem `go test` concorrente.

1. `interleaved_compare.ps1 -Runs 9` base × head (headline) e uma intercalação
   dos quatro na mesma janela (por estágio).
2. `cross_runtime/run_cross_runtime.ps1 -NoxyBaseline` base × head (mínimo de 9).
3. Perfil de `bubblesort` base × head (share de `referenceStorage`/`Load`).
4. Gates CoW ≤ +5 % (`bench_typed_call_map`, `bench_share_mutate`,
   `bench_call_light`, `bench_conway`); `bench_map_churn` citado (custo do
   `setIndexGeneric`).
5. Nova seção no topo de `RESULTS.md` + `results/2026-08-22-issue-66-typed-arrays-raw.md`.

## 6. Riscos

| risco | mitigação |
|---|---|
| forma fundida muda ordem de avaliação | só com índice/valor sem efeito colateral (§3.3); teste de closure/`ref` no índice → não fundido |
| NORC pular RC de um composto vindo por `any` | `NeverTracked(val) && NeverTracked(old)` em runtime; teste com oráculo `OwnersCount` |
| fallback divergir do genérico | leitura re-despacha o próprio `OP_GET_INDEX`; escrita chama o mesmo `setIndexGeneric`; tabela de erros idênticos |
| `setIndexGeneric` regredir map | medido em `bench_map_churn`; se > ruído, reverter para duplicação no fallback |
| ganho não se materializar | hipótese; números publicados como estão |

## 7. Decisões tomadas sem consulta (para a review)

1. **Escopo inclui o local `ref T[]`** — o bench da issue passa por `ref` e o
   perfil põe a resolução do ref como custo dominante; é a mesma família de
   opcodes, sem tocar `referenceStorage`/RWMutex.
2. Elemento composto não ganha opcode fundido; `OP_SET_INDEX` genérico vira
   chamada a `setIndexGeneric` (funil único), custo medido.
3. Formas fundidas exigem operandos sem efeito colateral (predicado sintático,
   como `tryFuseLocalIntIncrement`), com a exceção teórica da falha dupla.
4. `length()` → `OP_LEN` fica como follow-up (call_readonly 37 % em `callNative`).
5. Versão **v0.14.4** (patch: perf interna).
