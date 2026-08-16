param(
    [Parameter(Mandatory)][string]$Baseline,
    [Parameter(Mandatory)][string]$Candidate,
    [int]$TimeoutSec = 15
)
$ErrorActionPreference = "Stop"

# Exclusões: exemplos cuja saída não é determinística ou que dependem de
# recursos externos — cada grupo com o motivo.
$exclude = @(
    # servidores/bloqueantes/rede
    "http_server_basic.nx", "http_server_docs.nx", "http_server_sockets.nx",
    "debug_http.nx", "simple_server.nx", "simple_client.nx", "test_http_server.nx",
    "test_web_server.nx", "web_app.nx", "network_poller.nx", "test_net.nx",
    "test_import_net.nx", "watch_file.nx", "form_app.nx", "todo_app.nx",
    "signal_demo.nx", "test_close.nx", "test_is_closed.nx", "test_unclosed.nx",
    # interativos/animação/dependentes de tempo
    "cli_example.nx", "fibonacci_spinner.nx", "space_invaders.nx",
    "space_invaders2.nx", "conway.nx", "conway_random.nx", "langtons_ant.nx",
    "time_demo.nx", "benchmark_parallel.nx", "supervised_tasks.nx",
    # saída aleatória
    "rand_demo.nx", "password_generator.nx", "uuid_demo.nx",
    "test_crypto_aes.nx", "test_crypto_debug.nx", "test_crytpo.nx",
    # estado externo (sqlite em disco, AWS, plugins)
    "sqlite_demo.nx", "sqlite_read.nx", "sqlite_showcase.nx",
    "repro_sqlite_concurrency.nx", "cadastro_usuarios.nx", "read_passwords.nx",
    "dynamodb_example.nx", "dynamodb_query_scan.nx", "test_dynamodb_plugin.nx",
    "test_libs.nx", "test_file_hash.nx", "fs_test.nx",
    # meta-runners (rodam os demais)
    "run_all_tests.nx", "run_all_tests_concurrent.nx"
)

function Run-WithTimeout([string]$exe, [string]$file, [int]$timeoutSec) {
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = $exe
    $psi.Arguments = '"' + $file + '"'
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $p = [System.Diagnostics.Process]::Start($psi)
    $out = $p.StandardOutput.ReadToEndAsync()
    $err = $p.StandardError.ReadToEndAsync()
    if (-not $p.WaitForExit($timeoutSec * 1000)) {
        try { $p.Kill($true) } catch {}
        return "<<TIMEOUT>>"
    }
    return ($out.Result + "`n--stderr--`n" + $err.Result)
}

$diffs = @()
$same = 0
$repoRoot = Split-Path $PSScriptRoot -Parent
Get-ChildItem (Join-Path $repoRoot "noxy_examples") -Filter "*.nx" |
    Where-Object { $exclude -notcontains $_.Name } |
    Sort-Object Name | ForEach-Object {
        $b = Run-WithTimeout $Baseline $_.FullName $TimeoutSec
        $c = Run-WithTimeout $Candidate $_.FullName $TimeoutSec
        if ($b -ne $c) {
            $diffs += $_.Name
            Write-Host "DIFF: $($_.Name)"
        } else {
            $same++
        }
    }

Write-Host ""
Write-Host "iguais: $same | divergentes: $($diffs.Count) | excluidos: $($exclude.Count)"
if ($diffs) {
    Write-Host "DIVERGENTES:"
    $diffs | ForEach-Object { Write-Host "  $_" }
    exit 1
}
exit 0
