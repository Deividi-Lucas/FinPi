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
│   │   ├── sqlite.go           # Otimizações de WAL, pragmas, migração (inclui o livro Pessoal), escopo por livro (`ForBook`) e queries SQLite
│   │   ├── categories.go       # Categorias (por livro): criar, renomear, ocultar e excluir
│   │   ├── books.go            # API de livros: criar, copiar categorias, editar e arquivar
│   │   ├── accounts.go         # Contas por livro: CRUD, migração por banco e conta nos lançamentos
│   │   ├── move.go             # Mover lançamentos entre livros (prévia, categoria no destino, desfazer)
│   │   ├── transfer.go         # Ligações de transferência entre livros e lançamentos "fora do FinPi"
│   │   └── sqlite_test.go      # Testes do banco (centavos, migração, exclusão)
│   ├── handlers/
│   │   ├── handlers.go         # Rotas HTTP e formatação BRL
│   │   ├── categories.go       # Rotas do menu de categorias
│   │   ├── books.go            # Rotas dos livros (/livros, /livros/editar, /livros/arquivar)
│   │   ├── accounts.go         # Rotas das contas (/contas, /contas/editar, /contas/arquivar)
│   │   ├── overview.go         # Visão geral (/visao-geral): totais entre livros sem as transferências ligadas
│   │   ├── transfers.go        # Revisão das transferências (/transferencias/ligar, desligar, fora, voltar)
│   │   ├── book_scope.go       # Resolve o livro do pedido (?livro=) e escopa os handlers
│   │   ├── events.go           # Endpoint /events (SSE) para atualização em tempo real
│   │   ├── broker.go           # Distribui avisos de mudança às telas conectadas
│   │   ├── validation.go       # Validação compartilhada do formulário de lançamentos
│   │   ├── validation_test.go  # Testes de validação
│   │   └── handlers_test.go    # Testes dos handlers e do template
│   ├── dashboard/
│   │   └── dashboard.go        # Cálculo puro do dashboard do mês (maior/menor gasto, essencial × supérfluo)
│   ├── statement/
│   │   ├── parse.go            # Leitura do extrato (cabeçalho, datas, valores) e reconhecimento do banco
│   │   ├── csv.go              # Leitor de CSV (separador e codificação)
│   │   ├── xlsx.go             # Leitor de XLSX (biblioteca padrão, sem dependências)
│   │   ├── banks.go            # Detecção do cabeçalho e leitura das linhas do Nubank (CSV) e do Sicredi
│   │   ├── bb.go               # Leitura do CSV do Banco do Brasil (ignora saldos) e limpeza da descrição
│   │   ├── clean.go            # Limpeza da descrição (sem CPF/CNPJ, banco, agência e conta); guarda o texto original
│   │   ├── ofx.go              # Leitor de OFX (extrato do Nubank)
│   │   └── preview.go          # Revisão: situação de cada linha, categoria sugerida, tipos conhecidos e identificador de origem por banco
│   ├── installments/
│   │   ├── parse.go            # Lê a parcela da descrição (3/10, parcela 3 de 10)
│   │   ├── project.go          # Projeta as parcelas previstas dos meses seguintes
│   │   ├── annotate.go         # Selo e restante dos lançamentos parcelados
│   │   └── partial.go          # Detecta séries de parcelas só em parte selecionadas (aviso ao mover)
│   ├── bulk/
│   │   └── bulk.go             # Cálculo das edições em lote (categoria, prioridade, data e descrição)
│   ├── report/
│   │   ├── flow.go             # Fatias do fluxo de caixa (dia, semana, mês) e saldo acumulado
│   │   ├── recurring.go        # Gastos recorrentes por categoria e por descrição
│   │   └── chart.go            # Geometria do gráfico (SVG gerado no servidor)
│   ├── transfer/
│   │   ├── suggest.go          # Regra pura: valida pares e sugere transferências entre livros
│   │   └── overview.go         # Regra pura: monta os totais da Visão geral entre livros
│   ├── textutil/
│   │   └── fold.go             # Normalização de texto (sem acentos nem maiúsculas)
│   └── models/
│       └── models.go           # Modelos de dados e structs de tela
├── templates/
│   ├── index.html              # Interface web moderna com Tailwind CSS (embutida)
│   └── overview.html           # Página Visão geral e pop-up de revisão das transferências
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

## 📝 Novo lançamento

O botão **Novo lançamento** abre um pop-up. O que você digita é guardado como **rascunho** no próprio navegador:
fechar o pop-up (X, Esc ou clique fora), recarregar a página ou fechar a aba não perde nada. Ao reabrir, os campos
voltam preenchidos com o aviso "Rascunho restaurado" e o botão **Descartar**. O rascunho só some ao enviar com
sucesso ou ao descartar; erros de validação aparecem dentro do pop-up. O rascunho é por navegador (não é
compartilhado entre aparelhos) e, se o navegador bloquear o armazenamento, o pop-up funciona sem ele.

A **edição** de um lançamento tem o mesmo comportamento, com um rascunho **por lançamento** (um pontinho âmbar
no lápis da linha indica onde há alterações não salvas). Se o lançamento for alterado em outra tela depois do
rascunho, o rascunho não é aplicado por cima: a edição abre com os valores atuais e um aviso. Rascunhos de edição
com mais de 30 dias são limpos automaticamente.

---

## 🧾 Parcelas previstas

Escreva o número da parcela na descrição (`Geladeira 3/10` ou `Geladeira parcela 3 de 10`). O FinPi mostra o
selo `3/10`, quanto ainda resta e, nos meses seguintes, um bloco **Parcelas previstas** calculado pelo histórico
(nada é gravado; a previsão some quando você lança a parcela real). Por padrão as previstas ficam fora dos
totais; marque **Incluir previstas nos totais** para somá-las (`?previstas=1`). A parcela deve ficar no fim da
descrição, com `1 <= N <= M <= 60`.

Ao **importar um extrato**, cada linha com o mesmo tipo e valor de uma parcela prevista, com data a até 5 dias
dela (e da mesma conta, quando as duas têm conta), já vem com a descrição da série (`Geladeira 4/10`), a categoria
dela e o selo **parcela prevista 4/10**. Assim a prevista some ao confirmar, sem editar nada. Desmarque a caixa do
selo para voltar à descrição do banco (que fica guardada como descrição original de qualquer forma). Se duas
previstas ou duas linhas puderem casar, nada é sugerido.

---

## 🏷️ Categorias

O botão **Categorias** (no cabeçalho) abre o menu para criar, **renomear** (os lançamentos antigos acompanham),
**ocultar** (some da lista ao lançar, mas continua nos lançamentos antigos) e **excluir** (só categorias sem
lançamentos). Ao lançar, escolha **+ Nova categoria…** para criar uma na hora. Nomes são únicos ignorando
maiúsculas e acentos ("saude" e "Saúde" são a mesma categoria).

---

## 📊 Dashboard do mês

O mês da tela principal muda pelas setas **‹ ›** ao lado do campo de mês (mês anterior e próximo, virando o ano em
dezembro/janeiro) ou escolhendo um mês no próprio campo. As setas mantêm o livro, as previstas e os filtros, e ficam
escondidas com **Todos os meses** ligado.

Abaixo dos cartões de resumo, a seção **Dashboard do mês** mostra a quantidade de transações, o **maior** e o
**menor** gasto e a divisão das despesas em **Essencial** (não se vive sem), **Supérfluo** (se vive sem) e **Sem
classificação**. Segue o mês selecionado; parcelas previstas não entram na conta.

A classificação vem da **categoria**: no menu **Categorias**, escolha Essencial ou Supérfluo em cada uma. Para um
gasto que foge da regra, use o campo **Prioridade** ao lançar ou editar (a opção padrão é "Seguir a categoria").
Receitas não são classificadas. Nada é classificado automaticamente: enquanto houver gastos sem classificação, um
aviso na seção leva direto ao menu de categorias. Em empate de valor, o maior/menor gasto é o mais recente.

---

## 🔎 Filtros

A página principal e o **Relatório** têm uma barra de **Filtros**: busca por **texto** (na descrição, ignorando acentos e
maiúsculas; várias palavras valem juntas), **categorias** (várias, com caixas de seleção), **tipo** (todos, receitas ou
despesas) e **faixa de valor** (de/até, inclusive). Tudo se combina e vai na URL, então o link pode ser guardado ou
compartilhado.

- **Página principal:** o filtro age na **lista do histórico** (o mês selecionado; ligue **Todos os meses** para procurar
  em qualquer data, do mais recente ao mais antigo, até 500 lançamentos). Os cartões e o dashboard do mês continuam
  mostrando o mês inteiro. A lista tem um resumo próprio (quantos lançamentos, receitas e despesas). Criar, editar ou
  excluir mantém o filtro.
- **Relatório:** **todos os números** (cartões, fluxo de caixa, recorrentes e receitas) passam a considerar só o que o
  filtro deixa passar, inclusive as parcelas previstas (pela categoria da série e pelo nome). As setas de ano, os botões
  de agrupamento e o interruptor de previstas preservam o filtro.

---

## ✏️ Edição múltipla

No histórico da página principal, marque vários lançamentos (a caixa do cabeçalho seleciona toda a lista, que já
respeita os filtros) e use **Editar…** ou **Excluir**. Dá para mudar **categoria**, **prioridade**, **data** (definir uma
data ou deslocar por dias/meses; o dia que não existir vai para o fim do mês) e **descrição** (localizar e substituir um
trecho, sem diferenciar maiúsculas; o resultado precisa ter de 1 a 200 caracteres, senão nada é alterado). Antes de gravar,
uma **prévia** mostra cada lançamento em "antes → depois" e deixa desmarcar linhas; só **Aplicar** grava, numa única
transação. Valor e tipo não mudam. Até 500 lançamentos por vez.

**Desfazer:** cada edição ou exclusão em lote guarda o estado anterior (as 10 últimas, por 30 dias). O botão **Desfazer**
restaura a mais recente; o que você alterou depois dela fica como está e é avisado. Desfazer uma exclusão recria o
lançamento com o mesmo ID.

---

## 📈 Relatório

O botão **Relatório** abre `/relatorio`: escolha um **ano** (setas ‹ ›) ou um **período** (de/até, até 5 anos). A página
mostra entradas, saídas, resultado e a média de saídas por mês; o **fluxo de caixa** (barras de entradas e saídas e a
linha do saldo acumulado, que começa em zero no início do período) agrupado automaticamente por dia, semana ou mês
(botões para trocar); e os **gastos recorrentes** por categoria e por descrição (parcelas agrupadas), com quantas
vezes se repetiram e quanto somaram. **Incluir previstas** soma as parcelas futuras, marcadas como previstas. A página
só lê dados e se atualiza em tempo real, como a principal; a URL guarda o período e pode ser compartilhada.

A seção **Receitas** mostra de onde veio o dinheiro que entrou no período: quantas **fontes** (descrições) geraram receita,
a **maior fonte** e quanto ela pesa nas entradas (dependência), em quantos **meses** houve receita (regularidade) e a
receita média por mês. A tabela lista cada fonte por **Descrição** (todas, inclusive as de uma só entrada) ou por
**Categoria**, com vezes, total, média, % das entradas, meses com entrada e a data da última. Só lançamentos reais
entram (as parcelas previstas ficam de fora).

---

## 📚 Livros

**Livros** são conjuntos separados de lançamentos e categorias (por exemplo **Pessoal** e **PJ**). Todo o resto vale
**por livro**: dashboard, relatório, filtros, parcelas, edição múltipla e desfazer só enxergam o livro aberto.

- **Seletor:** no cabeçalho, cada livro tem uma **cor**; o livro aberto vai na URL (`?livro=`) e, sem ela, abre o
  primeiro livro ativo (o de menor id, normalmente o **Pessoal**).
- **Gerenciar** (`/livros`, `/livros/editar`, `/livros/arquivar`): criar um livro **vazio** ou **copiando as categorias**
  de outro, **renomear**, trocar a **cor** e **arquivar/reativar**. Livros nunca são excluídos e sempre há pelo menos um
  livro ativo. Arquivar o livro aberto leva ao primeiro livro ativo
  (o de menor id, normalmente o Pessoal).
- **Categorias por livro:** cada livro tem as suas (nomes únicos dentro do livro); criar "Impostos" no PJ não cria no
  Pessoal.
- **Mover lançamentos:** na edição múltipla, **Mover…** escolhe o livro de destino (`/bulk/...` com `acao=mover`). A
  **prévia** mostra a categoria de cada lançamento no destino e **(nova)** quando ela será criada; se a seleção
  contém só parte de uma série de parcelas, aparece um aviso. **Desfazer** a movimentação é feito a partir do livro de origem.
- **Importar extrato:** vai para o livro aberto e a tela mostra "Importando para o livro X". A checagem de duplicados pelo
  identificador de origem é **global**: o mesmo extrato não entra duas vezes, nem em outro livro.
- **Migração:** os dados existentes viram o livro **Pessoal** automaticamente na primeira abertura; a tabela de categorias é
  reconstruída numa transação, e o atualizador faz **backup do banco** antes de trocar o binário.

---

## 🏛️ Contas

Dentro de cada livro você pode separar o que foi feito em cada **conta bancária** (Nubank, Sicredi, Mercado Pago…).
A conta é só um **rótulo e um filtro**: os totais do livro, a Visão geral e a checagem de duplicados não mudam.

- **Migração automática:** na primeira atualização, cada livro ganha uma conta por banco encontrado nos lançamentos
  importados ("Mercado Pago", "Nubank", "Sicredi", "Banco do Brasil") e os importados entram nela. Os lançamentos
  manuais ficam em **Sem conta**.
- **Gerenciar** (`/contas`, `/contas/editar`, `/contas/arquivar`): na tela de **Livros**, cada livro lista as suas contas
  e permite criar, renomear e arquivar (nunca apaga; o lançamento antigo mantém o selo). O nome é único dentro do livro.
- **Importar extrato:** a prévia mostra **Conta**, já escolhida pelo banco do arquivo (criada na confirmação se ainda não
  existir). Dá para trocar por outra conta do livro ou criar uma **Nova conta…** (por exemplo, uma segunda conta do mesmo
  banco). Reimportar o mesmo extrato em outra conta do mesmo livro continua sendo detectado como "Já importado".
- **No lançamento:** o campo **Conta** (opcional) está no novo lançamento e na edição; editar sem mexer na conta
  preserva a conta. Na **edição múltipla**, **Definir conta…** atribui a conta (ou "Sem conta") a vários de uma vez,
  com desfazer.
- **Filtro e selo:** o filtro **Conta** (multisseleção, com "Sem conta") vale para o histórico e para todos os números do
  relatório (`conta=ID` na URL). Em **Todos os livros** ele lista as contas de todos os livros, com o nome do livro. O
  histórico mostra a conta como selo ao lado da categoria.
- **Mover entre livros:** o lançamento importado vai para a conta do **mesmo banco** no livro de destino (criada se faltar);
  o manual continua sem conta. O **desfazer** restaura a conta de origem.
- **Limitações:** a Visão geral não tem filtro por conta, e a ligação de transferências ainda só pega livros diferentes
  (uma transferência entre duas contas do mesmo livro precisa ser marcada como Movimentação interna).

---

## 🔁 Visão geral e transferências entre livros

**O problema:** um Pix do Pessoal para a PJ aparece como **despesa** num livro e como **receita** no outro. Somando os
livros, o mesmo dinheiro conta duas vezes.

- **Visão geral** (`/visao-geral`): botão na tela principal e na tela do Relatório. Escolha um **mês** ou um **ano** (setas ‹ ›).
  Os totais somam todos os livros ativos **sem os pares ligados** e sem a **Movimentação interna**. A tabela por livro
  tem as linhas **Transferências entre livros** e **Fora do FinPi**, e a linha **Total** bate com os cartões da própria Visão geral (cada livro mantém os seus cartões).
- **Revisão:** o pop-up tem abas. **Sugeridos** são pares de valor igual, com até 3 dias de diferença e em livros
  diferentes; nada é ligado sem a sua confirmação (**Ligar**, ou **Descartar** para esconder a sugestão neste
  navegador). **Ligados** lista os pares e permite **Desfazer** (`/transferencias/ligar`, `/transferencias/desligar`).
  **Fora do FinPi** guarda lançamentos cujo outro lado está numa conta que não está no FinPi
  (`/transferencias/fora`) e permite **Voltar a contar** (`/transferencias/voltar`). **Ligar à mão** liga dois
  lançamentos marcados que as sugestões não pegaram.
- **Quando a ligação some:** excluir um dos lançamentos apaga a ligação; editar o valor ou o tipo, ou mover um
  lançamento para o mesmo livro do par, a invalida e o par volta como pendência.
- **Cada livro continua somando a transferência normalmente:** cartões, dashboard e relatório do livro não mudam; só a
  Visão geral desconta os pares ligados.
- **Livro arquivado:** as ligações dele ficam fora da Visão geral e o lado ativo volta a contar normalmente.
- **Selos nas listas:** o histórico mostra **↔ livro do outro lado** nos lançamentos ligados e **fora do FinPi** nos
  marcados.
- **Relatório de todos os livros:** no seletor de livro do **Relatório** há a opção **Todos os livros** (`/relatorio?livro=todos`).
  Fluxo de caixa, recorrentes e receitas por fonte somam os livros ativos **sem os pares ligados**, sem os marcados
  "fora do FinPi" e sem a Movimentação interna; duas linhas abaixo dos cartões mostram o que foi tirado. O filtro de
  categorias junta as dos livros pelo nome (marcar "Outros" pega o "Outros" de cada livro) e as parcelas previstas vêm
  de todos os livros. Com um livro escolhido, o relatório é o de sempre.
- **Limitação:** só liga lançamentos com **valor igual**.

---

## 🏦 Importar extrato

O botão **Importar extrato** lê extratos de quatro bancos, reconhecidos **pelo conteúdo do arquivo** (não pelo nome):

- **Mercado Pago:** `.xlsx` ou `.csv`, **sem abrir e salvar no Excel** (o Excel destrói o identificador das transações e o sistema recusa o arquivo).
- **Nubank:** **OFX** ou **CSV**. O mesmo extrato em OFX e em CSV não duplica (identificador `nu:<uuid>`).
- **Sicredi:** planilha **XLSX**, mesmo quando o arquivo se chama `.csv` (identificador `sic:<identificador>|<data>|<centavos>`).
- **Banco do Brasil:** **CSV** do extrato da conta corrente. O identificador é `bb:<documento>|<data>|<centavos>`, porque o número do documento se repete entre lançamentos diferentes; as linhas de saldo (Saldo Anterior, Saldo do dia, S A L D O) são ignoradas.

Nada é gravado antes da **revisão**: cada linha mostra a categoria sugerida pelo histórico (você pode trocar; as linhas
com a mesma descrição acompanham), e uma situação: *Novo*, *Já importado*, *Possível duplicado* (já existe um
lançamento seu com a mesma data, valor e tipo) ou *Tipo novo* (tipo de transação ainda não conhecido pelo banco do
arquivo, que vem sem marcação). Só as linhas *Novas* vêm marcadas. Os tipos conhecidos são:

- **Nubank:** Pix recebido/enviado, Transferência recebida/enviada, Pagamento de boleto, Pagamento de fatura, Resgate RDB e Aplicação RDB.
- **Sicredi:** Pix recebido/enviado, Pagamento de boleto, Débito automático, Integralização de capital, Compra no débito, Recarga de celular, Transferência conta salário e Devolução Pix.
- **Banco do Brasil:** Pix recebido/enviado, Transferência recebida/enviada, TED, TEDinternet e Telefone. Os demais lançamentos aparecem como *Tipo novo*.

Reimportar o mesmo extrato não duplica nada. Fechar o pop-up mantém a revisão; **Descartar** ou recarregar a página a apagam.

**Descrição limpa:** o sistema tira da descrição o CPF/CNPJ, o banco, a agência e a conta (fica, por exemplo,
"Pix enviado Maria Souza"). O texto original do banco é guardado na coluna `original_description` do lançamento e
aparece ao passar o mouse sobre a descrição.

**Movimentação interna:** *Resgate RDB* e *Aplicação RDB* vão (já marcados) para a categoria **Movimentação interna**,
que fica **fora dos totais** de receitas e despesas (cartões, estatísticas, relatório e fontes de receita), em
qualquer livro. Elas aparecem numa linha separada, "Movimentações internas (fora dos totais)", e com o selo
**interna** na lista. Um livro sem categorias (um PJ novo, por exemplo) recebe "Outros" e "Movimentação interna" na
revisão, com "(será criada)", e as cria ao confirmar. A importação vai para o livro aberto.

Pix entre as suas próprias contas ou livros ainda contam como despesa/receita: desmarque-os na revisão até que uma
versão futura ligue as transferências entre livros.

Guarde os arquivos reais em `extratos/` (ignorada pelo git).

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
