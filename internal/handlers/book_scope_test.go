package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
)

// newPJ cria o livro PJ e devolve o acesso a ele e o id como texto.
func newPJ(t *testing.T, db *database.DB) (*database.DB, string) {
	t.Helper()
	b, err := db.CreateBook("PJ", "sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	return db.ForBook(b.ID), strconv.FormatInt(b.ID, 10)
}

func TestDashboard_ShowsOnlyTheSelectedBook(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	mustTx(t, db, "Mercado Pessoal", "Alimentação", "expense", 1000, "2026-10-05")
	mustTx(t, pj, "DAS Simples PJ", "Impostos", "expense", 8705, "2026-10-06")

	if body := history(getDashboard(h, "/?month=2026-10")); !strings.Contains(body, "Mercado Pessoal") || strings.Contains(body, "DAS Simples PJ") {
		t.Errorf("sem livro deveria mostrar só o Pessoal: %s", body)
	}
	if body := history(getDashboard(h, "/?month=2026-10&livro="+pjID)); strings.Contains(body, "Mercado Pessoal") || !strings.Contains(body, "DAS Simples PJ") {
		t.Errorf("livro PJ deveria mostrar só o PJ: %s", body)
	}
	if body := history(getDashboard(h, "/?month=2026-10&livro=9999")); !strings.Contains(body, "Mercado Pessoal") {
		t.Error("livro inexistente deveria cair no Pessoal")
	}
}

func TestCreateTransaction_GoesToTheBookInTheForm(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	if _, err := pj.CreateCategory("Impostos"); err != nil {
		t.Fatal(err)
	}
	rec := post(h.CreateTransaction, "/transactions", url.Values{
		"description": {"DAS"}, "amount": {"87.05"}, "type": {"expense"}, "category": {"Impostos"},
		"date": {"2026-10-06"}, "livro": {pjID},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "livro="+pjID) {
		t.Errorf("o redirecionamento perdeu o livro: %s", loc)
	}
	if list, _ := pj.GetTransactions("2026-10"); len(list) != 1 {
		t.Errorf("lançamento não foi para o PJ: %+v", list)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("lançamento vazou para o Pessoal: %+v", list)
	}
	// categoria do PJ não existe no Pessoal
	rec = post(h.CreateTransaction, "/transactions", url.Values{
		"description": {"X"}, "amount": {"1"}, "type": {"expense"}, "category": {"Impostos"}, "date": {"2026-10-06"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("categoria de outro livro deveria dar 400, deu %d", rec.Code)
	}
}

func TestPost_InvalidBookIsRejected(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := post(h.CreateTransaction, "/transactions", url.Values{
		"description": {"X"}, "amount": {"1"}, "type": {"expense"}, "category": {"Outros"}, "date": {"2026-10-06"}, "livro": {"9999"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("livro inválido deveria dar 400, deu %d", rec.Code)
	}
}

func TestBulkUndoAndCategories_AreScopedByBook(t *testing.T) {
	h, db := newTestHandler(t)
	_, pjID := newPJ(t, db)

	rec := post(h.CreateCategory, "/categories", url.Values{"name": {"Fornecedores"}, "livro": {pjID}, "month": {"2026-10"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("criar categoria: %d %s", rec.Code, rec.Body.String())
	}
	if ok, _ := db.CategoryExists("Fornecedores"); ok {
		t.Error("categoria do PJ apareceu no Pessoal")
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "livro="+pjID) {
		t.Errorf("redirecionamento sem o livro: %s", loc)
	}

	tx := seed(t, db, 1000) // lançamento do Pessoal
	rec = post(h.BulkApply, "/bulk/apply", url.Values{
		"acao": {"excluir"}, "ids": {strconv.FormatInt(tx.ID, 10)}, "livro": {pjID}, "month": {"2026-10"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("bulk: %d", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 1 {
		t.Errorf("excluir pelo PJ não pode alcançar o Pessoal: %+v", list)
	}
}

func TestReport_ScopedByBook(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	mustTx(t, db, "Mercado Pessoal", "Alimentação", "expense", 1000, "2026-10-05")
	mustTx(t, pj, "DAS Simples PJ", "Impostos", "expense", 8705, "2026-10-06")
	_, body := getReport(h, "/relatorio?ano=2026&livro="+pjID)
	if !strings.Contains(body, "Impostos") || strings.Contains(body, "Alimentação") {
		t.Error("o relatório do PJ deveria mostrar só o PJ")
	}
	req := httptest.NewRequest(http.MethodGet, "/relatorio?ano=2026&livro="+pjID, nil)
	rec := httptest.NewRecorder()
	h.Report(rec, req)
	if !strings.Contains(rec.Body.String(), "livro="+pjID) {
		t.Error("os links do relatório deveriam manter o livro")
	}
}
