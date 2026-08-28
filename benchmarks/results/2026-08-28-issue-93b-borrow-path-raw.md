# Dados brutos — custo por nível do empréstimo aninhado (issue #93, parte b): base × head

**Data:** 2026-08-28 · Windows 11 · Intel Core 7 150U · pwsh 7.6.5 · laptop, na
tomada. Sessão mais carregada que a do #96 (Creative Cloud/Dropbox em fundo;
`bench_bubblesort` 660 → 740 ms entre as duas rodadas) — só o delta intra-rodada
é comparável. Runner e `compare_examples.ps1` rodaram ANTES do A/B.

**Binários** (scratchpad):

| rótulo | commit | o que é |
|---|---|---|
| `base` | a52c7e1 | topo de `feature/issue-queue-2026-08-28` antes do #93b (= develop a855077 + #87 + #93a + #96 + #53-4) |
| `head` | 4026c43 | + `ObjRef.Slot` resolvido em `OP_REF_PROPERTY`, `fieldSlotOf` em `descend`/`referenceStorageMode`/setter, `validateReferencedValue` sem `reflect` no caso comum, `derefPlace` só com `VAL_REF` |

Spec: `docs/superpowers/specs/2026-08-28-vm-perf-issue-93b-borrow-path-design.md`.

**Protocolo:** `interleaved_compare.ps1 -Runs 9` (`pwsh -NoProfile -File`),
mediana de 9. Benches novos neste commit: `bench_bst_owned.nx` /
`bench_bst_ref.nx` (as fixtures da issue, 20k chaves, `CHECKSUM:`);
`bench_borrow_path.nx` ganhou `CHECKSUM:` (era pulado pelo guard).

## 1. Headline — base × head (mediana de 9)

| bench | base_a52c7e1_ms | head_4026c43_ms | delta |
|---|---|---|---|
| bench_borrow_path.nx | 564.7 | 507.7 | **-10.1%** |
| bench_bst_owned.nx | 780.9 | 565.8 | **-27.5%** |
| bench_bst_ref.nx | 216.7 | 217.9 | 0.6% |
| bench_bubblesort.nx | 740 | 744.5 | 0.6% |
| bench_call_light.nx | 32.3 | 28.8 | -10.8% |
| bench_call_readonly.nx | 572.5 | 546.5 | -4.5% |
| bench_call_ref.nx | 1109.4 | 1091.6 | -1.6% |
| bench_conway.nx | 1264.3 | 1283.4 | 1.5% |
| bench_generic_vs_hand.nx | 431.6 | 432.9 | 0.3% |
| bench_map_churn.nx | 212.3 | 207 | -2.5% |
| bench_path_update.nx | 162 | 163.1 | 0.7% |
| bench_share_mutate.nx | 115.4 | 107.4 | -6.9% |
| bench_spawn_sum.nx | 400.7 | 394.4 | -1.6% |
| bench_struct_records.nx | 127.4 | 126 | -1.1% |
| bench_typed_call_map.nx | 28.2 | 27 | -4.3% |
| bench_value_call_mutate.nx | 28.4 | 27 | -4.9% |

Pulado: `bench_hash31_bytes.nx` (não imprime `CHECKSUM:`; pré-existente).

## 2. Fixtures da issue (uma execução cada, máquina carregada — só a razão vale)

| fixture | base | head |
|---|---|---|
| `p_owned` 50k aleatórias | 6,36 s | 3,60 s (×1,77) |
| `p_owned_sorted` 1000 ordenadas | 21,2 s | 11,1 s (×1,9) |

Com a máquina livre, `p_owned` 50k: 2,20 s (base, perfil do #96) → **1,60 s** (head).

## 3. Perfil — `p_owned` 50k (`--cpuprofile`, uma execução)

**Base** (binário do #96, mesmo caminho do empréstimo) — 2,20 s, 2,74 s de amostras:

| símbolo | flat | cum |
|---|---|---|
| `borrowContainer` | 10,2 % | 53,7 % |
| `referenceStorageMode` | 3,3 % | 59,5 % |
| `descend` | 12,4 % | 32,5 % |
| `derefPlace` | 6,9 % | 6,9 % |
| `(*ObjInstance).Get` → `FieldIndex` → `mapaccess2_faststr` + `aeshashbody` | — | **16,4 %** |
| GC (`scanSpan`, `tryDeferToSpanScan`, `scanObjectsSmall`) | — | ~25 % |

**Head 4026c43** — 1,60 s, 1,95 s de amostras:

| símbolo | flat | cum |
|---|---|---|
| `borrowContainer` | 12,8 % | 41,0 % |
| `referenceStorageMode` | 6,2 % | 48,2 % |
| `descend` | 5,1 % | 12,8 % |
| `fieldSlotOf` (guarda `Fields[Slot] == Name`) | 6,2 % | 11,3 % |
| ↳ `memeqbody` | 4,6 % | 4,6 % |
| `sync/atomic.(*Int32).Add` (Retain/Release na caminhada de escrita) | 7,7 % | 7,7 % |
| `FieldIndex` / `mapaccess2_faststr` / `validateReferencedValue` | 0,5 % cada | ~1 % |
| GC | — | ~20 % |

Leitura: o hashing por nível saiu (16 % → 1 %) e o `reflect` do `defer` também;
o que sobra é a caminhada em si (O(profundidade) por acesso, modelo de lugar da
#83) — `borrowContainer` + `descend` + `referenceStorageMode` ainda são ~50 %
do tempo, agora dominados pela estrutura do laço e pelo RC da unicização, não
por lookup. É o custo que só a cache por época (spec §6) remove.

## 4. Verificação

- `go test ./...` verde; `go test -race ./internal/vm -count=1` verde (24 s).
- Corpus `run_all_tests_concurrent.nx` com o head: **180/180**.
- `compare_examples.ps1` base × head: **149 iguais, 0 divergentes, 70 excluídos**.
- Testes novos: `ObjRef` com `Slot` errado/fora da faixa resolve pelo nome e
  erra igual em campo inexistente; `ref xs[0].valor` em instância JSON
  reordenada escreve o campo certo; BST profunda com cópia CoW dá o mesmo
  resultado. `TestMalformedReferenceRejectsTaskPayload` pegou a primeira versão
  do fast path de `validateReferencedValue` (aceitava `VAL_TASK` com payload
  string) — o switch ficou restrito a `VAL_OBJ`/`VAL_REF`.
