param(
    [string]$Noxy = (Join-Path (Split-Path (Split-Path $PSScriptRoot -Parent) -Parent) "noxy.exe"),
    [string]$Python = "python",
    [string]$Lua = "lua",
    [int]$Runs = 9
)
# Compara o VM do Noxy com outros runtimes na mesma carga.
#
# Noxy e CPython cobrem os sete benches. Lua 5.4 e Go nativo cobrem so tres
# (startup, loop_arith, fib): entraram como calibracao, para responder "longe
# do que?" — o Lua e o comparavel direto (bytecode puro, sem JIT, sem inline
# cache, dinamicamente tipado) e o Go e o teto do hospedeiro. Um runtime
# ausente vira "-" na tabela, nao um erro.
#
# Tres decisoes de metodologia, cada uma por um efeito medido neste repo:
#
# 1. Intercalado (mesma tecnica de interleaved_compare.ps1): os runtimes
#    alternam dentro da mesma janela de tempo, imunizando a comparacao contra
#    drift termico/carga de background que contamina rodadas sequenciais por
#    rotulo.
#
# 2. Reporta o MINIMO, nao a mediana. Sob carga a distribuicao e assimetrica a
#    direita: interferencia so adiciona tempo, nunca remove. Medido aqui: a
#    mediana variou ~12% entre dois lotes identicos, o minimo manteve o ratio.
#
# 3. Copia os fontes para disco local. Este repo vive em OneDrive, e medir de
#    la inflou os tempos em ~2x (filtro de sync + antivirus no read). Aponte
#    -Noxy para um binario em disco local tambem.
#
# Cada implementacao de um bench imprime a mesma linha CHECKSUM: o script
# aborta se divergirem, porque ai nao seria a mesma carga.
$ErrorActionPreference = "Stop"

function Have([string]$exe) {
    return [bool](Get-Command $exe -ErrorAction SilentlyContinue)
}

$work = Join-Path $env:TEMP ("noxy_cross_" + [guid]::NewGuid().ToString("N").Substring(0, 8))
New-Item -ItemType Directory -Force $work | Out-Null

try {
    Copy-Item (Join-Path $PSScriptRoot "*.nx")  $work -Force
    Copy-Item (Join-Path $PSScriptRoot "*.py")  $work -Force
    Copy-Item (Join-Path $PSScriptRoot "*.lua") $work -Force -ErrorAction SilentlyContinue

    $hasLua = Have $Lua
    $hasGo  = Have "go"

    # Go compila antes de medir: o benchmark e do binario, nao do compilador.
    $goExe = @{}
    if ($hasGo) {
        Get-ChildItem (Join-Path $PSScriptRoot "go") -Directory | ForEach-Object {
            $out = Join-Path $work ("go_" + $_.Name + ".exe")
            & go build -o $out (Join-Path $_.FullName "main.go")
            if ($LASTEXITCODE -ne 0) { throw "go build falhou para $($_.Name)" }
            $goExe[$_.Name] = $out
        }
    }

    # Ordem fixa: define a ordem das colunas e a ordem do intercalamento.
    $order = @("noxy", "python", "lua", "go")

    $rows = @()
    foreach ($p in (Get-ChildItem $work -Filter "*.nx" | Sort-Object Name)) {
        $b = $p.BaseName

        # Monta so os runtimes que existem para este bench.
        $cmds = [ordered]@{}
        $cmds["noxy"]   = { & $Noxy   $p.FullName }.GetNewClosure()
        $py = [IO.Path]::ChangeExtension($p.FullName, ".py")
        if (Test-Path $py) { $cmds["python"] = { & $Python $py }.GetNewClosure() }
        $lu = [IO.Path]::ChangeExtension($p.FullName, ".lua")
        if ($hasLua -and (Test-Path $lu)) { $cmds["lua"] = { & $Lua $lu }.GetNewClosure() }
        if ($goExe.ContainsKey($b)) { $g = $goExe[$b]; $cmds["go"] = { & $g }.GetNewClosure() }

        # Warmup (aquece o cache de arquivo) + equivalencia entre runtimes.
        $chk = $null
        foreach ($k in $cmds.Keys) {
            $c = (& $cmds[$k] | Where-Object { $_ -match "^CHECKSUM:" })
            if (-not $c) { throw "$b/$k : sem linha CHECKSUM" }
            if ($null -eq $chk) { $chk = $c }
            elseif ($c -ne $chk) { throw "$b : checksum divergente ($k=$c, esperado $chk)" }
        }

        $times = @{}
        foreach ($k in $cmds.Keys) { $times[$k] = @() }
        for ($i = 0; $i -lt $Runs; $i++) {
            foreach ($k in $cmds.Keys) {
                $times[$k] += (Measure-Command { & $cmds[$k] | Out-Null }).TotalMilliseconds
            }
        }

        $row = [ordered]@{ bench = $b; checksum = $chk }
        foreach ($k in $order) {
            $row[$k] = if ($times.ContainsKey($k)) {
                [math]::Round(($times[$k] | Measure-Object -Minimum).Minimum, 1)
            } else { $null }
        }
        $rows += [pscustomobject]$row

        $shown = ($order | Where-Object { $null -ne $row[$_] } |
                  ForEach-Object { "{0}={1}ms" -f $_, $row[$_] }) -join "  "
        Write-Host ("{0,-12} {1}" -f $b, $shown)
    }
} finally {
    Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}

# ---- relatorio ----
$base = $rows | Where-Object { $_.bench -eq "startup" }
$present = $order | Where-Object { $b = $_; ($rows | Where-Object { $null -ne $_.$b }).Count -gt 0 }

function Cell($v) { if ($null -eq $v) { "-" } else { "{0:N1}" -f $v } }

# Liquido: total menos o piso de processo do proprio runtime. Para runtimes
# rapidos o trabalho pode caber dentro do ruido do piso, e a subtracao vai a
# zero ou fica negativa — reportamos "~0" em vez de fingir precisao.
function Net($v, $floor) {
    if ($null -eq $v -or $null -eq $floor) { return "-" }
    $n = $v - $floor
    if ($n -le 5) { return "~0" }
    return "{0:N1}" -f $n
}

$lines = @(
    "# Cross-runtime: Noxy x CPython x Lua x Go",
    "",
    "- noxy: ``$Noxy``",
    "- python: $((& $Python --version 2>&1) -join '')",
    "- lua: $(if (Have $Lua) { (& $Lua -v 2>&1) -join '' } else { 'ausente' })",
    "- go: $(if (Have 'go') { (& go version) -join '' } else { 'ausente' })",
    "- Data: $(Get-Date -Format s)",
    "- Runs por bench: $Runs, intercalados; **minimo** reportado",
    "",
    "## Tempo total (ms)",
    "",
    # Parenteses obrigatorios: dentro de @(...) o "+" entre strings vira
    # concatenacao de array e a linha sai quebrada em varios elementos.
    ("| bench | " + ($present -join " | ") + " |"),
    ("|---" * ($present.Count + 1) + "|")
)
foreach ($r in $rows) {
    $lines += "| ``$($r.bench)`` | " + (($present | ForEach-Object { Cell $r.$_ }) -join " | ") + " |"
}

$lines += @(
    "",
    "## Tempo de execucao, descontado o piso de ``startup`` (ms)",
    "",
    ("| bench | " + ($present -join " | ") + " |"),
    ("|---" * ($present.Count + 1) + "|")
)
foreach ($r in $rows) {
    if ($r.bench -eq "startup") { continue }
    $lines += "| ``$($r.bench)`` | " + (($present | ForEach-Object { Net $r.$_ $base.$_ }) -join " | ") + " |"
}
$lines += ""
$lines += "``~0`` = o trabalho cabe dentro do ruido do piso de processo do runtime."

$outDir = Join-Path $PSScriptRoot "results"
New-Item -ItemType Directory -Force $outDir | Out-Null
$lines | Set-Content (Join-Path $outDir "cross_runtime.md")
Write-Host "wrote results/cross_runtime.md"
