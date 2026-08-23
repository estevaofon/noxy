# Dados brutos — issue #66, item 3: protocolo de chamada

**Data:** 2026-08-22 · **Máquina:** Windows 11 · Intel Core 7 150U (mesma do
item 2; diferente das seções de item 1/fase 2, que rodaram num i7-1165G7 — só
o delta intra-sessão é comparável) · Python 3.14.7 · Lua 5.4.6 · Go 1.26.6 ·
pwsh 7.6.5 (scripts de `benchmarks/` direto). Binários em disco local
(scratchpad), os quatro compilados na mesma sessão:

- `noxy_base.exe` = `develop` c1cc12a (v0.15.1)
- `noxy_s0.exe` = a4e23e6 (Tasks 1–2: `ParamsUntracked` + `OP_RETURN` fast path / `popSimpleFrame`)
- `noxy_s1.exe` = 307b276 + 868d435 (s0 + `OP_CALL_STATIC` fast path **com** o fix de propagação do flag — ver §0)
- `noxy_call.exe` = 868d435 (head: + superinstruções)

Máquina sem `go test`/build durante as medições; CPU média no início de cada
passo (`Win32_Processor.LoadPercentage`).

## 0. Achado da rodada (custou um commit a mais)

A primeira medição por estágio deu **s1 ≈ s0** (`fib` 166 × 170 ms) apesar de
o perfil atribuir 12 % ao lado da chamada. O perfil de s1 mostrou
`callValueStatic`/`callPreparedClosure`/`ownSlot` **ainda presentes**: o fast
path nunca era tomado. Causa: `OP_CLOSURE`, `OP_CONSTANT` e `OP_CONSTANT_LONG`
copiam o `ObjFunction` campo a campo ao vincular ao ambiente e **perderam
`ParamsUntracked`**. Nenhum teste funcional pega isso (o caminho lento é
correto). Fix em 868d435 + `TestClosureKeepsParamsUntracked`, que pergunta ao
valor da closure. O `noxy_s1.exe` desta tabela é s1 **com** o fix
(cherry-pick em worktree destacado).

## 1. Verificação

- `go test ./...` verde (12 pacotes); `go test -race ./internal/value ./internal/vm` verde (2,4 s / 28,9 s)
- corpus `noxy_examples/run_all_tests_concurrent.nx`: **177/177**
- `compare_examples.ps1` base × head: **146 iguais, 0 divergentes, 70 excluídos**
- guards de inline inalterados (`push` 20, `pop` 18, `Retain` 67, `Release` 80, `NeverTracked`, `arrayTagIsRefSlot` 20, `ensureCallCapacity` 80, `isASCII` 23); `popSimpleFrame` custa 60 (chamada real, de propósito — uma, sem `frameOutcome`); guard de arquitetura `TestUnwindArchitectureCentralizesTerminalFrameTeardown` verde (teardown terminal só em `unwind.go`)
- dois opcodes novos por append (`OP_GET_LOCAL_ADD_IMM_INT`, `OP_GET_LOCAL_2`)

## 2. `interleaved_compare.ps1 -Runs 9` — base × head (CPU 17 % no início)

| bench | v0151_ms | call_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 776.9 | 694.1 | -10.7% |
| bench_call_light.nx | 19.9 | 19.7 | -1% |
| bench_call_readonly.nx | 531.3 | 537.4 | 1.1% |
| bench_call_ref.nx | 1137.1 | 1149.9 | 1.1% |
| bench_conway.nx | 1253.2 | 1275.3 | 1.8% |
| bench_generic_vs_hand.nx | 456.6 | 448.7 | -1.7% |
| bench_map_churn.nx | 194.3 | 200.1 | 3% |
| bench_path_update.nx | 229.8 | 231.8 | 0.9% |
| bench_share_mutate.nx | 100.6 | 103.3 | 2.7% |
| bench_spawn_sum.nx | 398.8 | 369.8 | -7.3% |
| bench_typed_call_map.nx | 22.3 | 22.5 | 0.9% |
| bench_value_call_mutate.nx | 21.1 | 21.2 | 0.5% |

`bench_call_readonly`/`bench_call_ref` chamam com parâmetro `int[]`/`ref int[]`
(`ParamsUntracked` false → caminho lento de chamada) e o laço não é `local OP
local` — ficam em ±1 %, como esperado.

## 3. A/B por estágio — quatro binários intercalados, 11 execuções, tempo de parede (piso ≈ 10 ms)

`ab_stages.ps1` (scratchpad): warmup + 11 rodadas `base, s0, s1, call` na
mesma janela; mediana (mínimo).

| bench | base | s0 | s1 | call (head) | s0 vs base | s1 vs s0 | head vs s1 | **head vs base** |
|---|---|---|---|---|---|---|---|---|
| `cross_runtime/fib.nx` (CPU 15 %) | 203,4 (196,1) | 161,1 (156,4) | 149,5 (144,1) | **122,0 (117,2)** | −20,8 % | −7,2 % | −18,4 % | **−40,0 %** |
| `cross_runtime/bubblesort.nx` (CPU 7 %) | 133,3 (127,8) | 132,7 (127,0) | 133,8 (127,8) | **118,9 (114,2)** | −0,5 % | +0,8 % | −11,1 % | **−10,8 %** |
| `cross_runtime/loop_arith.nx` (CPU 0 %) | 213,3 (209,6) | 214,5 (210,8) | 220,5 (216,3) | 216,2 (212,0) | +0,6 % | +2,8 % | −2,0 % | +1,4 % |
| `cross_runtime/mandelbrot.nx`¹ (CPU 34 %) | 149,5 (143,8) | 154,9 (147,7) | 152,7 (149,6) | 150,8 (145,1) | +3,6 % | −1,4 % | −1,2 % | +0,9 % |

¹ rodada anterior ao fix do flag (s1 sem o fix); `mandelbrot` não chama função
no laço nem tem `local OP local` int, então os quatro binários são
equivalentes ali — a coluna vale como ruído da máquina (±3 %).

Líquido (piso 10 ms): `fib` 193,4 → 112,0 ms (**−42 %**).

## 4. `run_cross_runtime.ps1 -NoxyBaseline` — mínimo de 9, intercalado (CPU 21 % no início)

Líquido (descontado `startup`: 9,6 / 9,3 ms) e razões:

| bench | head | v0151 | python | lua | head ÷ v0151 | head ÷ python | v0151 ÷ python |
|---|---|---|---|---|---|---|---|
| `fib` | **105,4** | 185,1 | 90,7 | 42,0 | **0,57x** | **1,16x** | 2,04x |
| `bubblesort` | **106,8** | 117,1 | 68,9 | – | **0,91x** | **1,55x** | 1,70x |
| `string_ops` | 69,0 | 73,7 | 31,8 | – | 0,94x | 2,17x | 2,32x |
| `loop_arith` | 194,7 | 193,7 | 183,6 | 38,8 | 1,01x | 1,06x | 1,06x |
| `mandelbrot` | 133,5 | 132,9 | 74,3 | – | 1,00x | 1,80x | 1,79x |
| `map_churn` | 117,5 | 112,3 | 56,9 | – | 1,05x | 2,07x | 1,97x |

Tabela completa em `cross_runtime/results/cross_runtime.md`. `fib` ÷ lua: 2,5x
(era 4,4x).

## 5. `BenchmarkNoxyCallOverhead` (`go test -bench -count 8`, worktree destacado para a base)

| | ns/op (8 rodadas) | mediana | B/op | allocs/op |
|---|---|---|---|---|
| base c1cc12a | 72242 · 73271 · 73627 · 73985 · 74182 · 73897 · 77855 · 73132 | **73 762** | 560 | 10 |
| head 868d435 | 52976 · 51756 · 53064 · 53053 · 53087 · 53176 · 53422 · 53174 | **53 076** | 560 | 10 |

**−28,0 %** (meta da issue ≥ −25 % ✅). `leaf(i)` tem parâmetro `int` → fast path
de chamada e de retorno; `acc = leaf(i)`/`i = i + 1` não usam as superinstruções.

## 6. Perfis (`noxy --cpuprofile`, `fib(34)`, amostras de 10 ms, mesma janela)

**Base c1cc12a** — 1,32 s, 1280 ms de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 41,4 % | 96,9 % |
| `push` / `pop` | 13,3 / 3,9 % | |
| `finishFrame` + `finalizeCurrentFrame` | 12,5 + 7,8 % | **24,2 %** |
| `callValueStatic` → `callPreparedClosure` → `ownSlot` | 3,9 / 2,3 / 2,3 % | **12,5 %** |

**s0 a4e23e6** (retorno) — 1,08 s: `run` 46 % flat; `popSimpleFrame` 2,9 %;
`callValueStatic` 22 % cum (`ownSlot` 11,5 %, `callPreparedClosure` 17 %) —
o retorno sumiu, a chamada ficou inteira.

**s1 sem o fix** — 1,13 s: `callPreparedClosure` 9,9 %, `ownSlot` 4,5 %,
`callValueStatic` 10,8 % cum **ainda lá** (ver §0).

**Head 868d435** — 0,80 s, 780 ms de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 60,3 % | 94,9 % |
| `push` / `pop` | 10,3 / 6,4 % | |
| `popSimpleFrame` | 6,4 % | 6,4 % |
| `GlobalCache` + `Generation` + `atomic.Load` | 3,8 + 2,6 + 3,8 % | **~12 %** |
| `finishFrame` / `finalizeCurrentFrame` / `callValueStatic` / `callPreparedClosure` / `ownSlot` | — | ausentes |

Leitura: o protocolo de chamada/retorno saiu do perfil (36 % → 6,4 % de
`popSimpleFrame`). O que resta em `fib`: despacho puro (`run` 60 % flat, 11
opcodes por chamada não-folha), `push`/`pop` 17 % e — agora visível — o
lookup cacheado de `fib` (`OP_GET_GLOBAL`: `Generation()` atômico + compare,
~12 %). Esse último é o "(a) literal" da issue, que ficou de fora deste PR por
ser 1,6 % no perfil de base; com o resto cortado virou o próximo candidato.

## 7. Carga e condições

| passo | CPU no início |
|---|---|
| A/B estágios (fib / bubblesort / loop_arith) | 15 / 7 / 0 % |
| `interleaved_compare` | 17 % |
| `cross_runtime` | 21 % |
| `go test -bench` | — (único processo) |

Firefox e o Claude Code abertos; nenhum `go test`/build concorrente.
