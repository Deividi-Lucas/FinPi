package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
	year := fmt.Sprint(time.Now().Year())
	for name, q := range map[string]string{
		"de depois de até": "de=2026-05-01&ate=2026-04-01",
		"mais de 5 anos":   "de=2020-01-01&ate=2026-01-01",
		"data ruim":        "de=2026-13-01&ate=2026-12-01",
		"só um campo":      "de=2026-01-01",
		"ano fora":         "ano=1999",
		"ano texto":        "ano=abc",
	} {
		code, body := getReport(h, "/relatorio?"+q)
		if code != http.StatusOK || !strings.Contains(body, "Período inválido") || !strings.Contains(body, year) {
			t.Errorf("%s: status=%d, aviso/ano atual ausentes", name, code)
		}
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
