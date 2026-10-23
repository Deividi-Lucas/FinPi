# Script de Deploy Automatizado (1-Click Update para Raspberry Pi)
param (
    [string]$PiIP = ""
)

# Se não passar o IP, pede uma vez no terminal
if (-not $PiIP) {
    $PiIP = Read-Host "Informe o IP do Raspberry Pi (ex: 192.168.1.100)"
}

if (-not $PiIP) {
    Write-Host "IP obrigatorio para realizar o deploy." -ForegroundColor Red
    exit 1
}

$User = "deividi"

Write-Host "=== 1. Compilando nova versao para ARM64 ===" -ForegroundColor Cyan
& .\build-arm64.ps1

if ($LASTEXITCODE -ne 0) {
    Write-Host "Falha na compilacao. Deploy cancelado." -ForegroundColor Red
    exit 1
}

Write-Host "`n=== 2. Enviando novo binario para o Raspberry Pi ===" -ForegroundColor Cyan
scp open-finance "$($User)@$($PiIP):/home/$($User)/open-finance_new"

if ($LASTEXITCODE -ne 0) {
    Write-Host "Falha ao enviar arquivo via SCP." -ForegroundColor Red
    exit 1
}

Write-Host "`n=== 3. Atualizando e reiniciando o servico ===" -ForegroundColor Cyan
# Se o serviço for finpi.service ou open-finance.service, atualiza o binário em uso
ssh "$($User)@$($PiIP)" "if [ -d /opt/open-finance ]; then sudo mv /home/$($User)/open-finance_new /opt/open-finance/open-finance && sudo chmod +x /opt/open-finance/open-finance && sudo systemctl restart open-finance; else sudo mv /home/$($User)/open-finance_new /opt/finpi/finpi && sudo chmod +x /opt/finpi/finpi && sudo systemctl restart finpi; fi"

if ($LASTEXITCODE -eq 0) {
    Write-Host "`nDeploy concluido com sucesso! O FinPi ja esta atualizado e rodando." -ForegroundColor Green
    Write-Host "Acesse em: http://$($PiIP):8080" -ForegroundColor Yellow
} else {
    Write-Host "Ocorreu um erro ao reiniciar o servico no Raspberry Pi." -ForegroundColor Red
}
