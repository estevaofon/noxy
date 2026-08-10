$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    go build -o noxy-plugin-terminal.exe .
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed with exit code $LASTEXITCODE"
    }
    Write-Host "Created noxy-plugin-terminal.exe"
} finally {
    Pop-Location
}
