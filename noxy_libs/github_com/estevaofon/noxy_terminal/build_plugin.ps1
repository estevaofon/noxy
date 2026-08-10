$ErrorActionPreference = "Stop"
Push-Location $PSScriptRoot
try {
    go build -o noxy-plugin-terminal.exe .
    Write-Host "Created noxy-plugin-terminal.exe"
} finally {
    Pop-Location
}
