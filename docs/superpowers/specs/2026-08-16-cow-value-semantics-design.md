# Semântica de Valor com Copy-on-Write (CoW)

**Data:** 2026-08-16
**Branch:** `feat/cow-value-semantics` (base: `origin/develop` @ c429bd7, v0.3.0)
**Status:** aprovado em discussão; spec formal para implementação

## 1. Objetivo

Substituir os três regimes atuais de semântica de compostos (arrays, maps, structs) por
**um único regime de semântica de valor**, implementado via copy-on-write.

Regimes hoje:

| Operação | Comportamento atual |
|---|---|
| `let b = a` (atribuição) | aliasing — mutar `b` afeta `a` |
| chamada de função (param sem `ref`) | cópia rasa ansiosa (`copyValue`, `calls.go:102`) — mutação aninhada vaza |
| param `ref` / builtins mutantes | compartilhamento por slot |
| `==` entre compostos | identidade de ponteiro (`stack.go:27`) — instável através de chamadas |

Problemas: a cópia rasa não é nem valor nem referência (mutação aninhada vaza para o
chamador); atribuição e chamada têm semânticas diferentes; toda chamada paga O(n)
mesmo quando a função só lê.

## 2. Contrato semântico (visível ao usuário)

> **Todo array, map ou struct se comporta como uma cópia profunda e independente em
> qualquer vínculo sem `ref`. Duas variáveis nunca interferem uma na outra — a menos
> que o compartilhamento seja pedido explicitamente com `ref`.**

Regras observáveis:

1. **Atribuição é cópia**: `let b = a` e `x = y` produzem valores independentes em
   qualquer profundidade.
2. **Chamada é cópia**: argumentos sem `ref` são valores independentes em qualquer
   profundidade — mutação aninhada não vaza mais.
3. **Ler de contêiner é cópia**: `let c = a[0]` produz valor independente; mutar `c`
   não afeta `a[0]`.
4. **Armazenar em contêiner é cópia**: `append(out, inner)`, `m[k] = v`, `s.f = arr`,
   `Person(arr, …)` guardam valores independentes; mutar o original depois não afeta
   o que foi guardado.
5. **Canais transportam valores**: `chan_send` entrega cópia independente. Vale para
   `spawn` e `spawn_task` igualmente — **a exceção legada do `spawn` (encaminhar
   identidade) é removida**.
6. **`ref` é o único mecanismo de compartilhamento.** Um `ref` aponta para um *slot*
   (variável, campo, índice, entrada de map). Escrita através de qualquer alias do
   slot é visível por todos os aliases do slot. Um valor lido de um slot referenciado
   é cópia independente a partir daquele momento.
7. **`==`/`!=` entre compostos é estrutural** (recursivo): arrays elemento a elemento,
   maps por conjunto de chaves + valores, instâncias por mesmo struct + campos.
   Valores `ref` comparam por identidade de slot e **não são dereferenciados** na
   comparação estrutural (o que também elimina ciclos, já que ciclos só existem
   através de `ref`). Tipos mistos comparam `false`.
8. **Closures preservam o comportamento atual**: capturam *variáveis* (slots via
   upvalue); o aliasing de variável capturada é ortogonal ao CoW de objetos.

## 3. Mudanças de comportamento (breaking) e migração

Release alvo: **0.4.0**. Cada item entra no CHANGELOG com migração, no padrão da 0.3.0.

| Mudança | Código afetado | Migração |
|---|---|---|
| `let b = a` deixa de aliasar | código que muta via segundo nome | usar `ref` |
| leitura de contêiner deixa de aliasar (`let p = arr[0]; p.x = 1`) | mutação via alias local | mutar pelo caminho (`arr[0].x = 1`) ou `ref` |
| mutação aninhada via parâmetro deixa de vazar | código que dependia do vazamento | declarar o parâmetro `ref` |
| `spawn` deixa de encaminhar identidade | rotinas que compartilhavam arg mutável | canais ou `ref` a global |
| `==` de compostos vira estrutural | código que testava identidade | comparar via `ref` ou campos-chave |

## 4. Design de implementação

### 4.1 O bit `Shared`

`ObjArray`, `ObjMap` e `ObjInstance` ganham `Shared atomic.Bool` — **sticky**: liga e
não desliga (clones novos nascem desligados). Refcount fica explicitamente fora do
escopo (ver §8); o custo dos falsos positivos é uma cópia extra por episódio de
compartilhamento, mitigável depois sem mudança de semântica.

### 4.2 Regra de marcação (onde `Shared` liga) — *mark-on-store*

Uma operação marca `Shared` no composto quando **cria um segundo ponteiro vivo**:

- store em variável/global/upvalue (`OP_SET_LOCAL`, `OP_SET_GLOBAL`, define, upvalue store);
- store em contêiner (`OP_SET_INDEX`, `OP_SET_PROPERTY`) — marca o **valor armazenado**;
- argumento sem `ref` em chamada de closure (substitui `copyValue` em `calls.go:102`);
- captura de `defer` (`defer.go`), args de `spawn`/`spawn_task` (`task_execution.go`);
- deref de `ref` para contexto de valor (o `OP_COPY` de `compiler.go:1497` vira marcação);
- construção de instância (args viram campos);
- **natives SEM assinatura**: por padrão, todo composto passado é marcado
  (conservador). Natives comprovadamente só-leitura entram numa allowlist
  auditada que não marca (`length`, `to_str`, `fmt`, …) — `cow_natives.go`.
- **natives COM assinatura mantêm a cópia rasa ansiosa** (decisão refinada na
  implementação): o corpo em Go pode mutar o argumento sem passar pelo CoW do
  bytecode, então a cópia é a única proteção do chamador. Custo restrito a
  natives; closures/tasks/defer-closures usam marcação.

Leitura pura (`print(a[0])`, `a[0].x` em expressão) **não marca** — evita clones
espúrios em loops de leitura.

### 4.3 Regra de escrita (quando clona)

Toda mutação in-place checa `Shared`: ligado → clone raso primeiro (o atual
`copyValue` vira exatamente essa operação de clone), mutar o clone, e o clone
substitui o original **no slot que o continha**. Ao clonar, os filhos imediatos do
clone são marcados `Shared` (dois contêineres passam a apontá-los).

### 4.4 Primitiva universal: unicizar slot

`unicizeSlot(load, store) -> objeto único`: lê o slot; se o composto está `Shared`,
clona raso, grava o clone de volta no slot, devolve o clone. Slots existentes no VM:
locais (stack), globais (`GlobalEnvironment`), upvalues, `Elements[i]`, entradas de
map, `Fields[name]`, e **qualquer `ObjRef`** — `referenceStorage` (`references.go:64`)
já devolve exatamente o par (valor, setter) necessário.

### 4.5 Mutação por caminho (lowering no compilador)

`a[i].x = v` desce unicizando cada nível, de cima para baixo:

```
GET_MUT_LOCAL a          // uniciza slot da variável → array único no stack
GET_INDEX_MUT i          // uniciza a.Elements[i] (grava clone de volta) → struct único
SET_PROPERTY x           // escrita direta — alvo garantidamente único
```

Novos opcodes: `GET_MUT_LOCAL`, `GET_MUT_GLOBAL`, `GET_MUT_UPVALUE`, `GET_INDEX_MUT`,
`GET_PROP_MUT`, `GET_MUT_DEREF` (para lvalues através de `ref`). O compilador aplica o
lowering a **todo lvalue composto** (variável, caminho de índice, caminho de campo,
deref). Os `SET_*` finais assumem alvo único; um guard de arquitetura em teste
verifica que nenhum caminho de lvalue escapa do lowering.

### 4.6 Builtins mutantes

`append`, `pop`, `delete` já recebem o alvo como `ObjRef`
(`builtins_collections.go:101-150`, `builtin_calls.go`). Passam a unicizar via
`referenceStorage` antes de mutar. O item inserido é marcado `Shared` (§4.2).

### 4.7 Concorrência

`Shared` é `atomic.Bool` — marcação e checagem são thread-safe. Dois writers
clonam independentemente e cada um atualiza seu próprio slot (proteções existentes de
slot permanecem). Reader concorrente segura o ponteiro antigo e observa um valor
consistente (melhor que hoje, onde mutação é visível no meio da leitura).
`chan_send` marca o payload; receptor cai no CoW ao escrever → dados por canal ficam
livres de race por construção.

### 4.8 Igualdade estrutural

`valuesEqual` (`stack.go:15`) passa a: arrays/maps/instâncias → comparação estrutural
recursiva; `ObjRef` → identidade de slot (RefType + alvo); funções/canais/tasks →
identidade como hoje; `bytes`/`string` inalterados. Sem risco de ciclo (§2.7).

### 4.9 Contador de clones (observabilidade)

Contador atômico interno de clones CoW, exposto para testes: permite afirmar
"chamada só-leitura faz 0 clones" e vigiar regressões de marcação espúria.

## 5. Performance: modelo de custo e benchmarks

Modelo: hoje = O(n) por chamada, sempre; CoW = O(1) por vínculo + O(n) na primeira
escrita de valor compartilhado + 1 branch atômico por mutação.

### 5.1 Suite `benchmarks/`

Programas `.nx` determinísticos, ≥1s de execução, **imprimem checksum** ao final
(as duas versões devem produzir o mesmo resultado):

| Programa | Perfil | Expectativa |
|---|---|---|
| `bench_call_readonly.nx` | array grande passado a leitor puro em loop | **ganho grande** |
| `bench_call_ref.nx` | mutação in-place via `ref` | neutro |
| `bench_share_mutate.nx` | compartilha e muta em loop (pior caso) | regressão documentada |
| `bench_path_update.nx` | `a[i].x = …` em loop (custo do lowering) | ≤ ~5% |
| `bench_bubblesort.nx` | sort in-place 2000 elementos | ≤ ~5% |
| `bench_conway.nx` | grid mutado por gerações (determinístico) | ≤ ~5% |
| `bench_map_churn.nx` | escrita/leitura intensa de map | ≤ ~5% |
| `bench_spawn_sum.nx` | soma paralela via `spawn_task` | neutro/ganho |

### 5.2 Harness e registro

`benchmarks/run_benchmarks.ps1 -Binary <exe> -Label <nome>`: warmup + N execuções
cronometradas por programa, mediana, grava `benchmarks/results/<label>.md`.
Fluxo: rodar com o binário baseline (c429bd7) **antes** da implementação e com o
binário CoW ao final; `benchmarks/RESULTS.md` consolida a comparação e é commitado.

### 5.3 Critérios de aceitação

- Checksums idênticos entre baseline e CoW em toda a suite.
- `bench_call_readonly`: melhora mensurável (esperado ≥2x com array grande).
- Benchmarks de mutação in-place (`ref`, path, bubblesort, conway, map): regressão ≤ ~5%.
- `bench_share_mutate`: regressão livre, mas medida e documentada no RESULTS.md com a
  migração (`ref`).

## 6. Testes

1. **Suite de contrato (TDD, escrita antes da implementação)**: testes Go no VM + `.nx`
   end-to-end cobrindo cada regra do §2 — independência em atribuição/chamada/leitura/
   store/canal, `ref` compartilhando, `==` estrutural, closures inalteradas, spawn sem
   exceção de identidade, 0 clones em chamada só-leitura (via contador §4.9).
2. **Testes existentes que codificam a semântica antiga** (ex.:
   `TestCollectionRuntimeMetadataPreservesShallowCopyAndIdentity`, shallow-copy em
   `defer_test.go`, `reference_ownership_test.go`) são atualizados deliberadamente,
   um a um, com justificativa no commit.
3. **Corpus**: rodar `noxy_examples/*.nx` (respeitando exclusões de demos interativos)
   e comparar saída baseline × CoW; divergências viram itens de migração documentados.
4. **Guard de arquitetura**: nenhum lvalue composto compila sem o lowering §4.5.

## 7. Documentação

- `docs/NOXY_LANGUAGE_SPEC.md`: §2 reescrito (some "shallow copy", entra o contrato §2).
- `docs/REF_SEMANTICS.md`: atualizado (ref = único compartilhamento).
- `CHANGELOG.md`: seção 0.4.0 breaking no padrão da 0.3.0 (§3).
- `docs/CONCURRENCY.md`: canais transportam valores; exceção do spawn removida.

## 8. Fora de escopo

- Refcount no lugar do bit sticky (otimização futura, sem mudança de semântica).
- Estruturas persistentes/structural sharing.
- Tipos imutáveis/freeze.
- `copy()`/`deep_copy()` builtins — desnecessários sob semântica de valor.

## 9. Riscos

| Risco | Mitigação |
|---|---|
| Ref para dentro de contêiner (`ref a[0]`, campo) fixa a identidade do contêiner | a base do ref é **unicizada na criação** (cadeia MUT em `compileLValueBase`); anomalia residual: se o contêiner for compartilhado DEPOIS da criação do ref, a escrita através do ref é visível pela cópia ainda não materializada — documentado como limitação (mesma classe de aresta do ref pré-CoW) |
| Lvalue fora do lowering → vazamento de mutação | guard de arquitetura + suite de contrato por forma de lvalue |
| Native retentor fora da marcação → aliasing oculto | default conservador (marca tudo), allowlist auditada só-leitura |
| Marcação espúria → clones em loop (O(n²) acidental) | mark-on-store (não on-read) + contador de clones em benchs |
| Stdlib `.nx` dependendo da semântica antiga | corpus test (§6.3) roda a stdlib embarcada inteira |
| `==` estrutural mudar comportamento de código existente | listado como breaking com migração (§3) |
