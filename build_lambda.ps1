# build_lambda.ps1
# Script para compilar o Noxy para Linux e preparar o pacote de deploy (ZIP)

Write-Host "Preparando Noxy para AWS Lambda..." -ForegroundColor Cyan

# 1. Definir variáveis de ambiente para Cross-Compilation (Linux amd64)
$Env:GOOS = "linux"
$Env:GOARCH = "amd64"

Write-Host "1. Compilando binário 'bootstrap' (Noxy VM) para Linux/AMD64..." -ForegroundColor Yellow
# O binário deve se chamar 'noxy' ou podemos chamá-lo de 'noxy_linux'
# O script 'bootstrap' (shell) vai chamar o executável noxy.
go build -o noxy_linux .\cmd\noxy

if ($LASTEXITCODE -ne 0) {
    Write-Error "Falha na compilação!"
    exit 1
}

Write-Host "Compilação concluída!" -ForegroundColor Green

# 2. Organizar arquivos para o ZIP
$lambdaDir = ".\lambda_dist"
if (Test-Path $lambdaDir) { Remove-Item -Recurse -Force $lambdaDir }
New-Item -ItemType Directory -Force -Path $lambdaDir | Out-Null

Write-Host "2. Copiando arquivos para $lambdaDir..." -ForegroundColor Yellow

# Copia e renomeia o binário para 'noxy' dentro da pasta
Copy-Item ".\noxy_linux" -Destination "$lambdaDir\noxy"

# Copia os scripts do runtime e função
Copy-Item ".\noxy_examples\aws_lambda\bootstrap" -Destination "$lambdaDir\bootstrap"
Copy-Item ".\noxy_examples\aws_lambda\runtime.nx" -Destination "$lambdaDir\runtime.nx"
Copy-Item ".\noxy_examples\aws_lambda\function.nx" -Destination "$lambdaDir\function.nx"

Write-Host "Arquivos copiados." -ForegroundColor Green

# 3. Criar o ZIP
$zipFile = ".\noxy_lambda_deploy.zip"
if (Test-Path $zipFile) { Remove-Item -Force $zipFile }

Write-Host "3. Criando arquivo ZIP: $zipFile" -ForegroundColor Yellow
Compress-Archive -Path "$lambdaDir\*" -DestinationPath $zipFile

Write-Host "`nPacote de Deploy pronto: $zipFile" -ForegroundColor Cyan
Write-Host "IMPORTANTE: Como você está no Windows, o arquivo 'bootstrap' pode perder a permissão de execução (+x)." -ForegroundColor Red
Write-Host "Se o Lambda reclamar de 'permission denied', use o comando 'chmod +x bootstrap' em um ambiente Linux antes de zipar ou ajuste via CI/CD." -ForegroundColor Red
