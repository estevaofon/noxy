param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate,
    [string]$BaselineLabel = "baseline",
    [string]$CandidateLabel = "candidate",
    [int]$Runs = 5
)
# Medição intercalada: alterna os dois binários dentro da mesma janela de
# tempo, imunizando a comparação contra drift térmico/carga de background
# que contamina rodadas sequenciais por rótulo.
#
# O warmup também é o guard de equivalência: cada bench imprime uma linha
# CHECKSUM: e os dois binários têm de concordar. Sem isso, um bench que o
# baseline nem compila (sintaxe mais nova que ele) sai do erro em ~30ms e
# entra na tabela como se o candidato tivesse regredido 10x — a comparação
# seria entre um programa e uma mensagem de erro. Bench assim é pulado e
# listado como tal, não medido.
$ErrorActionPreference = "Stop"

function Checksum([string]$exe, [string]$file) {
    $out = & $exe $file 2>&1 | Where-Object { $_ -match "^CHECKSUM:" }
    if ($out -is [array]) { $out = $out[0] }
    return $out
}

$programs = Get-ChildItem $PSScriptRoot -Filter "bench_*.nx" | Sort-Object Name
$lines = @(
    "| bench | $($BaselineLabel)_ms | $($CandidateLabel)_ms | delta |",
    "|---|---|---|---|"
)
$skipped = @()
foreach ($p in $programs) {
    $cb = Checksum $Baseline  $p.FullName
    $cc = Checksum $Candidate $p.FullName
    if (-not $cb -or -not $cc -or $cb -ne $cc) {
        $why = if (-not $cb) { "sem CHECKSUM no $BaselineLabel" }
               elseif (-not $cc) { "sem CHECKSUM no $CandidateLabel" }
               else { "checksum divergente ($BaselineLabel=$cb, $CandidateLabel=$cc)" }
        $skipped += "$($p.Name) — $why"
        Write-Host "$($p.Name): PULADO ($why)" -ForegroundColor Yellow
        continue
    }
    $tb = @(); $tc = @()
    for ($i = 0; $i -lt $Runs; $i++) {
        $tb += [math]::Round((Measure-Command { & $Baseline $p.FullName | Out-Null }).TotalMilliseconds, 1)
        $tc += [math]::Round((Measure-Command { & $Candidate $p.FullName | Out-Null }).TotalMilliseconds, 1)
    }
    $mb = ($tb | Sort-Object)[[int](($Runs - 1) / 2)]
    $mc = ($tc | Sort-Object)[[int](($Runs - 1) / 2)]
    $delta = [math]::Round(100 * ($mc - $mb) / $mb, 1)
    $lines += "| $($p.Name) | $mb | $mc | $delta% |"
    Write-Host "$($p.Name): $BaselineLabel=$mb $CandidateLabel=$mc delta=$delta%"
}
if ($skipped.Count -gt 0) {
    $lines += ""
    $lines += "Pulados (sem equivalencia entre os dois binarios):"
    $lines += ""
    foreach ($s in $skipped) { $lines += "- $s" }
}
$lines | Set-Content (Join-Path $PSScriptRoot "results\interleaved.md")
Write-Host "wrote results/interleaved.md"
