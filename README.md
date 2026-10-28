# 🪙 FinPi

Uma aplicação web minimalista, ultraleve e open source para gerenciamento de finanças pessoais e controle de despesas.

Este projeto foi projetado *by design* para rodar de forma contínua e eficiente em hardwares com recursos computacionais restritos, como o **Raspberry Pi 3 Model B (1 GB de RAM)** em ambiente headless (Ubuntu Server / Raspberry Pi OS Lite).

---

## 🚀 Por que Go + SQLite?

Quando o assunto é servidores domésticos pequenos ou instâncias de baixo custo, o consumo de memória RAM e o desgaste do armazenamento (cartões MicroSD) são os maiores gargalos.

A escolha dessa stack resolve esses dois problemas:

* **Go (Golang)**: Compila para um binário nativo único sem a necessidade de um runtime ou máquina virtual pesada (como Node.js, Java ou Python). Consome apenas **~15MB a 25MB de RAM** em execução.
* **SQLite (Pure Go)**: Banco de dados em um único arquivo de disco local. Não requer um daemon pesado em segundo plano (como MySQL ou PostgreSQL), eliminando o overhead de memória.
* **Sem CGO (`modernc.org/sqlite`)**: Facilita a compilação cruzada (*Cross-Compilation*) direta da sua máquina de desenvolvimento para a arquitetura ARM64 do Raspberry Pi, sem dependência de toolchains C cruzadas.
* **Server-Driven HTML + Tailwind CSS**: Interface limpa, responsiva e reativa renderizada diretamente pelo servidor (via `html/template`), utilizando Tailwind CSS via CDN sem a necessidade de etapas complexas de build no frontend.
* **Proteção contra Desgaste do Cartão SD**: Configurado com `PRAGMA journal_mode = WAL;` e `PRAGMA synchronous = NORMAL;`, reduzindo drasticamente escritas síncronas no flash.

---

## 🛠️ Requisitos e Recursos

* **Processador**: Qualquer CPU ARMv7, ARM64 ou x86_64.
* **Memória RAM**: < 30 MB livres.
* **Armazenamento**: Poucos megabytes para o binário (~11 MB) e o arquivo SQLite.
* **Acesso Remoto Seguro**: Projetado para funcionar perfeitamente integrado ao **Tailscale**, permitindo acesso privado e seguro de qualquer lugar pelo celular ou notebook, sem a necessidade de abrir portas no roteador (*port forwarding*).

---

## 📁 Estrutura do Projeto

```text
finpi/
├── .github/workflows/
│   └── build.yml               # Pipeline de compilação automática no GitHub Actions
├── internal/
│   ├── database/
│   │   ├── sqlite.go           # Otimizações de WAL, pragmas, migração e queries SQLite
│   │   ├── categories.go       # Categorias: criar, renomear, ocultar e excluir
│   │   └── sqlite_test.go      # Testes do banco (centavos, migração, exclusão)
│   ├── handlers/
│   │   ├── handlers.go         # Rotas HTTP e formatação BRL
│   │   ├── categories.go       # Rotas do menu de categorias
│   │   ├── events.go           # Endpoint /events (SSE) para atualização em tempo real
│   │   ├── broker.go           # Distribui avisos de mudança às telas conectadas
│   │   ├── validation.go       # Validação compartilhada do formulário de lançamentos
│   │   ├── validation_test.go  # Testes de validação
│   │   └── handlers_test.go    # Testes dos handlers e do template
│   ├── installments/
│   │   ├── parse.go            # Lê a parcela da descrição (3/10, parcela 3 de 10)
│   │   ├── project.go          # Projeta as parcelas previstas dos meses seguintes
│   │   └── annotate.go         # Selo e restante dos lançamentos parcelados
│   ├── textutil/
│   │   └── fold.go             # Normalização de texto (sem acentos nem maiúsculas)
│   └── models/
│       └── models.go           # Modelos de dados e structs de tela
├── templates/
│   └── index.html              # Interface web moderna com Tailwind CSS (embutida)
├── build-arm64.ps1             # Script para compilação cruzada no Windows
├── deploy.ps1                  # Deploy em 1 clique para o Raspberry Pi
├── finpi.service               # Unidade systemd para produção contínua
├── go.mod                      # Dependências do módulo Go
├── go.sum                      # Checksums criptográficos dos módulos
├── LICENSE                     # Licença MIT
├── Makefile                    # Targets de automação de build
└── main.go                     # Entrypoint com embed.FS e Graceful Shutdown
```

---

## 🧾 Parcelas previstas

Escreva o número da parcela na descrição (`Geladeira 3/10` ou `Geladeira parcela 3 de 10`). O FinPi mostra o
selo `3/10`, quanto ainda resta e, nos meses seguintes, um bloco **Parcelas previstas** calculado pelo histórico
(nada é gravado; a previsão some quando você lança a parcela real). Por padrão as previstas ficam fora dos
totais; marque **Incluir previstas nos totais** para somá-las (`?previstas=1`). A parcela deve ficar no fim da
descrição, com `1 <= N <= M <= 60`.

---

## 🏷️ Categorias

O botão **Categorias** (no cabeçalho) abre o menu para criar, **renomear** (os lançamentos antigos acompanham),
**ocultar** (some da lista ao lançar, mas continua nos lançamentos antigos) e **excluir** (só categorias sem
lançamentos). Ao lançar, escolha **+ Nova categoria…** para criar uma na hora. Nomes são únicos ignorando
maiúsculas e acentos ("saude" e "Saúde" são a mesma categoria).

---

## 📦 Como Compilar e Rodar

### 1. Compilação Local (Para Testes na Máquina de Dev)

Caso queira testar na sua própria máquina (Linux, macOS ou Windows):

```bash
# Baixar dependências
go mod tidy

# Rodar a aplicação diretamente
go run main.go
```
Acesse no navegador: `http://localhost:8080`

Para rodar a verificação estática e os testes:

```bash
go vet ./...
go test ./...
```

> **Tempo real**: a tela se atualiza sozinha quando outro dispositivo cria, edita ou exclui um lançamento (Server-Sent Events em `/events`, sem dependências extras). Se você estiver digitando ou editando, a tela não recarrega: aparece um aviso "Atualizar agora". Após um deploy, a conexão cai e volta, e a tela recarrega para pegar a versão nova. Limite de 32 telas conectadas simultaneamente.

> **Valores em centavos**: os valores são guardados como inteiros (centavos) para evitar erros de arredondamento. Bancos criados por versões anteriores (coluna `amount` em `REAL`) são convertidos automaticamente na primeira inicialização. Faça backup do arquivo `.db` antes de atualizar uma instalação existente.

---

### 2. Compilação Cruzada (Cross-Compilation para Raspberry Pi 3B - ARM64)

Na sua máquina de desenvolvimento (ex: Windows ou Linux x86_64), você pode gerar o binário nativo otimizado para o Raspberry Pi com um único comando:

#### No Windows (PowerShell):
```powershell
$env:GOOS = "linux"
$env:GOARCH = "arm64"
$env:CGO_ENABLED = "0"
go build -ldflags="-s -w" -o finpi .
```
*(Ou execute o script: `.\build-arm64.ps1`)*

#### No Linux / macOS:
```bash
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o finpi .
```

> **Dica**: A flag `-ldflags="-s -w"` remove símbolos de depuração e metadados DWARF, reduzindo o tamanho do binário de ~25 MB para apenas **~11 MB**.

---

### 3. Executando no Raspberry Pi

#### Opção A: Deploy Automático em 1 Clique (Recomendado)
Use o script [`deploy.ps1`](./deploy.ps1) no PowerShell para compilar, enviar e reiniciar o serviço de forma transparente:
```powershell
.\deploy.ps1 -PiIP <IP_DO_RASPBERRY_PI>
```

#### Opção B: Manual via SCP e SSH

1. Envie o arquivo compilado (`finpi`) e a unidade de serviço para o seu Raspberry Pi:
```bash
scp finpi finpi.service deividi@<IP_DO_PI>:/home/deividi/
```

2. No Raspberry Pi, mova para a pasta de produção e configure o systemd:
```bash
# Criar diretórios e mover binário
sudo mkdir -p /opt/finpi/data
sudo mv /home/deividi/finpi /opt/finpi/finpi
sudo chmod +x /opt/finpi/finpi
sudo chown -R deividi:deividi /opt/finpi

# Instalar serviço systemd
sudo mv /home/deividi/finpi.service /etc/systemd/system/finpi.service
sudo systemctl daemon-reload
sudo systemctl enable --now finpi
```

Verifique o status do serviço:
```bash
sudo systemctl status finpi
```

---

## 🔄 Implantação Contínua (CI/CD Automático)

O deploy é *pull-based*: o Raspberry Pi busca sozinho a última release, sem expor nenhuma porta nem guardar segredos no GitHub.

```text
PR aprovado e mergeado na main
   └─> GitHub Actions: vet + testes + build ARM64 ──> Release "build-N" (binário + SHA256SUMS)
                                                          │
   Raspberry Pi (timer a cada 5 min) <────────────────────┘
   └─> baixa, confere checksum, faz backup do banco, troca o binário, reinicia
       └─> se o app não responder: volta o binário e o banco anteriores
```

### Instalando o atualizador no Pi (uma vez)

Copie a pasta `deploy/` para o Pi e execute:

```bash
sudo bash deploy/install-updater.sh
```

Isso instala `finpi-update` em `/usr/local/sbin` e ativa o `finpi-update.timer`. Requisitos no Pi: `curl`, `python3`, `flock` e `sha256sum`.

### Operação

```bash
systemctl list-timers finpi-update.timer     # próxima verificação
journalctl -u finpi-update -n 50 --no-pager  # histórico das atualizações
sudo systemctl start finpi-update            # verificar agora, sem esperar o timer
cat /opt/finpi/VERSION                       # versão instalada
ls /opt/finpi/backups                        # backups automáticos (últimos 14)
```

- Se uma versão falhar e sofrer rollback, ela é marcada em `/opt/finpi/FAILED_VERSION` e **não é tentada de novo**; o próximo merge gera uma nova release.
- O script do atualizador é uma cópia em `/usr/local/sbin`: mudanças em `deploy/finpi-update.sh` só valem depois de rodar `install-updater.sh` de novo.
- O teste do atualizador (`bash deploy/test-finpi-update.sh`) roda no CI a cada PR.

---

## 📄 Licença

Este projeto está sob a licença **MIT**. Sinta-se livre para usar, modificar, estudar ou redistribuir da forma que preferir. Veja o arquivo [LICENSE](./LICENSE) para mais detalhes.
