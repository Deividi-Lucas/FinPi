package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"finpi/internal/models"
)

func getOverview(h *Handler, target string) (int, string) {
	rec := httptest.NewRecorder()
	h.Overview(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Code, rec.Body.String()
}

func TestOverview_SumsBooksWithoutLinkedTransfersAndReconciles(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := newPJ(t, db)
	mustTx(t, db, "Salário", "Salário", "income", 1000000, "2026-10-05")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 200000, "2026-10-06")
	mustTx(t, db, "Pix para PJ", "Outros", "expense", 300000, "2026-10-07")
	mustTx(t, pj, "Pix do Pessoal", "Outros", "income", 300000, "2026-10-07")
	mustTx(t, pj, "DAS", "Impostos", "expense", 100000, "2026-10-08")

	// sem ligar, o total conta a transferência duas vezes: receitas 13.000, despesas 6.000
	code, body := getOverview(h, "/visao-geral?month=2026-10")
	if code != http.StatusOK || !strings.Contains(body, "R$ 13.000,00") || !strings.Contains(body, "R$ 6.000,00") {
		t.Fatalf("antes de ligar: %d %.300s", code, body)
	}
	if !strings.Contains(body, "1 par sugerido") && !strings.Contains(body, "1 pares sugeridos") {
		t.Error("deveria avisar do par sugerido")
	}

	all, _, _ := db.AllTransactions(100)
	var outID, inID int64
	for _, tx := range all {
		switch tx.Description {
		case "Pix para PJ":
			outID = tx.ID
		case "Pix do Pessoal":
			inID = tx.ID
		}
	}
	if err := db.LinkTransfer(outID, inID); err != nil {
		t.Fatal(err)
	}
	_, body = getOverview(h, "/visao-geral?month=2026-10")
	if !strings.Contains(body, "R$ 10.000,00") || !strings.Contains(body, "R$ 3.000,00") || !strings.Contains(body, "R$ 7.000,00") {
		t.Errorf("depois de ligar: receitas 10.000, despesas 3.000 e saldo 7.000 — %.400s", body)
	}
	for _, want := range []string{"Pessoal", "PJ", "Transferências entre livros"} {
		if !strings.Contains(body, want) {
			t.Errorf("a página deveria conter %q", want)
		}
	}
	if strings.Contains(body, "par sugerido") || strings.Contains(body, "pares sugeridos") {
		t.Error("com o par ligado não há mais sugestões")
	}
}

func TestOverview_YearAndDefaultsAndMethod(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Mercado", "Alimentação", "expense", 1000, "2026-03-05")
	if _, body := getOverview(h, "/visao-geral?ano=2026"); !strings.Contains(body, "2026") || !strings.Contains(body, "R$ 10,00") {
		t.Error("a visão do ano deveria somar março")
	}
	if code, _ := getOverview(h, "/visao-geral"); code != http.StatusOK {
		t.Errorf("sem parâmetros deveria abrir o mês atual: %d", code)
	}
	if code, _ := getOverview(h, "/visao-geral?month=lixo&ano=9999"); code != http.StatusOK {
		t.Errorf("parâmetros inválidos caem no mês atual: %d", code)
	}
	rec := httptest.NewRecorder()
	h.Overview(rec, httptest.NewRequest(http.MethodPost, "/visao-geral", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST deveria dar 405: %d", rec.Code)
	}
}

func TestOverview_InternalMovementIsOutAndSoloMarkIsOut(t *testing.T) {
	h, db := newTestHandler(t)
	if _, err := db.CreateCategory(models.InternalCategory); err != nil {
		t.Fatal(err)
	}
	mustTx(t, db, "Resgate", models.InternalCategory, "income", 55500, "2026-10-05")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 10000, "2026-10-06")
	mustTx(t, db, "Pix para conta de fora", "Outros", "expense", 7700, "2026-10-07")
	all, _, _ := db.AllTransactions(10)
	for _, tx := range all {
		if tx.Description == "Pix para conta de fora" {
			if _, _, err := db.MarkSolo([]int64{tx.ID}); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, body := getOverview(h, "/visao-geral?month=2026-10")
	if !strings.Contains(body, "Movimentações internas") {
		t.Error("deveria mostrar a linha de movimentações internas")
	}
	// despesas = só o Mercado (R$ 100,00): a marcada "fora do FinPi" (R$ 77,00) é subtraída; receitas = R$ 0,00
	if !strings.Contains(body, "Fora do FinPi") || !strings.Contains(body, "R$ 77,00") || !strings.Contains(body, "R$ 100,00") {
		t.Error("deveria mostrar a linha Fora do FinPi (R$ 77,00) e as despesas de R$ 100,00")
	}
}

func TestOverview_HasNavLinksInOtherScreens(t *testing.T) {
	h, _ := newTestHandler(t)
	if body := getDashboard(h, "/?month=2026-10"); !strings.Contains(body, `href="/visao-geral"`) {
		t.Error("a tela principal deveria ter o botão Visão geral")
	}
	if _, body := getReport(h, "/relatorio?ano=2026"); !strings.Contains(body, `href="/visao-geral"`) {
		t.Error("o relatório deveria ter o botão Visão geral")
	}
}
