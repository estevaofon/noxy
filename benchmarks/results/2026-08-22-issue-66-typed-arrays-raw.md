# Dados brutos — indexação tipada de array (issue #66, item 1): v0.14.3 × estágios

**Data:** 2026-08-22 · Windows 11 · i7-1165G7 (8 threads lógicas) · Go 1.24.11 ·
laptop, na tomada.

**Binários** (todos em disco local, fora do OneDrive; mesma sessão de build,
cada um executado uma vez logo depois do `go build` — o EDR da máquina apaga
binário recém-compilado que fica parado sem rodar: o `s0` original sumiu
entre o build e a primeira rodada e foi rebuildado de uma worktree destacada
no mesmo commit):

| rótulo | commit | o que é |
|---|---|---|
| `base` (v0143) | 7eed082 | `develop` = v0.14.3 |
| `s0` | bf4f995 | só o VM: seis handlers novos (nenhum emitido ainda), `setIndexGeneric`, `unicizeOwnedSlot`/`unicizeBorrowedSlot`, rótulo `goto redispatch` |
| `s1` | 59c3b57 | + compilador emite `OP_GET_INDEX_ARRAY` / `OP_SET_INDEX_ARRAY_NORC` (formas genéricas tipadas) |
| `s2` | c252400 | + formas fundidas por slot de local plano (`OP_GET_LOCAL_INDEX_ARRAY`, inclusive for-each; `OP_SET_LOCAL_INDEX_ARRAY_NORC`) |
| `s3goto` | 4799e1d | + formas fundidas de `ref T[]` (`OP_GET_REF_LOCAL_INDEX_ARRAY` / `OP_SET_REF_LOCAL_INDEX_ARRAY_NORC`) — **ainda com o `goto redispatch`** |
| `s3` (typed, head) | d870a02 | = s3goto com o fallback de leitura por `getIndexGeneric` (método), sem rótulo — o código que vai no PR |

**Protocolo:** o de `benchmarks/RESULTS.md` — execuções intercaladas na mesma
janela de tempo, guard de `CHECKSUM:` entre binários, mediana de 9 (headline,
`interleaved_compare.ps1`) ou de 5 (por estágio, `stages.ps1` reproduzido no
fim, 5–6 binários). Fontes `.nx` copiados para disco local antes de medir.
**Scripts rodados com `pwsh -NoProfile -File`** — `interleaved_compare.ps1`
não parseia no Windows PowerShell 5.1 (UTF-8 sem BOM, travessão na linha 39);
a primeira tentativa com `powershell -File` saiu com exit 1 sem medir.

**Carga da máquina:** `Win32_Processor.LoadPercentage` no início de cada passo
(linhas "load antes"); Firefox, Slack e o Claude Code abertos; nenhum `go test`
nem build durante as medições (os builds de estágio aconteceram antes).

Os benches curtos (`bench_call_light`, `bench_typed_call_map`,
`bench_value_call_mutate`, ~120 ms com piso de processo ~90 ms) **não decidem
nada** (RESULTS.md, 2026-08-22): são listados, não interpretados.

## 1. Rodada 1 — com o `goto redispatch` (binário `s3goto`)

### 1.1 Headline — `interleaved_compare.ps1` base × s3goto (mediana de 9)

`load antes: 11 %` · `load depois: 3 %`

| bench | v0143_ms | s3goto_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 3007.7 | 1168.2 | -61.2% |
| bench_call_light.nx | 131.4 | 126.3 | -3.9% |
| bench_call_readonly.nx | 1038.5 | 974.4 | -6.2% |
| bench_call_ref.nx | 3216 | 1760.4 | -45.3% |
| bench_conway.nx | 1959.2 | 1958.7 | -0% |
| bench_generic_vs_hand.nx | 734.8 | 829.2 | **12.8%** |
| bench_map_churn.nx | 449.8 | 430.4 | -4.3% |
| bench_path_update.nx | 515.2 | 471 | -8.6% |
| bench_share_mutate.nx | 265.4 | 250 | -5.8% |
| bench_spawn_sum.nx | 710.9 | 751.2 | 5.7% |
| bench_typed_call_map.nx | 129.2 | 127.7 | -1.2% |
| bench_value_call_mutate.nx | 129.9 | 124.8 | -3.9% |

### 1.2 Por estágio — 5 binários intercalados (mediana de 5, tempo de parede)

`load antes: 6 %` · `load depois: 11 %`

| bench | base_ms | s0_ms | s1_ms | s2_ms | s3goto_ms |
|---|---|---|---|---|---|
| bench_bubblesort | 3.080,1 | 2.990,4 | 2.954,4 | 2.843,3 | 1.138,6 |
| bench_call_light | 140,9 | 148,9 | 110,0 | 121,3 | 118,4 |
| bench_call_readonly | 1.007,5 | 1.089,4 | 980,1 | 943,8 | 981,3 |
| bench_call_ref | 3.015,2 | 3.053,3 | 2.963,4 | 2.919,6 | 1.736,0 |
| bench_conway | 1.906,6 | 1.962,4 | 1.811,3 | 1.902,7 | 1.844,8 |
| bench_generic_vs_hand | 749,1 | 795,7 | 797,6 | 849,3 | 815,4 |
| bench_map_churn | 419,1 | 433,7 | 446,1 | 458,3 | 435,0 |
| bench_path_update | 508,1 | 515,6 | 476,3 | 445,8 | 460,2 |
| bench_share_mutate | 266,8 | 257,9 | 252,1 | 262,1 | 249,4 |
| bench_spawn_sum | 709,5 | 758,3 | 716,9 | 744,6 | 714,1 |
| bench_typed_call_map | 134,1 | 127,5 | 117,5 | 118,4 | 128,1 |
| bench_value_call_mutate | 122,9 | 141,4 | 116,4 | 116,2 | 116,5 |

Leitura: o ganho de `bubblesort`/`call_ref` é todo do estágio 3 (a resolução
do `ref`); s1 e s2 dão −1 a −8 % em `call_readonly`/`path_update`/`conway`.
**`generic_vs_hand` sobe já no s0** (+6 %), que não emite opcode novo — só muda
o código de `run()`. Esse bench não indexa nada (`acc = acc + length(arr)`):
é regressão de codegen do despacho, não dos opcodes.

### 1.3 A/B focado — `bench_generic_vs_hand`, relógio interno (`GEN_MS+HAND_MS`), 9 intercaladas

Isola o laço quente do piso de processo. Mediana (min/max) e as 9 amostras:

| binário | mediana | min | max | amostras |
|---|---|---|---|---|
| base | 610 | 577 | 725 | 603 577 606 605 610 612 644 630 725 |
| s0 | 669 | 635 | 778 | 669 683 666 653 778 677 635 638 708 |
| s3goto | 698 | 667 | 756 | 698 673 756 675 667 709 702 712 670 |

s1–s3goto têm o **mesmo** `run()` do s0 (só o compilador muda entre eles), então
+10 % no s0 é o código novo de `run()`: rótulo `redispatch:` (que faz de
`instruction` um phi com várias definições) + três tails de fallback inline.
Variante `v1` = s3goto com os fallbacks de leitura chamando `getIndexGeneric`
(corpo de `OP_GET_INDEX` extraído em método) e sem rótulo:

| binário | mediana | min | max | amostras |
|---|---|---|---|---|
| base | 623 | 574 | 731 | 619 574 581 711 599 671 633 623 731 |
| s3goto | 714 | 624 | 758 | 714 684 727 758 725 674 711 624 716 |
| v1 (= s3 final) | 631 | 598 | 655 | 654 610 598 600 652 636 617 631 655 |

O rótulo era o custo; a chamada no fallback não aparece. `v1` virou o commit
d870a02 (= `s3`), e a rodada 2 abaixo remede tudo com ele.

## 2. Rodada 2 — binário final `s3` (d870a02, sem `goto`)

### 2.1 Headline — `interleaved_compare.ps1` base × s3 (mediana de 9)

`load antes: 4 %` · `load depois: 23 %` (o load do fim inclui o próprio pwsh encerrando)

| bench | v0143_ms | typed_ms | delta |
|---|---|---|---|
| bench_bubblesort.nx | 3071.2 | 1089.4 | -64.5% |
| bench_call_light.nx | 130.7 | 128.6 | -1.6% |
| bench_call_readonly.nx | 1018.9 | 895.5 | -12.1% |
| bench_call_ref.nx | 3010.6 | 1702.8 | -43.4% |
| bench_conway.nx | 1871.3 | 1801.2 | -3.7% |
| bench_generic_vs_hand.nx | 730.7 | 736 | 0.7% |
| bench_map_churn.nx | 408.7 | 408 | -0.2% |
| bench_path_update.nx | 495.1 | 449.6 | -9.2% |
| bench_share_mutate.nx | 218.1 | 236.4 | 8.4% |
| bench_spawn_sum.nx | 668.7 | 677 | 1.2% |
| bench_typed_call_map.nx | 151.7 | 149.1 | -1.7% |
| bench_value_call_mutate.nx | 139.6 | 140.9 | 0.9% |

`generic_vs_hand` volta ao ruído (+0,7 %; era +12,8 % com o `goto`).
`share_mutate` +8,4 % nesta rodada contra −5,8 % na rodada 1, com a própria
base oscilando 265 → 218 ms entre rodadas — bench de ~230 ms perto do piso.
Rodada focada, 15 intercaladas, tempo de parede:

| binário | mediana | min | max |
|---|---|---|---|
| base | 233,0 | 214,4 | 345,1 |
| s3 | 236,7 | 217,0 | 298,9 |

+1,6 % na mediana, +1,2 % no mínimo: dentro do ruído, gate CoW (≤ +5 %) ok.
(O caminho que esse bench exercita é o fallback "array compartilhado" da
escrita fundida — `unicizeOwnedSlot` + `setIndexGeneric` com dois pops e três
pushes a mais que o caminho rápido; o clone O(n) domina.)

### 2.2 Por estágio — 6 binários intercalados (mediana de 5, tempo de parede)

`load antes: 5 %` · `load depois: 12 %`

| bench | base_ms | s0_ms | s1_ms | s2_ms | s3goto_ms | s3_ms |
|---|---|---|---|---|---|---|
| bench_bubblesort | 2.936,1 | 2.999,8 | 2.871,4 | 2.958,5 | 1.108,2 | 1.107,3 |
| bench_call_light | 124,2 | 123,5 | 119,1 | 117,3 | 104,5 | 135,4 |
| bench_call_readonly | 997,0 | 1.009,8 | 956,2 | 956,4 | 946,7 | 875,5 |
| bench_call_ref | 3.094,7 | 3.112,1 | 2.996,2 | 2.924,8 | 1.752,4 | 1.732,3 |
| bench_conway | 1.928,2 | 1.943,6 | 1.834,8 | 1.908,0 | 1.868,6 | 1.857,2 |
| bench_generic_vs_hand | 718,2 | 825,3 | 812,3 | 810,8 | 816,7 | 758,4 |
| bench_map_churn | 442,3 | 424,3 | 441,4 | 428,6 | 443,4 | 413,7 |
| bench_path_update | 506,3 | 539,0 | 479,6 | 458,9 | 467,2 | 453,6 |
| bench_share_mutate | 273,9 | 260,4 | 253,6 | 254,9 | 254,3 | 277,4 |
| bench_spawn_sum | 751,6 | 767,1 | 771,1 | 713,4 | 713,5 | 769,3 |
| bench_typed_call_map | 125,5 | 126,5 | 116,9 | 129,2 | 136,2 | 137,3 |
| bench_value_call_mutate | 129,9 | 132,4 | 118,8 | 129,0 | 126,0 | 129,7 |

Deltas `s3` vs `base` (medianas acima): bubblesort −62,3 %, call_ref −44,0 %,
call_readonly −12,2 %, path_update −10,4 %, conway −3,7 %, map_churn −6,5 %,
share_mutate +1,3 %, spawn_sum +2,4 %, generic_vs_hand +5,6 % (tempo de parede;
o relógio interno do bench em §1.3 dá +1,3 % — a coluna `s3goto` mostra os
+13,7 % que o `goto` custava, e `s0`–`s3goto`, que têm o mesmo `run()`,
empatam entre si em +12,9…+13,7 %).

## 3. Cross-runtime — `run_cross_runtime.ps1 -NoxyBaseline` (mínimo de 9, intercalado com CPython 3.13.1 / Lua 5.4.7 / Go 1.24.11)

`load antes: 4 %` · `load depois: 8 %`. Tabela completa em
`benchmarks/cross_runtime/results/cross_runtime.md` (sobrescrita por esta rodada).

| bench | noxy (s3) | v0143 | python | lua | go |
|---|---|---|---|---|---|
| `bubblesort` | 249,7 | 526,3 | 169,2 | - | - |
| `fib` | 406,0 | 417,7 | 209,7 | 112,2 | 76,1 |
| `loop_arith` | 379,3 | 364,2 | 371,7 | 102,0 | 76,6 |
| `mandelbrot` | 283,4 | 280,8 | 164,2 | - | - |
| `map_churn` | 291,4 | 269,3 | 164,1 | - | - |
| `startup` | 96,1 | 95,7 | 83,7 | 60,1 | 72,2 |
| `string_ops` | 230,5 | 234,5 | 124,1 | - | - |

Tempo líquido (menos `startup`): `bubblesort` 430,6 → **153,6 ms (0,36x)**,
**÷ CPython 5,50x → 1,80x**; `fib` 0,96x, `loop_arith` 1,05x, `mandelbrot`
1,01x, `map_churn` 1,12x, `string_ops` 0,97x de v0.14.3.

Os dois que subiram no mínimo-de-9 foram refeitos em A/B focado (11
intercaladas, tempo de parede, fontes locais):

| programa | base mediana (min) | s3 mediana (min) |
|---|---|---|
| `cross_runtime/map_churn.nx` | 316,4 (276,3) | 295,7 (269,2) |
| `cross_runtime/loop_arith.nx` | 372,6 (348,7) | 372,0 (345,1) |

`map_churn` sai −6,5 % e `loop_arith` empata: o +12 % / +5 % do cross eram
ruído (`map_churn.nx` nem passa pelos opcodes novos — só map com chave string).

## 4. Perfil de `cross_runtime/bubblesort.nx` (`noxy --cpuprofile`, uma execução, amostras de 10 ms)

**base** (490 ms de amostras, 637 ms de duração):

```
      flat  flat%   sum%        cum   cum%
     150ms 30.61% 30.61%      450ms 91.84%  noxy-vm/internal/vm.(*VM).run
      40ms  8.16% 38.78%      180ms 36.73%  noxy-vm/internal/vm.(*VM).referenceStorage
      30ms  6.12% 44.90%       60ms 12.24%  noxy-vm/internal/value.(*ObjUpvalue).Load
      30ms  6.12% 51.02%       40ms  8.16%  noxy-vm/internal/value.IsShared
      30ms  6.12% 57.14%       30ms  6.12%  runtime.deferprocStack
      20ms  4.08% 61.22%       20ms  4.08%  noxy-vm/internal/vm.(*VM).pop (inline)
      20ms  4.08% 65.31%       20ms  4.08%  noxy-vm/internal/vm.(*VM).push (inline)
      20ms  4.08% 69.39%       90ms 18.37%  noxy-vm/internal/vm.(*VM).resolveReferenceValue
      20ms  4.08% 73.47%       20ms  4.08%  runtime.cgocall
      20ms  4.08% 77.55%       30ms  6.12%  runtime.mallocgc
      20ms  4.08% 81.63%       20ms  4.08%  sync/atomic.(*Int32).Add (inline)
      10ms  2.04% 83.67%       10ms  2.04%  noxy-vm/internal/value.Release
```

(Um perfil anterior da mesma base, tirado antes da implementação, dava
`resolveReferenceValue`/`referenceStorage` 42 % cum, `mallocgc` 15 %,
`ObjUpvalue.Load` 10,6 %, `unicizeThroughRefValue` 10,6 % — a motivação da
forma fundida de `ref` na spec §1.)

**head** (160 ms de amostras, 328 ms de duração):

```
      flat  flat%   sum%        cum   cum%
      90ms 56.25% 56.25%      130ms 81.25%  noxy-vm/internal/vm.(*VM).run
      20ms 12.50% 68.75%       20ms 12.50%  runtime.cgocall
      20ms 12.50% 81.25%       20ms 12.50%  sync/atomic.(*Int32).Add (inline)
      10ms  6.25% 87.50%       10ms  6.25%  noxy-vm/internal/vm.(*VM).pop (inline)
      10ms  6.25% 93.75%       10ms  6.25%  noxy-vm/internal/vm.(*VM).push (inline)
      10ms  6.25%   100%       10ms  6.25%  runtime.unlock2
         0     0%   100%       20ms 12.50%  noxy-vm/internal/value.(*ObjUpvalue).Load
```

`referenceStorage`, `resolveReferenceValue`, `IsShared`, `mallocgc` e o `defer`
somem do perfil; o que resta do acesso via `ref` é `Upvalue.Load` e os
atômicos do seu `RWMutex` (`atomic.Int32.Add` + `unlock2`, ~25 % das amostras
do head) — o follow-up natural se `bubblesort` precisar passar de 1,8x.

## 5. Script dos 5–6 binários (`stages.ps1`)

```powershell
param(
    [int]$Runs = 5,
    [string]$Repo = "D:\OneDrive\Documentos\go_projects\noxy\.claude\worktrees\perf-issue-66-arrays"
)
# Intercalacao de CINCO binarios (base + 4 estagios) na mesma janela, mediana
# de $Runs por bench — o "script de sessao" da medicao por estagio (issue #66,
# item 1). Mesmo principio de benchmarks/interleaved_compare.ps1: os binarios
# alternam a cada execucao, imunizando contra drift de carga/termico.
$ErrorActionPreference = "Stop"
$S = $PSScriptRoot
$bins = [ordered]@{
    base = "$S\noxy_base.exe"
    s0   = "$S\noxy_s0.exe"
    s1   = "$S\noxy_s1.exe"
    s2   = "$S\noxy_s2.exe"
    s3goto = "$S\noxy_s3goto.exe"
    s3   = "$S\noxy_s3.exe"
}
$local = "$S\nx"
New-Item -ItemType Directory -Force $local | Out-Null
$benches = Get-ChildItem "$Repo\benchmarks" -Filter "bench_*.nx" | Sort-Object Name
$benches | ForEach-Object { Copy-Item $_.FullName $local -Force }

$header = "| bench | " + (($bins.Keys | ForEach-Object { "$($_)_ms" }) -join " | ") + " |"
$sep = "|---|" + (($bins.Keys | ForEach-Object { "---" }) -join "|") + "|"
$rows = @($header, $sep)
foreach ($b in $benches) {
    # guard de equivalencia: mesma linha CHECKSUM em todos
    $sums = @{}
    foreach ($k in $bins.Keys) {
        $out = & $bins[$k] "$local\$($b.Name)" 2>&1 | Where-Object { $_ -match "^CHECKSUM:" }
        if ($out -is [array]) { $out = $out[0] }
        $sums[$k] = "$out"
    }
    $distinct = $sums.Values | Sort-Object -Unique
    if ($distinct.Count -ne 1 -or -not $distinct[0]) {
        $rows += "| $($b.BaseName) | PULADO (checksums: $($sums.Values -join ' / ')) |"
        Write-Host "$($b.Name): PULADO" -ForegroundColor Yellow
        continue
    }
    $times = @{}
    $bins.Keys | ForEach-Object { $times[$_] = @() }
    for ($r = 0; $r -lt $Runs; $r++) {
        foreach ($k in $bins.Keys) {
            $sw = [Diagnostics.Stopwatch]::StartNew()
            & $bins[$k] "$local\$($b.Name)" *> $null
            $sw.Stop()
            $times[$k] += $sw.Elapsed.TotalMilliseconds
        }
    }
    $cells = $bins.Keys | ForEach-Object {
        $sorted = $times[$_] | Sort-Object
        "{0:N1}" -f $sorted[[int][math]::Floor($sorted.Count / 2)]
    }
    $line = "| $($b.BaseName) | " + ($cells -join " | ") + " |"
    $rows += $line
    Write-Host $line
}
$rows | Set-Content "$S\stages_results.md" -Encoding UTF8
Write-Host "gravado em $S\stages_results.md"
```
