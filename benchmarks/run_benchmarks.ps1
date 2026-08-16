param(
    [Parameter(Mandatory)][string]$Binary,
    [Parameter(Mandatory)][string]$Label,
    [int]$Runs = 5
)
$ErrorActionPreference = "Stop"
$programs = Get-ChildItem $PSScriptRoot -Filter "bench_*.nx" | Sort-Object Name
$outDir = Join-Path $PSScriptRoot "results"
New-Item -ItemType Directory -Force $outDir | Out-Null
$lines = @(
    "# Benchmark results: $Label",
    "",
    "- Binary: ``$Binary``",
    "- Date: $(Get-Date -Format s)",
    "- Runs per bench: $Runs (median reported)",
    "",
    "| bench | median_ms | runs_ms | checksum |",
    "|---|---|---|---|"
)
foreach ($p in $programs) {
    $out = & $Binary $p.FullName
    $chk = ($out | Where-Object { $_ -match "^CHECKSUM:" }) -join ";"
    if (-not $chk) { throw "$($p.Name): sem linha CHECKSUM (saida: $out)" }
    $times = @()
    for ($i = 0; $i -lt $Runs; $i++) {
        $t = Measure-Command { & $Binary $p.FullName | Out-Null }
        $times += [math]::Round($t.TotalMilliseconds, 1)
    }
    $sorted = $times | Sort-Object
    $median = $sorted[[int](($sorted.Count - 1) / 2)]
    $lines += "| $($p.Name) | $median | $($times -join ' ') | $chk |"
    Write-Host "$($p.Name): median=${median}ms checksum=$chk"
}
$lines | Set-Content (Join-Path $outDir "$Label.md")
Write-Host "wrote results/$Label.md"
