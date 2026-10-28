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
scp finpi "$($User)@$($PiIP):/home/$($User)/finpi_new"

if ($LASTEXITCODE -ne 0) {
    Write-Host "Falha ao enviar arquivo via SCP." -ForegroundColor Red
    exit 1
}

Write-Host "`n=== 3. Atualizando e reiniciando o servico ===" -ForegroundColor Cyan
# -t permite digitar a senha do sudo para reiniciar o serviço
ssh -t "$($User)@$($PiIP)" "sudo mv /home/$($User)/finpi_new /opt/finpi/finpi && sudo chmod +x /opt/finpi/finpi && sudo systemctl restart finpi"

if ($LASTEXITCODE -eq 0) {
    Write-Host "`nDeploy concluido com sucesso! O FinPi ja esta atualizado e rodando." -ForegroundColor Green
    Write-Host "Acesse em: http://$($PiIP):8080" -ForegroundColor Yellow
} else {
    Write-Host "Ocorreu um erro ao reiniciar o servico no Raspberry Pi." -ForegroundColor Red
}
