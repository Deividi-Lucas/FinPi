package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"finpi/internal/database"
	"finpi/internal/models"
)

func getReport(h *Handler, target string) (int, string) {
	rec := httptest.NewRecorder()
	h.Report(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

func mustTx(t *testing.T, db *database.DB, desc, cat, typ string, cents int64, date string) {
	t.Helper()
	tx := models.Transaction{Description: desc, Category: cat, Type: typ, AmountCents: cents, Date: date}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
}

// seedYear: entradas 10.000,00; saídas 1.185,00 (Mercado duas vezes, Aluguel e Cinema uma).
func seedYear(t *testing.T, db *database.DB) {
	t.Helper()
	mustTx(t, db, "Salário", "Salário", "income", 500000, "2026-01-05")
	mustTx(t, db, "Salário", "Salário", "income", 500000, "2026-02-05")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-01-10")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-02-10")
	mustTx(t, db, "Aluguel", "Moradia", "expense", 90000, "2026-02-01")
	mustTx(t, db, "Cinema", "Lazer", "expense", 4500, "2026-03-03")
}

// panel devolve o trecho do HTML de um painel de recorrentes (até o início do próximo painel).
func panel(body, id string) string {
	marker := `id="` + id + `"`
	start := strings.Index(body, marker)
	if start < 0 {
		return ""
	}
	rest := body[start+len(marker):]
	if end := strings.Index(rest, `id="panel-`); end >= 0 {
		return body[start : start+len(marker)+end]
	}
	return body[start:]
}

func TestReport_DefaultsToCurrentYear(t *testing.T) {
	h, _ := newTestHandler(t)
	code, body := getReport(h, "/relatorio")
	if code != http.StatusOK || !strings.Contains(body, fmt.Sprint(time.Now().Year())) {
		t.Errorf("status=%d, ano atual ausente", code)
	}
}

func TestReport_YearCardsAndMonthlyFlow(t *testing.T) {
	h, db := newTestHandler(t)
	seedYear(t, db)

	code, body := getReport(h, "/relatorio?ano=2026")

	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	for _, want := range []string{"R$ 10.000,00", "R$ 1.185,00", "R$ 8.815,00", "88,2%", "R$ 98,75", "jan/26", "dez/26"} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
	if n := strings.Count(body, "data-bucket"); n != 12 {
		t.Errorf("fatias = %d, quer 12 (ano inteiro por mês)", n)
	}
	for _, want := range []string{"<svg", "saldo acumulado no período (começa em zero)"} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
}

func TestReport_RecurringTabs(t *testing.T) {
	h, db := newTestHandler(t)
	seedYear(t, db)
	_, body := getReport(h, "/relatorio?ano=2026")

	cat := panel(body, "panel-categoria")
	desc := panel(body, "panel-descricao")
	if !strings.Contains(cat, "Alimentação") || !strings.Contains(cat, "Lazer") {
		t.Error("a aba Categoria deveria listar todas as categorias com despesa")
	}
	if !strings.Contains(desc, "Mercado") {
		t.Error("a aba Descrição deveria listar o que se repetiu (Mercado)")
	}
	if strings.Contains(desc, "Cinema") || strings.Contains(desc, "Aluguel") {
		t.Error("a aba Descrição só lista o que se repetiu 2 vezes ou mais")
	}
}

func TestReport_CustomPeriodUsesDays(t *testing.T) {
	h, db := newTestHandler(t)
	seedYear(t, db)
	code, body := getReport(h, "/relatorio?de=2026-02-01&ate=2026-02-28")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if n := strings.Count(body, "data-bucket"); n != 28 {
		t.Errorf("fatias = %d, quer 28 (período curto por dia)", n)
	}
	if !strings.Contains(body, "R$ 5.000,00") {
		t.Error("entradas de fevereiro ausentes")
	}
}

func TestReport_InvalidPeriodsShowWarningAndFallBackToCurrentYear(t *testing.T) {
	h, _ := newTestHandler(t)
	year := time.Now().Year()
	for name, q := range map[string]string{
		"de depois de até": "de=2026-05-01&ate=2026-04-01",
		"mais de 5 anos":   "de=2020-01-01&ate=2026-01-01",
		"data ruim":        "de=2026-13-01&ate=2026-12-01",
		"só um campo":      "de=2026-01-01",
		"ano fora":         "ano=1999",
		"ano texto":        "ano=abc",
	} {
		code, body := getReport(h, "/relatorio?"+q)
		if code != http.StatusOK || !strings.Contains(body, "Período inválido") {
			t.Errorf("%s: status=%d, aviso ausente", name, code)
		}
		for _, want := range []string{fmt.Sprintf(`value="%d-01-01"`, year), fmt.Sprintf(`value="%d-12-31"`, year)} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: campo de data sem %s (não voltou ao ano atual)", name, want)
			}
		}
	}
}

func TestReport_OnlyIncomeKeepsLiveUpdatesAndEmptyPanels(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Salário", "Salário", "income", 500000, "2026-01-05")
	code, body := getReport(h, "/relatorio?ano=2026")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if !strings.Contains(panel(body, "panel-descricao"), "Nenhum gasto se repetiu neste período.") {
		t.Error("painel Descrição sem a mensagem de vazio")
	}
	if !strings.Contains(panel(body, "panel-categoria"), "Sem despesas neste período.") {
		t.Error("painel Categoria sem a mensagem de vazio")
	}
	// Painel vazio não tem <tbody>: o tempo real precisa ser ligado antes de qualquer renderização da interface.
	live := strings.Index(body, "startLiveUpdates(function")
	render := strings.LastIndex(body, "renderRecurring()")
	if live < 0 || render < 0 || live > render {
		t.Errorf("startLiveUpdates (%d) deve vir antes da renderização dos recorrentes (%d)", live, render)
	}
	if !strings.Contains(body, "if (!body) return;") {
		t.Error("renderRecurring deve tolerar painel sem tbody")
	}
}

func TestReport_YearArrowsStopAtBounds(t *testing.T) {
	h, _ := newTestHandler(t)
	_, low := getReport(h, "/relatorio?ano=2000")
	if strings.Contains(low, "ano=1999") || !strings.Contains(low, "ano=2001") {
		t.Error("em 2000 não deve haver link para 1999, mas deve haver para 2001")
	}
	_, high := getReport(h, "/relatorio?ano=2100")
	if strings.Contains(high, "ano=2101") || !strings.Contains(high, "ano=2099") {
		t.Error("em 2100 não deve haver link para 2101, mas deve haver para 2099")
	}
}

func TestParseReportQuery(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name         string
		query        string
		wantFrom     string
		wantTo       string
		wantCustom   bool
		wantWarnPart string
	}{
		{"cinco anos exatos aceito", "de=2026-01-01&ate=2030-12-31", "2026-01-01", "2030-12-31", true, ""},
		{"cinco anos e um dia rejeitado", "de=2026-01-01&ate=2031-01-01", "2026-01-01", "2026-12-31", false, "no máximo 5 anos"},
		{"de/ate têm prioridade sobre ano", "ano=2020&de=2026-03-01&ate=2026-03-31", "2026-03-01", "2026-03-31", true, ""},
		{"ano sozinho", "ano=2024", "2024-01-01", "2024-12-31", false, ""},
		{"padrão é o ano atual", "", "2026-01-01", "2026-12-31", false, ""},
	}
	for _, c := range cases {
		v, _ := url.ParseQuery(c.query)
		q, warn := parseReportQuery(v, now)
		if q.From != c.wantFrom || q.To != c.wantTo || q.Custom != c.wantCustom {
			t.Errorf("%s: got %s..%s custom=%v", c.name, q.From, q.To, q.Custom)
		}
		if (c.wantWarnPart == "") != (warn == "") || !strings.Contains(warn, c.wantWarnPart) {
			t.Errorf("%s: aviso = %q, quer conter %q", c.name, warn, c.wantWarnPart)
		}
	}
}

func TestReport_SingleMonthHidesMonthlyAverage(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-02-10")
	_, one := getReport(h, "/relatorio?de=2026-02-01&ate=2026-02-28")
	if strings.Contains(one, "Média de saídas por mês") {
		t.Error("período de um único mês não deve mostrar a média mensal")
	}
	_, year := getReport(h, "/relatorio?ano=2026")
	if !strings.Contains(year, "Média de saídas por mês") {
		t.Error("o ano completo deve mostrar a média mensal")
	}
}

func TestReport_GranularityOverrideAllowedAndRefused(t *testing.T) {
	h, db := newTestHandler(t)
	seedYear(t, db)

	_, weeks := getReport(h, "/relatorio?de=2026-02-01&ate=2026-02-28&agrupar=semana")
	if n := strings.Count(weeks, "data-bucket"); n != 5 {
		t.Errorf("semanas de fevereiro/2026 = %d, quer 5", n)
	}
	_, refused := getReport(h, "/relatorio?ano=2026&agrupar=dia") // 365 dias: dia não é permitido
	if n := strings.Count(refused, "data-bucket"); n != 12 {
		t.Errorf("agrupar=dia no ano deveria ser ignorado: %d fatias", n)
	}
	_, bogus := getReport(h, "/relatorio?ano=2026&agrupar=xyz")
	if n := strings.Count(bogus, "data-bucket"); n != 12 {
		t.Errorf("agrupar inválido deveria ser ignorado: %d fatias", n)
	}
}

func TestReport_ProjectedInstallmentsOnlyWhenAsked(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Geladeira 3/10", "Casa", "expense", 25000, "2026-10-15")

	_, off := getReport(h, "/relatorio?ano=2026")
	if strings.Contains(off, "data-projected") {
		t.Error("sem previstas não deveria haver fatias previstas")
	}
	_, on := getReport(h, "/relatorio?ano=2026&previstas=1")
	if n := strings.Count(on, "data-projected"); n != 2 {
		t.Errorf("fatias previstas = %d, quer 2 (novembro e dezembro)", n)
	}
	if n := strings.Count(on, "(com previstas)"); n != 4 {
		t.Errorf("rótulo (com previstas) = %d vezes, quer 4 (um por cartão)", n)
	}
	if !strings.Contains(on, "com previstas") || !strings.Contains(on, "R$ 750,00") { // 250 + 250 + 250
		t.Error("totais com previstas ausentes")
	}
	// Parcelas previstas nunca entram nos recorrentes.
	if strings.Contains(panel(on, "panel-descricao"), "Geladeira") {
		t.Error("previstas não devem aparecer nos recorrentes")
	}
}

func TestReport_EmptyPeriod(t *testing.T) {
	h, _ := newTestHandler(t)
	_, body := getReport(h, "/relatorio?ano=2030")
	if !strings.Contains(body, "Sem lançamentos neste período.") {
		t.Error("mensagem de período vazio ausente")
	}
	if strings.Contains(body, "<svg") {
		t.Error("período vazio não deve desenhar gráfico")
	}
}

func TestReport_NavigationLinks(t *testing.T) {
	h, _ := newTestHandler(t)
	_, body := getReport(h, "/relatorio?ano=2026&previstas=1")
	for _, want := range []string{`href="/relatorio?ano=2025&amp;previstas=1"`, `href="/relatorio?ano=2027&amp;previstas=1"`, `href="/"`} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem o link %s", want)
		}
	}
}

func TestReport_LiveUpdateWiring(t *testing.T) {
	h, _ := newTestHandler(t)
	_, body := getReport(h, "/relatorio")
	for _, want := range []string{"new EventSource('/events')", `id="live-update-banner"`, "startLiveUpdates(function", `id="period-form"`} {
		if !strings.Contains(body, want) {
			t.Errorf("relatório sem %q", want)
		}
	}
}

func TestReport_RejectsNonGET(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.Report(rec, httptest.NewRequest(http.MethodPost, "/relatorio", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, quer 405", rec.Code)
	}
}

func TestDashboard_HasReportButton(t *testing.T) {
	h, _ := newTestHandler(t)
	if body := getDashboard(h, "/"); !strings.Contains(body, `href="/relatorio"`) {
		t.Error("página principal sem o botão Relatório")
	}
}

// slice devolve o trecho do HTML que começa em start e termina no primeiro dos marcadores seguintes.
func slice(body, start string, ends ...string) string {
	i := strings.Index(body, start)
	if i < 0 {
		return ""
	}
	rest := body[i+len(start):]
	end := len(rest)
	for _, e := range ends {
		if j := strings.Index(rest, e); j >= 0 && j < end {
			end = j
		}
	}
	return body[i : i+len(start)+end]
}

func incomePanel(body, id string) string {
	return slice(body, `id="`+id+`"`, `id="income-panel-`, `id="panel-`, `</section>`)
}

// seedIncomes: Salário (jan e fev, R$ 5.000,00), Bônus (fev, R$ 2.000,00) e um Pix (mar, R$ 100,00): R$ 12.100,00.
func seedIncomes(t *testing.T, db *database.DB) {
	t.Helper()
	mustTx(t, db, "Salário", "Salário", "income", 500000, "2026-01-05")
	mustTx(t, db, "Salário", "Salário", "income", 500000, "2026-02-05")
	mustTx(t, db, "Bônus", "Outros", "income", 200000, "2026-02-20")
	mustTx(t, db, "Pix recebido Fulano", "Outros", "income", 10000, "2026-03-01")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-01-10")
}

func TestReport_IncomeSectionShowsIndicatorsAndSources(t *testing.T) {
	h, db := newTestHandler(t)
	seedIncomes(t, db)

	code, body := getReport(h, "/relatorio?ano=2026")

	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	section := slice(body, `id="incomes"`, `id="panel-`)
	for _, want := range []string{
		"Receitas", "Fontes", "Maior fonte", "Salário", "82,6%", // 10.000 de 12.100
		"Meses com receita", "3 de 12", "Receita média por mês", "R$ 1.008,33",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("seção Receitas sem %q", want)
		}
	}

	desc := incomePanel(body, "income-panel-descricao")
	for _, want := range []string{"Salário", "Bônus", "Pix recebido Fulano", "R$ 10.000,00", "2 de 12", "05/02/2026"} {
		if !strings.Contains(desc, want) {
			t.Errorf("aba Descrição sem %q", want)
		}
	}
	// Ordem inicial: maior total primeiro.
	if strings.Index(desc, "Salário") > strings.Index(desc, "Bônus") || strings.Index(desc, "Bônus") > strings.Index(desc, "Pix recebido") {
		t.Error("as fontes deveriam vir ordenadas por total")
	}
	cat := incomePanel(body, "income-panel-categoria")
	if !strings.Contains(cat, "Salário") || !strings.Contains(cat, "Outros") || strings.Contains(cat, "Alimentação") {
		t.Error("aba Categoria deveria listar só categorias de receita")
	}
	if !strings.Contains(body, `data-itab="descricao"`) || !strings.Contains(body, `data-itab="categoria"`) {
		t.Error("abas da seção Receitas ausentes")
	}
}

func TestReport_IncomeSectionIgnoresProjectedInstallments(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Venda 1/3", "Outros", "income", 10000, "2026-10-05")

	_, body := getReport(h, "/relatorio?ano=2026&previstas=1")

	desc := incomePanel(body, "income-panel-descricao")
	if !strings.Contains(desc, "R$ 100,00") || strings.Contains(desc, "R$ 200,00") || strings.Contains(desc, "R$ 300,00") {
		t.Errorf("as parcelas previstas não entram nas receitas: %s", desc)
	}
}

func TestReport_IncomeSectionEmptyWhenOnlyExpenses(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-01-10")

	code, body := getReport(h, "/relatorio?ano=2026")

	if code != http.StatusOK || !strings.Contains(slice(body, `id="incomes"`, `id="panel-`), "Nenhuma receita neste período.") {
		t.Error("seção Receitas deveria avisar que não há receitas")
	}
	if strings.Contains(body, "data-irow") {
		t.Error("sem receitas não deve haver tabela de fontes")
	}
}

func TestReport_IncomeSectionSingleMonthHidesMonthlyIndicators(t *testing.T) {
	h, db := newTestHandler(t)
	seedIncomes(t, db)

	_, body := getReport(h, "/relatorio?de=2026-02-01&ate=2026-02-28")

	section := slice(body, `id="incomes"`, `id="panel-`)
	if strings.Contains(section, "Meses com receita") || strings.Contains(section, "Receita média por mês") {
		t.Error("período de um mês não mostra os indicadores mensais")
	}
	if strings.Contains(section, `data-isort="months"`) {
		t.Error("período de um mês não mostra a coluna Meses")
	}
	if !strings.Contains(section, "Maior fonte") || !strings.Contains(section, "Fontes") {
		t.Error("os demais indicadores continuam")
	}
}

func TestReport_IncomeTablesKeepLiveUpdatesBeforeRendering(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Mercado", "Alimentação", "expense", 12000, "2026-01-10") // seção de receitas vazia
	_, body := getReport(h, "/relatorio?ano=2026")

	live := strings.Index(body, "startLiveUpdates(function")
	render := strings.LastIndex(body, "renderIncome()")
	if live < 0 || render < 0 || live > render {
		t.Errorf("startLiveUpdates (%d) deve vir antes da renderização das receitas (%d)", live, render)
	}
}
