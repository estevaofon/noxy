# Dados brutos — issue #66, item 4 (+ #40 item 1): maps e structs

**Data:** 2026-08-22 · **Máquina:** Windows 11 · Intel Core 7 150U (mesma dos
itens 2 e 3) · Python 3.14.7 · Lua 5.4.6 · Go 1.26.6 · pwsh 7.6.5. Binários em
disco local (scratchpad), compilados na mesma sessão:

- `noxy_base4.exe` = 4ac27f5 (v0.15.2 = head do item 3, PR #69 — a base desta rodada; branch empilhada)
- `noxy4_s0.exe` = cbf74c8 (cache do schema do construtor + `NewInstance`/clone pré-dimensionados)
- `noxy4_s1.exe` = 410d170 = head (+ maps: `Swap` sob um lock, chave sem re-boxing, `Len` sem `Snapshot`)

Máquina sem `go test`/build durante as medições; CPU média no início de cada
passo. **4d** (construtor via `OP_CALL_STATIC` sem validação por argumento)
**não entrou**: o perfil de s0/s1 já não mostra `validateStructConstructorArguments`
no topo (critério da spec §3.4).

## 1. Verificação

- `go test ./...` verde (12 pacotes); `go test -race ./internal/value ./internal/vm` verde (1,9 s / 30,0 s)
- corpus `run_all_tests_concurrent.nx`: **177/177**; `compare_examples.ps1` base × head: **146 iguais, 0 divergentes, 70 excluídos**
- guards de inline inalterados; nenhum opcode novo; `run()` intocado
- testes novos: `struct_ctor_cache_test.go` (mensagem e aridade de construtor mal-tipado iguais com cache frio × quente), `map_fastpath_test.go` (RC do valor velho via `OwnersCount`: 1 → 2 → 1 → 2; `length` após `delete`; `has_key`; chave int/string)

## 2. `interleaved_compare.ps1 -Runs 9` — base4 × head (CPU 15 % no início)

| bench | v0152_ms | maps_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 685.1 | 687.2 | 0.3% |
| bench_call_light.nx | 19.3 | 18.6 | -3.6% |
| bench_call_readonly.nx | 540.8 | 543.9 | 0.6% |
| bench_call_ref.nx | 1152.1 | 1152.9 | 0.1% |
| bench_conway.nx | 1285.7 | 1291.4 | 0.4% |
| bench_generic_vs_hand.nx | 455.1 | 452.3 | -0.6% |
| bench_map_churn.nx | 204.8 | 184.8 | -9.8% |
| bench_path_update.nx | 236.6 | 234.6 | -0.8% |
| bench_share_mutate.nx | 95.8 | 98.9 | 3.2% |
| bench_spawn_sum.nx | 361 | 359.8 | -0.3% |
| bench_struct_records.nx | 146.3 | 77.5 | **-47%** |
| bench_typed_call_map.nx | 22.6 | 22 | -2.7% |
| bench_value_call_mutate.nx | 20.8 | 20.7 | -0.5% |

`bench_struct_records.nx` é novo neste PR (pedido no Test Plan da #40).

## 3. A/B por estágio — três binários intercalados, 11 execuções, tempo de parede (piso ≈ 9,5 ms)

| bench | base4 | s0 | s1 (head) | s0 vs base | s1 vs s0 | **head vs base** |
|---|---|---|---|---|---|---|
| `benchmarks/bench_struct_records.nx` (CPU 18 %) | 145,4 (133,4) | 75,2 (72,2) | **75,8 (69,4)** | **−48,3 %** | +0,8 % | **−47,9 %** (líquido −51 %) |
| `cross_runtime/map_churn.nx` (CPU 23 %) | 135,8 (129,5) | 132,7 (127,8) | **121,7 (117,3)** | −2,3 % | −8,3 % | **−10,4 %** (líquido −11,6 %) |
| `benchmarks/bench_map_churn.nx` (CPU 11 %) | 204,7 (197,2) | 205,1 (194,8) | **191,2 (180,3)** | +0,2 % | −6,8 % | **−6,6 %** |
| `benchmarks/bench_typed_call_map.nx` (CPU 18 %) | 21,8 (18,7) | 21,9 (19,6) | 21,4 (19,6) | — | — | piso |

## 4. `run_cross_runtime.ps1 -NoxyBaseline` — mínimo de 9, intercalado (CPU 17 % no início)

Líquido (descontado `startup`: 9,4 / 9,6 ms) e razões:

| bench | head | v0152 | python | head ÷ v0152 | head ÷ python | v0152 ÷ python |
|---|---|---|---|---|---|---|
| `map_churn` | **103,6** | 118,0 | 58,3 | **0,88x** | **1,78x** | 2,02x |
| `loop_arith` | 190,7 | 200,5 | 182,8 | 0,95x | 1,04x | 1,10x |
| `string_ops` | 67,4 | 69,2 | 31,8 | 0,97x | 2,12x | 2,18x |
| `mandelbrot` | 132,9 | 134,5 | 73,8 | 0,99x | 1,80x | 1,82x |
| `fib` | 109,6 | 109,1 | 89,4 | 1,00x | 1,23x | 1,22x |
| `bubblesort` | 107,7 | 103,9 | 65,8 | 1,04x | 1,64x | 1,58x |

`loop_arith`/`bubblesort` ±5 % são ruído (não tocam map nem struct). Tabela
completa em `cross_runtime/results/cross_runtime.md`.

## 5. Perfis (`noxy --cpuprofile`, amostras de 10 ms)

**`struct_records` 10x (600 k construções), base4** — 1,41 s, 1,78 s de amostras:

| símbolo | cum |
|---|---|
| `callValueStatic` → `callValue` | 48,9 % |
| `validateStructConstructorArguments` → `validStructConstructorType` → `runtimeTypeComplete` | **35,4 / 30,3 / 29,8 %** (`mapassign_fast64ptr` 22,5 %, `growToTable` 14,6 % — o `make(map[*RuntimeTypeInfo]bool)` por construção) |
| `callPreparedValue` (`NewInstance` + 5 `mapassign_faststr`) | 11,8 % (`growToSmall`/`newTable` 7 %) |
| GC (`gcBgMarkWorker`…) | ~14 % |
| `(*VM).run` flat | 8,4 % |

**head (s1)** — 0,65 s, 750 ms de amostras: `callValueStatic` 40 % cum →
`callPreparedValue` 33 % (`NewInstance` 10,7 %, `mapassign_faststr` 14,7 %,
`makemap` 6,7 %); `validateStructConstructorArguments` **fora do topo**;
`mapaccess2_faststr` (leitura de campo) 6,7 %; GC ~15 %; `run` 17 % flat. O que
resta é o próprio `map[string]Value` da instância (alocar + 5 escritas por
construção) — é o "campos por índice" da #40 item 3, fora deste PR.

**`map_churn` 10x, base4** — 1,18 s: `setIndexGeneric` 28,8 % (`Get`+`Set`),
`getIndexGeneric`/`ObjMap.Get` 14,4 / 15,3 %, `atomic.Int32.Add` 8,5 %,
`nilinterhash`+`nilinterequal` ~9 %, `convTstring` 5 %, `callNative` (`to_str`)
13,6 %, `concatstrings` 5 %.

**head (s1)** — 1,08 s: `setIndexGeneric` 23,7 % (`Swap` 17,8 %),
`atomic.Int32.Add` 9,3 %, `convTstring`/`nilinterhash` **fora do topo**
(`efaceeq` 3,4 %), `callNative` 17,8 %. O que resta: RWMutex + `gen` por
operação (~12 %), `to_str`+concat (~20 %), chave `interface{}` (store tipado
só com número).

## 6. Carga e condições

| passo | CPU no início |
|---|---|
| A/B estágios | 18 / 23 / 18 / 11 % |
| `interleaved_compare` | 15 % |
| `cross_runtime` | 17 % |

Firefox e o Claude Code abertos; nenhum `go test`/build concorrente.
