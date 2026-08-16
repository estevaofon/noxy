param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate,
    [int]$Runs = 5
)
# Medição intercalada: alterna os dois binários dentro da mesma janela de
# tempo, imunizando a comparação contra drift térmico/carga de background
# que contamina rodadas sequenciais por rótulo.
$ErrorActionPreference = "Stop"
$programs = Get-ChildItem $PSScriptRoot -Filter "bench_*.nx" | Sort-Object Name
$lines = @(
    "| bench | baseline_ms | cow_ms | delta |",
    "|---|---|---|---|"
)
foreach ($p in $programs) {
    & $Baseline $p.FullName | Out-Null
    & $Candidate $p.FullName | Out-Null
    $tb = @(); $tc = @()
    for ($i = 0; $i -lt $Runs; $i++) {
        $tb += [math]::Round((Measure-Command { & $Baseline $p.FullName | Out-Null }).TotalMilliseconds, 1)
        $tc += [math]::Round((Measure-Command { & $Candidate $p.FullName | Out-Null }).TotalMilliseconds, 1)
    }
    $mb = ($tb | Sort-Object)[[int](($Runs - 1) / 2)]
    $mc = ($tc | Sort-Object)[[int](($Runs - 1) / 2)]
    $delta = [math]::Round(100 * ($mc - $mb) / $mb, 1)
    $lines += "| $($p.Name) | $mb | $mc | $delta% |"
    Write-Host "$($p.Name): baseline=$mb cow=$mc delta=$delta%"
}
$lines | Set-Content (Join-Path $PSScriptRoot "results\interleaved.md")
Write-Host "wrote results/interleaved.md"
