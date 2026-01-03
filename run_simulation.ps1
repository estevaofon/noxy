# run_simulation.ps1
# Script para rodar a simulação do Lambda localmente

Write-Host "Iniciando Simulação Local do Lambda..." -ForegroundColor Cyan

# 1. Matar processos anteriores (limpeza)
Stop-Process -Name "noxy" -ErrorAction SilentlyContinue

# 2. Iniciar o Mock da API (Servidor)
Write-Host "1. Iniciando Mock API Server..." -ForegroundColor Yellow
$mockProcess = Start-Process -FilePath ".\noxy.exe" -ArgumentList "noxy_examples\aws_lambda\mock_lambda_api.nx" -PassThru -NoNewWindow
Start-Sleep -Seconds 2

# 3. Iniciar o Runtime (Cliente que faz polling)
Write-Host "2. Iniciando Noxy Runtime..." -ForegroundColor Yellow
$runtimeProcess = Start-Process -FilePath ".\noxy.exe" -ArgumentList "noxy_examples\aws_lambda\runtime.nx" -PassThru -NoNewWindow
Start-Sleep -Seconds 2

# 4. Disparar um Evento
Write-Host "3. Disparando Evento de Teste..." -ForegroundColor Green
./noxy.exe noxy_examples/aws_lambda/trigger.nx

Write-Host "`nSimulação rodando! O Runtime deve ter processado o evento." -ForegroundColor Cyan
Write-Host "Pressione ENTER para encerrar tudo..."
Read-Host

# Limpeza
Stop-Process -Id $mockProcess.Id -ErrorAction SilentlyContinue
Stop-Process -Id $runtimeProcess.Id -ErrorAction SilentlyContinue
Stop-Process -Name "noxy" -ErrorAction SilentlyContinue
Write-Host "Encerrado."
