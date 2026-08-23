# Dados brutos — issue #66, item 2: strings (fast path ASCII + `to_str(int)`)

**Data:** 2026-08-22 · **Máquina:** Windows 11 · Intel Core 7 150U (máquina
diferente das seções anteriores de `RESULTS.md`, que rodaram num i7-1165G7 — só
o delta intra-sessão é comparável) · Python 3.14.7 · Go 1.26.6 · Lua ausente ·
Windows PowerShell 5.1 via cópias BOM dos scripts (pwsh 7 não instalado).
Binários em disco local (scratchpad), os três compilados na mesma sessão:

- `noxy_base.exe` = `develop` 73cf11a (v0.15.0)
- `noxy_s1.exe` = b0699e6 (Tasks 1–3: `isASCII`, `substring`/`char_at`, `s[i]`/`slice` — **sem** `to_str`)
- `noxy_str.exe` = 0bc9e5d (head: + `Value.String()` de int via `strconv` e `to_str` de escalar sem validação)

Máquina sem `go test`/build durante as medições; CPU média reportada no início
de cada passo (`Win32_Processor.LoadPercentage`).

## 1. Verificação

- `go test ./...` verde (12 pacotes); `go test -race ./internal/value ./internal/vm` verde (2,4 s / 33,8 s)
- corpus `noxy_examples/run_all_tests_concurrent.nx`: **177/177**
- `compare_examples.ps1` base × head: **146 iguais, 0 divergentes, 70 excluídos**
- guards de inline: `push` 20, `pop` 18, `Retain` 67, `Release` 80, `NeverTracked`, `arrayTagIsRefSlot` 20, `ensureCallCapacity` 80 — inalterados; novo `isASCII` custo **23** (≤ 80; não é chamada de `run()`)
- nenhum opcode novo; `run()` intocado

## 2. `interleaved_compare.ps1 -Runs 9` — base × head (CPU 26 % no início)

| bench | v0150_ms | str_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 862.7 | 878.8 | 1.9% |
| bench_call_light.nx | 20.3 | 19.8 | -2.5% |
| bench_call_readonly.nx | 587.7 | 591.6 | 0.7% |
| bench_call_ref.nx | 1252.2 | 1240.7 | -0.9% |
| bench_conway.nx | 1348.9 | 1330.3 | -1.4% |
| bench_generic_vs_hand.nx | 474.4 | 474.8 | 0.1% |
| bench_map_churn.nx | 230.1 | 203.6 | -11.5% |
| bench_path_update.nx | 239.1 | 244.6 | 2.3% |
| bench_share_mutate.nx | 144.4 | 146.5 | 1.5% |
| bench_spawn_sum.nx | 431.5 | 419.4 | -2.8% |
| bench_typed_call_map.nx | 22.4 | 21.5 | -4% |
| bench_value_call_mutate.nx | 20.2 | 20.1 | -0.5% |

Piso de processo nesta máquina ≈ 11 ms (`startup` do cross-runtime), então os
benches de ~20 ms são ~10 ms de trabalho — não decidem nada.

## 3. A/B focado por estágio — três binários intercalados, 11 execuções, tempo de parede

`ab_string_ops.ps1` (scratchpad): warmup + 11 rodadas `base, s1, str` na mesma
janela; mediana e mínimo.

| bench | base mediana (mín) | s1 mediana (mín) | str mediana (mín) | s1 vs base | str vs base |
|---|---|---|---|---|---|
| `cross_runtime/string_ops.nx` (CPU 17 %) | 120,5 (112,4) | 109,7 (102,8) | **97,1 (92,3)** | −9,0 % | **−19,4 %** |
| `cross_runtime/map_churn.nx` (CPU 19 %) | 151,8 (144,1) | 150,6 (145,0) | **133,8 (130,0)** | −0,8 % | **−11,9 %** |

Descontando o piso de 11 ms: `string_ops` 109,5 → 98,7 → 86,1 ms líquidos
(**−10 % só ASCII, −21 % total**); `map_churn` 140,8 → 122,8 (−12,8 %, todo do
estágio `to_str`: `f"k{i % 500}"` interpola um int por iteração via
`Value.String()`).

## 4. `run_cross_runtime.ps1 -NoxyBaseline` — mínimo de 9, intercalado (CPU 2 % no início)

Tempo total (ms):

| bench | noxy (head) | v0150 | python | go |
|---|---|---|---|---|
| `bubblesort` | 140,0 | 137,1 | 91,8 | - |
| `fib` | 208,5 | 213,5 | 118,5 | 10,4 |
| `loop_arith` | 209,7 | 213,4 | 212,0 | 15,0 |
| `mandelbrot` | 149,0 | 146,7 | 96,5 | - |
| `map_churn` | 126,0 | 143,0 | 78,6 | - |
| `startup` | 11,0 | 10,7 | 20,6 | 7,2 |
| `string_ops` | 85,2 | 106,9 | 51,3 | - |

Líquido (descontado `startup`) e razões:

| bench | head | v0150 | python | head ÷ v0150 | head ÷ python | v0150 ÷ python |
|---|---|---|---|---|---|---|
| `bubblesort` | 129,0 | 126,4 | 71,2 | 1,02x | 1,81x | 1,78x |
| `fib` | 197,5 | 202,8 | 97,9 | 0,97x | 2,02x | 2,07x |
| `loop_arith` | 198,7 | 202,7 | 191,4 | 0,98x | 1,04x | 1,06x |
| `mandelbrot` | 138,0 | 136,0 | 75,9 | 1,01x | 1,82x | 1,79x |
| `map_churn` | 115,0 | 132,3 | 58,0 | **0,87x** | **1,98x** | 2,28x |
| `string_ops` | 74,2 | 96,2 | 30,7 | **0,77x** | **2,42x** | 3,13x |

Tabela completa gerada em `cross_runtime/results/cross_runtime.md`. As razões
÷ python desta máquina (Python 3.14.7) não são comparáveis às da seção do item
1 (Python 3.13.1, i7-1165G7): lá `string_ops` dava 3,33x e aqui a base dá
3,13x — a coluna válida é **head ÷ v0150**.

## 5. Perfis (`noxy --cpuprofile`, `string_ops.nx` com 2 000 000 iterações, amostras de 10 ms, mesma janela)

**Base (73cf11a)** — 988 ms, 940 ms de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 20,2 % | 95,7 % |
| `(*VM).callValue` / `callNative` / `ObjNative.Invoke` | 3,2 / 6,4 / 3,2 % | 52,1 / 48,9 / 40,4 % |
| `to_str` (`defineCoreBuiltins.func5`) | 4,3 % | **22,3 %** |
| `Value.String` (→ `fmt.Sprintf("%d")`, `pp.doPrintf` 6,4 %, `fmtInteger` 5,3 %) | 2,1 % | 13,8 % |
| `strings_substring` (`defineStringBuiltins.func17`) | 6,4 % | **13,8 %** |
| `runtime.mallocgc` | 2,1 % | 18,1 % |
| `runtime.stringtoslicerune` | 2,1 % | 2,1 % |
| `push` / `pop` | 5,3 / 4,3 % | |

**Head (0bc9e5d)** — 862 ms, 940 ms de amostras:

| símbolo | flat | cum |
|---|---|---|
| `(*VM).run` | 23,4 % | 83,0 % |
| `(*VM).callValue` / `callNative` / `ObjNative.Invoke` | 2,1 / 3,2 / 3,2 % | 29,8 / 26,6 / 21,3 % |
| `to_str` (`func5`) | 0 | **8,5 %** |
| `Value.String` (→ `strconv.formatBase10` 5,3 %) | 2,1 % | 7,5 % |
| `strings_substring` (`func17`; `isASCII` 1,1 %, `clampSubstringRange` 1,1 %) | 4,3 % | **7,5 %** |
| `runtime.convTstring` (boxing de string em `NewString`) | 1,1 % | 5,3 % |
| `value.Release` | 5,3 % | 5,3 % |
| `stringtoslicerune` / `fmt.*` | — | ausentes |

Leitura: `to_str` caiu de 22 % para 8,5 % e `substring` de 14 % para 7,5 %;
`[]rune` e `fmt` sumiram do perfil. O que resta no caminho de string: o box
da string no `Value` (`convTstring` — etapa 2 do item 2), o protocolo de
chamada de builtin (`callNative` 27 %, com o wrapper Noxy de `substring` em
`stdlib/strings.nx` custando um frame inteiro — item 3) e `formatBase10` (5 %,
irredutível sem cache).

## 6. Carga e condições

| passo | CPU no início |
|---|---|
| A/B `string_ops` | 17 % |
| `interleaved_compare` | 26 % |
| `cross_runtime` | 2 % |
| A/B `map_churn` | 19 % |

Firefox e o Claude Code abertos; nenhum `go test`/build concorrente.
