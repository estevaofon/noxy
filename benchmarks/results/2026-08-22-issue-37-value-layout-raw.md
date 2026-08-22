# Dados brutos — fase 2 de perf (issue #37): v0.14.2 × estágios 1 / 1+2 / 1+2+pop

**Data:** 2026-08-22 · Windows 11 · i7-1165G7 (8 threads lógicas) · Go 1.24.11 ·
laptop, plano Equilibrado, na tomada.

**Binários** (todos em disco local, fora do OneDrive; mesma sessão de build):

| rótulo | commit | o que é |
|---|---|---|
| `base` (v0142) | cb8efcb | `develop` = v0.14.2 |
| `s1` | 41638e8 | estágio 1: `Value` 48 → 32 B, acessores |
| `s1+2` | e4d5a5f | + estágio 2: `ObjHeader` no offset 0, dica `kind` em `ownersOf` |
| `s1+2+pop` | ba7f85d | + `pop()` inlinada em `run()` (custo 22 → 18, 0 → 79 sites) |

**Protocolo:** o de `benchmarks/RESULTS.md` — execuções intercaladas na mesma
janela de tempo, guard de `CHECKSUM:` entre binários, mediana de 9 (mínimo
também registrado). A tabela "headline" é o script do repo
(`interleaved_compare.ps1`, 2 binários); as tabelas por estágio são uma
generalização do mesmo protocolo para 4 binários (`interleave4.ps1`, reproduzido
no fim deste arquivo). Fontes `.nx` copiados para disco local antes de medir
(OneDrive infla ~2x).

**Carga da máquina:** registrada no início de cada passo (`Win32_Processor.LoadPercentage`),
ver linhas `[passo] inicio ... CPU load`. Firefox, Slack e o Claude Code abertos;
sem Zoom/Chrome; nenhum `go test` em andamento durante as medições.

Os benches curtos (`bench_call_light`, `bench_typed_call_map`,
`bench_value_call_mutate`, ~100 ms com piso de processo ~84 ms) **não decidem
nada** (RESULTS.md, 2026-08-22): um jitter de 5 ms é ±5 %. São listados, não
interpretados.

## 1. Headline — `interleaved_compare.ps1` base × s1+2+pop (mediana de 9)

`[headline] inicio 16:24:22 | CPU load: 1 %` → `fim 16:28:55`

| bench | v0142_ms | s12p_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 4046.3 | 3002.8 | -25.8% |
| bench_call_light.nx | 120.3 | 110.7 | -8% |
| bench_call_readonly.nx | 1386.8 | 929.4 | -33% |
| bench_call_ref.nx | 4160.8 | 3052.1 | -26.6% |
| bench_conway.nx | 2214.9 | 1878.8 | -15.2% |
| bench_generic_vs_hand.nx | 904.5 | 732.4 | -19% |
| bench_map_churn.nx | 479.1 | 442 | -7.7% |
| bench_path_update.nx | 682.4 | 498.2 | -27% |
| bench_share_mutate.nx | 302.1 | 251.4 | -16.8% |
| bench_spawn_sum.nx | 767.1 | 703.7 | -8.3% |
| bench_typed_call_map.nx | 124.5 | 122.9 | -1.3% |
| bench_value_call_mutate.nx | 119.3 | 121.1 | 1.5% |

## 2. Por estágio — 4 binários intercalados (mediana de 9, mínimo entre parênteses)

`[stages_bench_a] inicio 16:29:22 | CPU load: 11 %` · `[stages_bench_b] inicio 16:35:53 | CPU load: 15 %` · `[stages_cr] inicio 16:39:14 | CPU load: 21 %` (o load inclui o próprio PowerShell do passo anterior encerrando)

### 2.1 `benchmarks/bench_*.nx`

| bench | base | s1 | s1+2 | s1+2+pop |
|---|---|---|---|---|
| bench_bubblesort.nx | 4144.6 (min 3985.5) | 3589.6 (min 3398.1) | 3489.6 (min 3355.5) | 3041.4 (min 2940.2) |
| bench_call_light.nx | 126.9 (min 96.2) | 111.7 (min 98.7) | 115.1 (min 97.8) | 129.6 (min 116.5) |
| bench_call_readonly.nx | 1354.3 (min 1265.5) | 1195.5 (min 1074) | 1216.6 (min 1128.3) | 984.9 (min 907.4) |
| bench_call_ref.nx | 4150.9 (min 4073.5) | 3559.1 (min 3445.9) | 3548.5 (min 3488.8) | 3073.7 (min 3020.9) |
| bench_conway.nx | 2191.5 (min 2098.8) | 2152.2 (min 2032.3) | 2066 (min 2026.2) | 1941.7 (min 1803.7) |
| bench_generic_vs_hand.nx | 880.1 (min 823.4) | 816.6 (min 779.2) | 824.9 (min 802.7) | 733.8 (min 699.7) |
| bench_map_churn.nx | 480.5 (min 432.7) | 470.6 (min 457) | 475.6 (min 423.2) | 455.5 (min 366.8) |
| bench_path_update.nx | 671.8 (min 624.7) | 633.7 (min 617.4) | 625.5 (min 585.2) | 465.8 (min 453.9) |
| bench_share_mutate.nx | 301.8 (min 277.1) | 259.5 (min 241.2) | 247.2 (min 237.8) | 250.9 (min 238.9) |
| bench_spawn_sum.nx | 781.6 (min 720.1) | 723.5 (min 709.2) | 753 (min 659.7) | 676.1 (min 642.1) |
| bench_typed_call_map.nx | 127.5 (min 120.6) | 126 (min 116.9) | 125.4 (min 118) | 122.5 (min 118) |
| bench_value_call_mutate.nx | 129.8 (min 120.8) | 120.7 (min 110.6) | 122.3 (min 113.6) | 122.3 (min 116.6) |

### 2.2 `benchmarks/cross_runtime/*.nx` (só o Noxy, 4 binários)

| bench | base | s1 | s1+2 | s1+2+pop |
|---|---|---|---|---|
| cr_bubblesort.nx | 731.5 (min 684.2) | 647.6 (min 606.6) | 630.2 (min 594.3) | 556.2 (min 541.3) |
| cr_fib.nx | 530.7 (min 478.4) | 477.6 (min 436.5) | 462.5 (min 438.9) | 436.5 (min 423.7) |
| cr_loop_arith.nx | 455 (min 421.4) | 438.8 (min 407.4) | 425.3 (min 394.6) | 363.2 (min 351.8) |
| cr_mandelbrot.nx | 350.8 (min 331.3) | 339.3 (min 305.8) | 340.7 (min 305) | 296.7 (min 273.4) |
| cr_map_churn.nx | 334.6 (min 323.8) | 348.3 (min 327.6) | 344.1 (min 327.4) | 312.4 (min 290.3) |
| cr_startup.nx | 106.2 (min 102.2) | 110.8 (min 101.1) | 110.2 (min 101.7) | 108.9 (min 100.4) |
| cr_string_ops.nx | 287.4 (min 265) | 268.8 (min 253.6) | 284.1 (min 250.1) | 254 (min 244.4) |

### 2.3 Deltas derivados das medianas de 2.1/2.2

"vs base" é o acumulado; "passo" é o delta do estágio contra o binário
imediatamente anterior (s1 vs base, s1+2 vs s1, pop vs s1+2).

| bench | s1 vs base | s1+2 vs base (passo) | s1+2+pop vs base (passo) |
|---|---|---|---|
| bench_bubblesort | −13,4 % | −15,8 % (−2,8 %) | **−26,6 %** (−12,8 %) |
| bench_call_ref | −14,3 % | −14,5 % (−0,3 %) | **−26,0 %** (−13,4 %) |
| bench_conway | −1,8 % | −5,7 % (−4,0 %) | **−11,4 %** (−6,0 %) |
| bench_call_readonly | −11,7 % | −10,2 % (+1,8 %) | **−27,3 %** (−19,0 %) |
| bench_generic_vs_hand | −7,2 % | −6,3 % (+1,0 %) | **−16,6 %** (−11,0 %) |
| bench_path_update | −5,7 % | −6,9 % (−1,3 %) | **−30,7 %** (−25,5 %) |
| bench_share_mutate | −14,0 % | −18,1 % (−4,7 %) | **−16,9 %** (+1,5 %) |
| bench_spawn_sum | −7,4 % | −3,7 % (+4,1 %) | **−13,5 %** (−10,2 %) |
| bench_map_churn | −2,1 % | −1,0 % (+1,1 %) | **−5,2 %** (−4,2 %) |
| bench_call_light / typed_call_map / value_call_mutate | piso de processo | piso | piso |
| cr_fib | −10,0 % | −12,9 % (−3,2 %) | **−17,7 %** (−5,6 %) |
| cr_bubblesort | −11,5 % | −13,8 % (−2,7 %) | **−24,0 %** (−11,7 %) |
| cr_loop_arith | −3,6 % | −6,5 % (−3,1 %) | **−20,2 %** (−14,6 %) |
| cr_mandelbrot | −3,3 % | −2,9 % (+0,4 %) | **−15,4 %** (−12,9 %) |
| cr_string_ops | −6,5 % | −1,1 % (+5,7 %) | **−11,6 %** (−10,6 %) |
| cr_map_churn | +4,1 % | +2,8 % (−1,2 %) | **−6,6 %** (−9,2 %) |
| cr_startup | +4,3 % | +3,8 % | +2,5 % (piso: 100–110 ms, ruído) |

## 3. Perfil de `fib` (`noxy --cpuprofile`, uma execução, amostras de 10 ms)

`[profile] inicio 16:41:28 | CPU load: 7 %`

base (v0.14.2) — 410 ms de amostras:

```
     flat  flat%        cum   cum%
    190ms 46.34%      390ms 95.12%  vm.(*VM).run
     70ms 17.07%       70ms 17.07%  vm.(*VM).pop            ← chamada real, nao inlinada
     30ms  7.32%       30ms  7.32%  chunk.(*Chunk).GlobalCache
     30ms  7.32%       30ms  7.32%  vm.(*VM).finalizeCurrentFrame
     30ms  7.32%       30ms  7.32%  vm.(*VM).push (inline)
     20ms  4.88%       50ms 12.20%  vm.(*VM).finishFrame
```

s1+2+pop — 310 ms de amostras:

```
     flat  flat%        cum   cum%
    160ms 51.61%      290ms 93.55%  vm.(*VM).run
     40ms 12.90%       40ms 12.90%  vm.(*VM).push (inline)
     30ms  9.68%       40ms 12.90%  vm.(*VM).callPreparedClosure
     20ms  6.45%       20ms  6.45%  vm.(*VM).finalizeCurrentFrame
     20ms  6.45%       40ms 12.90%  vm.(*VM).finishFrame
     10ms  3.23%       10ms  3.23%  vm.(*VM).ensureCallCapacity (inline)
     10ms  3.23%       10ms  3.23%  vm.(*VM).pop (inline)
```

`pop` sai de 17 % flat (função chamada) para 3 % (inlinada); `push` inlinada
segue visível porque o perfil atribui ao corpo embutido. O perfil é curto
(fib(30), ~0,5 s) — serve de indicador de onde o tempo foi, não de medida.

## 4. Cross-runtime — `run_cross_runtime.ps1 -NoxyBaseline` (mínimo de 9, intercalado com CPython/Lua/Go)

`[cross] inicio 16:41:47 | CPU load: 8 %` → `fim 16:43:10`

## Tempo total (ms)

| bench | noxy | v0142 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 514,4 | 716,4 | 165,4 | - | - |
| `fib` | 387,3 | 511,7 | 191,9 | 114,7 | 75,0 |
| `loop_arith` | 363,8 | 418,6 | 346,4 | 103,9 | 79,7 |
| `mandelbrot` | 268,2 | 310,1 | 179,1 | - | - |
| `map_churn` | 265,0 | 317,6 | 171,2 | - | - |
| `startup` | 91,3 | 91,0 | 88,5 | 61,9 | 71,4 |
| `string_ops` | 225,8 | 236,8 | 120,4 | - | - |

## Tempo de execucao, descontado o piso de `startup` (ms)

| bench | noxy | v0142 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 423,1 | 625,4 | 76,9 | - | - |
| `fib` | 296,0 | 420,7 | 103,4 | 52,8 | ~0 |
| `loop_arith` | 272,5 | 327,6 | 257,9 | 42,0 | 8,3 |
| `mandelbrot` | 176,9 | 219,1 | 90,6 | - | - |
| `map_churn` | 173,7 | 226,6 | 82,7 | - | - |
| `string_ops` | 134,5 | 145,8 | 31,9 | - | - |

`~0` = o trabalho cabe dentro do ruido do piso de processo do runtime.

## Razoes sobre o tempo liquido (noxy / outro)

| bench | / v0142 | / python | / lua | / go |
|---|---|---|---|---|
| `bubblesort` | 0,68x | 5,50x | - | - |
| `fib` | 0,70x | 2,86x | 5,61x | - |
| `loop_arith` | 0,83x | 1,06x | 6,49x | 32,83x |
| `mandelbrot` | 0,81x | 1,95x | - | - |
| `map_churn` | 0,77x | 2,10x | - | - |
| `string_ops` | 0,92x | 4,22x | - | - |

Menor e melhor. `-` = um dos lados cai dentro do ruido do piso e a razao nao tem significado.

## 5. `BenchmarkNoxyCallOverhead` por estágio (`go test ./internal/vm -run '^$' -bench NoxyCallOverhead -count 8`)

Rodado em sequência (um worktree por commit, não intercalado) com a máquina
sem outra carga; 1000 chamadas `leaf(i)` por op. Complementa, não decide.

| estágio | commit | min ns/op | mediana ns/op | max ns/op | mediana vs base |
|---|---|---|---|---|---|
| base (v0.14.2) | cb8efcb | 133 029 | 145 098 | 212 789 | — |
| s1 | 41638e8 | 123 838 | 139 155 | 155 788 | −4,1 % |
| s1+2 | e4d5a5f | 130 533 | 137 894 | 147 586 | −5,0 % |
| s1+2+pop | ba7f85d | 120 172 | 126 310 | 141 656 | **−13,0 %** |

## 6. Microbench Go de `ownersOf` (módulo descartável, não entra no repo)

`go test -bench . -count 3 -benchtime 200ms` sobre 1200 Values misturados
(array/string/int/map/instância/struct) e cargas puras (só arrays, só strings):

| forma | misto ns/op (3 rodadas) | só arrays | só strings |
|---|---|---|---|
| A: type switch + checagem de Type (v0.14.2) | 6006 / 5025 / 5415 | 6099 / 6015 / 5189 | 4691 / 5888 / 5954 |
| B: dica `kind` + type switch (embarcado) | 6123 / 5751 / 5204 | 5387 / 5749 / 5519 | 5152 / 5329 / 4950 |
| B2: switch no `kind` + assertion por caso + slow path (custo 73, não cabe) | 6133 / 5898 / 6149 | 5428 / 6065 / 5878 | — |
| C: dica + cast `unsafe` do header (sem slow path) | 5073 / 5355 / 6213 | 5988 / 4964 / 4844 | 4730 / 5367 / 6110 |

~4–5 ns por chamada em todas; a dispersão entre rodadas (±20 %) é maior que
qualquer diferença entre formas. Conclusão: a discriminação por tipo não é
custo mensurável no `gc`; o custo do RC são os atômicos (`Load`/`Add`/`CAS`).

## 7. Script dos 4 binários (`interleave4.ps1`)

```powershell
param([string[]]$Exes, [string[]]$Labels, [string[]]$Files, [int]$Runs = 9, [string]$Out = "")
# Intercala N binarios na mesma janela de tempo sobre cada arquivo .nx:
# para cada rodada, roda exe1, exe2, ..., exeN; repete Runs vezes. Reporta
# mediana (min) por binario. Mesma ideia de benchmarks/interleaved_compare.ps1,
# generalizada para isolar estagios (base / s1 / s1+2 / s1+2+pop).
$ErrorActionPreference = "Stop"
$rows = @("| bench | " + ($Labels -join " | ") + " |", "|---|" + ("---|" * $Labels.Count))
foreach ($f in $Files) {
    # guard de equivalencia: todos os binarios tem de concordar no CHECKSUM
    $sums = @()
    for ($k = 0; $k -lt $Exes.Count; $k++) {
        $o = & $Exes[$k] $f 2>&1 | Where-Object { $_ -match "^CHECKSUM:" }
        if ($o -is [array]) { $o = $o[0] }
        $sums += "$o"
    }
    if (($sums | Select-Object -Unique).Count -ne 1) {
        Write-Host "$(Split-Path $f -Leaf): PULADO (checksums divergem: $($sums -join ' / '))" -ForegroundColor Yellow
        $rows += "| $(Split-Path $f -Leaf) | PULADO: " + ($sums -join " / ") + " |"
        continue
    }
    $times = @{}; foreach ($l in $Labels) { $times[$l] = @() }
    for ($i = 0; $i -lt $Runs; $i++) {
        for ($k = 0; $k -lt $Exes.Count; $k++) {
            $ms = (Measure-Command { & $Exes[$k] $f | Out-Null }).TotalMilliseconds
            $times[$Labels[$k]] += [math]::Round($ms, 1)
        }
    }
    $cells = @()
    foreach ($l in $Labels) {
        $sorted = $times[$l] | Sort-Object
        $med = $sorted[[int](($Runs - 1) / 2)]; $min = $sorted[0]
        $cells += "$med (min $min)"
    }
    $rows += "| $(Split-Path $f -Leaf) | " + ($cells -join " | ") + " |"
    Write-Host ($rows[-1])
}
if ($Out) { $rows | Set-Content $Out; Write-Host "wrote $Out" }
$rows
```
