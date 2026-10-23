# Adiciona o Go ao PATH desta sessao caso o terminal tenha sido aberto antes da instalacao do Go
if (-not (Get-Command "go" -ErrorAction SilentlyContinue)) {
    if (Test-Path "C:\Program Files\Go\bin\go.exe") {
        $env:Path += ";C:\Program Files\Go\bin"
    } elseif (Test-Path "C:\Go\bin\go.exe") {
        $env:Path += ";C:\Go\bin"
    }
}

# Configuracoes de Cross-Compilation para Linux ARM64
$env:GOOS = "linux"
$env:GOARCH = "arm64"
$env:CGO_ENABLED = "0"

Write-Host "Compilando binario estatico para Linux ARM64..." -ForegroundColor Cyan

go build -ldflags="-s -w" -o open-finance .

if ($LASTEXITCODE -eq 0) {
    Copy-Item open-finance finpi -Force
    Write-Host "Sucesso! Binario 'open-finance' gerado para ARM64 (Linux)." -ForegroundColor Green
    Write-Host "Tamanho do binario: $((Get-Item open-finance).Length / 1MB | ForEach-Object { '{0:N2} MB' -f $_ })" -ForegroundColor Cyan
    Write-Host "Para enviar ao Raspberry Pi: scp open-finance deividi@<IP_DO_PI>:/home/deividi/" -ForegroundColor Yellow
} else {
    Write-Host "Erro durante a compilacao." -ForegroundColor Red
}
