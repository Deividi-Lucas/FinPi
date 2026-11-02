package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"finpi/internal/database"
	"finpi/internal/models"
)

// mustAccTx grava um lançamento com conta (0 = sem conta).
func mustAccTx(t *testing.T, db *database.DB, desc, cat, typ string, cents int64, date string, account int64) {
	t.Helper()
	tx := models.Transaction{Description: desc, Category: cat, Type: typ, AmountCents: cents, Date: date, AccountID: account}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
}

func mustAccount(t *testing.T, db *database.DB, name string) models.Account {
	t.Helper()
	a, err := db.CreateAccount(name, "")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// seedAccounts: Carteira (Mercado 10,00), Cofre (Cinema 20,00) e sem conta (Feira 40,00), em outubro de 2026.
func seedAccounts(t *testing.T, db *database.DB) (carteira, cofre models.Account) {
	t.Helper()
	carteira, cofre = mustAccount(t, db, "Carteira"), mustAccount(t, db, "Cofre")
	mustAccTx(t, db, "Mercado", "Alimentação", "expense", 1000, "2026-10-05", carteira.ID)
	mustAccTx(t, db, "Cinema", "Lazer", "expense", 2000, "2026-10-06", cofre.ID)
	mustAccTx(t, db, "Feira", "Alimentação", "expense", 4000, "2026-10-07", 0)
	return carteira, cofre
}

func historySummary(body string) string {
	return slice(body, `id="history-summary"`, `</p>`)
}

func TestHistory_FilterByAccount(t *testing.T) {
	h, db := newTestHandler(t)
	carteira, cofre := seedAccounts(t, db)

	cases := map[string]struct{ query, want string }{
		"uma conta":      {"conta=" + idStr(carteira.ID), "1 lançamento &middot; receitas R$ 0,00 &middot; despesas R$ 10,00"},
		"sem conta":      {"conta=0", "1 lançamento &middot; receitas R$ 0,00 &middot; despesas R$ 40,00"},
		"conta ou nada":  {"conta=" + idStr(cofre.ID) + "&conta=0", "2 lançamentos &middot; receitas R$ 0,00 &middot; despesas R$ 60,00"},
		"sem filtro":     {"", "3 lançamentos &middot; receitas R$ 0,00 &middot; despesas R$ 70,00"},
		"com categoria":  {"conta=" + idStr(carteira.ID) + "&conta=0&cat=" + url.QueryEscape("Alimentação"), "2 lançamentos &middot; receitas R$ 0,00 &middot; despesas R$ 50,00"},
		"conta inválida": {"conta=abc", "3 lançamentos &middot; receitas R$ 0,00 &middot; despesas R$ 70,00"},
	}
	for name, c := range cases {
		body := getPage(h, "/?month=2026-10&"+c.query)
		if got := historySummary(body); !strings.Contains(got, c.want) {
			t.Errorf("%s: resumo %q, quer %q", name, got, c.want)
		}
	}

	body := getPage(h, "/?month=2026-10&conta=abc")
	if !strings.Contains(body, "Conta inválida ignorada.") {
		t.Error("conta inválida deveria avisar")
	}
	body = getPage(h, "/?month=2026-10&conta="+idStr(carteira.ID))
	bar := slice(body, `id="filters-form"`, `</form>`)
	if !strings.Contains(bar, "1 filtro ativo") {
		t.Error("o filtro de conta conta como um filtro ativo")
	}
}

func TestHistory_AccountMenuListsBookAccounts(t *testing.T) {
	h, db := newTestHandler(t)
	carteira, cofre := seedAccounts(t, db)
	velhaEmUso := mustAccount(t, db, "Velha em uso")
	mustAccTx(t, db, "Antigo", "Outros", "expense", 100, "2025-01-01", velhaEmUso.ID)
	_ = db.SetAccountArchived(velhaEmUso.ID, true)
	velhaSemUso := mustAccount(t, db, "Velha sem uso")
	_ = db.SetAccountArchived(velhaSemUso.ID, true)
	pj, _ := newPJ(t, db)
	_, _ = pj.CreateAccount("Caixa da Empresa", "")

	body := getPage(h, "/?month=2026-10&conta="+idStr(cofre.ID)+"&conta=0")
	bar := slice(body, `id="filters-form"`, `</form>`)
	for _, want := range []string{
		`name="conta" value="` + idStr(carteira.ID) + `"`,
		`name="conta" value="` + idStr(cofre.ID) + `" checked`,
		`name="conta" value="0" checked`,
		`name="conta" value="` + idStr(velhaEmUso.ID) + `"`,
		"Sem conta", "Velha em uso", "Conta (2)",
	} {
		if !strings.Contains(bar, want) {
			t.Errorf("menu Conta sem %q", want)
		}
	}
	if strings.Contains(bar, `name="conta" value="`+idStr(carteira.ID)+`" checked`) {
		t.Error("Carteira não está marcada")
	}
	for _, not := range []string{"Velha sem uso", "Caixa da Empresa"} {
		if strings.Contains(bar, not) {
			t.Errorf("o menu de um livro não deveria ter %q", not)
		}
	}
}

func TestHistory_AccountFilterTravelsWithFormsAndRedirects(t *testing.T) {
	h, db := newTestHandler(t)
	carteira, _ := seedAccounts(t, db)
	id := idStr(carteira.ID)
	body := getPage(h, "/?month=2026-10&conta="+id+"&conta=0")

	if !strings.Contains(body, `<input type="hidden" name="conta" value="`+id+`"><input type="hidden" name="conta" value="0">`) {
		t.Error("o seletor de mês (filterHidden) deveria repetir as contas")
	}
	if !strings.Contains(body, `name="filtros" value="conta=`+id+`&amp;conta=0"`) {
		t.Error("os formulários de gravação (filtros) deveriam levar as contas")
	}

	form := validForm()
	form.Set("filtros", "conta="+id+"&conta=0")
	rec := post(h.CreateTransaction, "/transactions", form)
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "conta="+id+"&conta=0") {
		t.Errorf("o redirecionamento deveria manter as contas: %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestReport_FilterByAccountRecalculatesEverything(t *testing.T) {
	h, db := newTestHandler(t)
	carteira := mustAccount(t, db, "Carteira")
	cofre := mustAccount(t, db, "Cofre")
	mustAccTx(t, db, "Salário", "Salário", "income", 500000, "2026-01-05", carteira.ID)
	mustAccTx(t, db, "Salário", "Salário", "income", 500000, "2026-02-05", carteira.ID)
	mustAccTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-01-10", carteira.ID)
	mustAccTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-02-10", carteira.ID)
	mustAccTx(t, db, "Aluguel", "Moradia", "expense", 90000, "2026-02-01", cofre.ID)
	mustAccTx(t, db, "Freela", "Outros", "income", 30000, "2026-03-03", cofre.ID)
	mustAccTx(t, db, "Cinema", "Lazer", "expense", 4500, "2026-03-03", 0)

	_, body := getReport(h, "/relatorio?ano=2026&conta="+idStr(carteira.ID))
	cards := slice(body, `<section class="mt-6 grid`, `<!-- Fluxo de caixa`)
	if !strings.Contains(cards, "R$ 10.000,00") || !strings.Contains(cards, "R$ 240,00") || strings.Contains(cards, "R$ 10.300,00") {
		t.Errorf("cartões só da Carteira (entradas 10.000, saídas 240): %s", cards)
	}
	rec := panel(body, "panel-categoria")
	if !strings.Contains(rec, "Alimentação") || strings.Contains(rec, "Moradia") || strings.Contains(rec, "Lazer") {
		t.Error("os recorrentes deveriam ter só os da Carteira")
	}
	incomes := slice(body, `id="incomes"`, `id="panel-`)
	if !strings.Contains(incomes, "Salário") || strings.Contains(incomes, "Freela") {
		t.Error("as receitas deveriam ser só as da Carteira")
	}

	_, body = getReport(h, "/relatorio?ano=2026&conta=0")
	cards = slice(body, `<section class="mt-6 grid`, `<!-- Fluxo de caixa`)
	if !strings.Contains(cards, "R$ 45,00") || strings.Contains(cards, "R$ 240,00") {
		t.Errorf("Sem conta: só o Cinema (45,00): %s", cards)
	}

	_, body = getReport(h, "/relatorio?ano=2026")
	cards = slice(body, `<section class="mt-6 grid`, `<!-- Fluxo de caixa`)
	if !strings.Contains(cards, "R$ 10.300,00") || !strings.Contains(cards, "R$ 1.185,00") {
		t.Errorf("sem conta= o relatório fica igual: %s", cards)
	}

	code, body := getReport(h, "/relatorio?ano=2026&conta=-1")
	if code != http.StatusOK || !strings.Contains(body, "Conta inválida ignorada.") || !strings.Contains(slice(body, `<section class="mt-6 grid`, `<!-- Fluxo de caixa`), "R$ 10.300,00") {
		t.Errorf("conta inválida avisa e é ignorada: %d", code)
	}
}

func TestReport_FilterProjectedInstallmentsByAccount(t *testing.T) {
	h, db := newTestHandler(t)
	carteira := mustAccount(t, db, "Carteira")
	cofre := mustAccount(t, db, "Cofre")
	mustAccTx(t, db, "Geladeira 3/10", "Moradia", "expense", 25000, "2026-10-15", carteira.ID)

	_, on := getReport(h, "/relatorio?ano=2026&previstas=1&conta="+idStr(carteira.ID))
	if n := strings.Count(on, "data-projected"); n != 2 {
		t.Errorf("previstas da série da Carteira = %d fatias, quer 2", n)
	}
	for _, q := range []string{"conta=" + idStr(cofre.ID), "conta=0"} {
		_, off := getReport(h, "/relatorio?ano=2026&previstas=1&"+q)
		if strings.Contains(off, "data-projected") {
			t.Errorf("%s: as previstas herdam a conta da série", q)
		}
	}
}

func TestReport_AccountFilterKeepsLinksAndForms(t *testing.T) {
	h, db := newTestHandler(t)
	carteira, _ := seedAccounts(t, db)
	id := idStr(carteira.ID)
	_, body := getReport(h, "/relatorio?ano=2026&conta="+id+"&conta=0")

	for _, want := range []string{
		`href="/relatorio?ano=2025&amp;conta=` + id + `&amp;conta=0"`,                     // seta do ano
		`agrupar=mes&amp;ano=2026&amp;conta=` + id + `&amp;conta=0`,                       // agrupamento
		`data-url="/relatorio?ano=2026&amp;conta=` + id + `&amp;conta=0&amp;previstas=1"`, // previstas
	} {
		if !strings.Contains(body, want) {
			t.Errorf("link sem as contas: falta %s", want)
		}
	}
	period := slice(body, `id="period-form"`, `</form>`)
	if !strings.Contains(period, `name="conta" value="`+id+`"`) || !strings.Contains(period, `name="conta" value="0"`) {
		t.Error("o formulário de período deveria levar as contas")
	}
	bar := slice(body, `id="filters-form"`, `</form>`)
	if !strings.Contains(bar, `name="conta" value="`+id+`" checked`) || !strings.Contains(bar, "1 filtro ativo") {
		t.Error("a barra de filtros deveria mostrar a conta marcada")
	}
	if !strings.Contains(bar, `href="/relatorio?ano=2026"`) {
		t.Error("Limpar volta ao mesmo período sem filtros (inclusive as contas)")
	}
}

// seedAccountsTwoBooks: Pessoal (Carteira: Salário 10.000 e Pix para PJ 3.000; sem conta: Mercado 2.000) e
// PJ (Caixa: Pix do Pessoal 3.000; Banco PJ: DAS 1.000).
func seedAccountsTwoBooks(t *testing.T, db *database.DB) (pj *database.DB, carteira, caixa, bancoPJ models.Account) {
	t.Helper()
	pj, _ = newPJ(t, db)
	carteira = mustAccount(t, db, "Carteira")
	caixa, bancoPJ = mustAccount(t, pj, "Caixa"), mustAccount(t, pj, "Banco PJ")
	mustAccTx(t, db, "Salário", "Salário", "income", 1000000, "2026-10-05", carteira.ID)
	mustAccTx(t, db, "Mercado", "Alimentação", "expense", 200000, "2026-10-06", 0)
	mustAccTx(t, db, "Pix para PJ", "Outros", "expense", 300000, "2026-10-07", carteira.ID)
	mustAccTx(t, pj, "Pix do Pessoal", "Outros", "income", 300000, "2026-10-07", caixa.ID)
	mustAccTx(t, pj, "DAS", "Impostos", "expense", 100000, "2026-10-08", bancoPJ.ID)
	return pj, carteira, caixa, bancoPJ
}

func TestReport_AllBooks_FilterByAccountsOfDifferentBooks(t *testing.T) {
	h, db := newTestHandler(t)
	_, carteira, caixa, bancoPJ := seedAccountsTwoBooks(t, db)

	// Carteira (Pessoal) + Banco PJ (PJ): entradas 10.000, saídas 3.000 + 1.000
	_, body := getReport(h, "/relatorio?ano=2026&livro=todos&conta="+idStr(carteira.ID)+"&conta="+idStr(bancoPJ.ID))
	cards := slice(body, `<section class="mt-6 grid`, `<!-- Fluxo de caixa`)
	if !strings.Contains(cards, "R$ 10.000,00") || !strings.Contains(cards, "R$ 4.000,00") || strings.Contains(cards, "R$ 13.000,00") {
		t.Errorf("só as contas escolhidas dos dois livros: %s", cards)
	}

	// Sem conta em Todos os livros: só o Mercado
	_, body = getReport(h, "/relatorio?ano=2026&livro=todos&conta=0")
	cards = slice(body, `<section class="mt-6 grid`, `<!-- Fluxo de caixa`)
	if !strings.Contains(cards, "R$ 2.000,00") || strings.Contains(cards, "R$ 10.000,00") {
		t.Errorf("Sem conta em Todos os livros: %s", cards)
	}

	// Transferência ligada: as somas de excluídos respeitam o filtro de conta
	ids := txIDs(t, db)
	if err := db.LinkTransfer(ids["Pix para PJ"], ids["Pix do Pessoal"]); err != nil {
		t.Fatal(err)
	}
	_, body = getReport(h, "/relatorio?ano=2026&livro=todos&conta="+idStr(caixa.ID))
	if !strings.Contains(body, "Transferências entre livros (fora do fluxo): entradas R$ 3.000,00 &middot; saídas R$ 0,00") {
		t.Errorf("com a Caixa, só a entrada ligada entra nas excluídas: %s", slice(body, "Transferências entre livros", "</p>"))
	}
	_, body = getReport(h, "/relatorio?ano=2026&livro=todos&conta="+idStr(bancoPJ.ID))
	if strings.Contains(body, "Transferências entre livros") {
		t.Error("com o Banco PJ, nenhuma transferência ligada passa pelo filtro")
	}
}

func TestReport_AllBooks_AccountMenuShowsBookNames(t *testing.T) {
	h, db := newTestHandler(t)
	_, carteira, caixa, _ := seedAccountsTwoBooks(t, db)
	arch, _ := db.CreateBook("Antigo", "rose", 0)
	_, _ = db.ForBook(arch.ID).CreateAccount("Conta do Antigo", "")
	_ = db.SetBookArchived(arch.ID, true)

	_, body := getReport(h, "/relatorio?ano=2026&livro=todos&conta="+idStr(caixa.ID))
	bar := slice(body, `id="filters-form"`, `</form>`)
	for _, want := range []string{
		`name="conta" value="` + idStr(carteira.ID) + `"`,
		`name="conta" value="` + idStr(caixa.ID) + `" checked`,
		"Carteira", "Pessoal", "Caixa", "PJ", "Banco PJ", "Sem conta",
	} {
		if !strings.Contains(bar, want) {
			t.Errorf("menu Conta de Todos os livros sem %q", want)
		}
	}
	if !strings.Contains(bar, `data-account-book>PJ<`) {
		t.Error("cada conta mostra o nome do livro ao lado")
	}
	if strings.Contains(bar, "Conta do Antigo") {
		t.Error("contas de livros arquivados ficam fora")
	}

	// relatório de um livro: só as contas dele, sem o nome do livro
	_, own := getReport(h, "/relatorio?ano=2026")
	ownBar := slice(own, `id="filters-form"`, `</form>`)
	if !strings.Contains(ownBar, "Carteira") || strings.Contains(ownBar, "Banco PJ") || strings.Contains(ownBar, "data-account-book") {
		t.Errorf("menu do Pessoal: %s", ownBar)
	}
}
