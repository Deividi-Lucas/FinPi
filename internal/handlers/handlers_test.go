package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
	"finpi/internal/models"
)

func TestFormatCurrencyBRL(t *testing.T) {
	cases := map[int64]string{
		0:          "R$ 0,00",
		5:          "R$ 0,05",
		150:        "R$ 1,50",
		123456:     "R$ 1.234,56",
		100000000:  "R$ 1.000.000,00",
		-150:       "-R$ 1,50",
		-123456789: "-R$ 1.234.567,89",
	}
	for cents, want := range cases {
		if got := formatCurrencyBRL(cents); got != want {
			t.Errorf("formatCurrencyBRL(%d) = %q, quer %q", cents, got, want)
		}
	}
}

func TestFormatCentsInput(t *testing.T) {
	cases := map[int64]string{5: "0.05", 150: "1.50", 123456: "1234.56"}
	for cents, want := range cases {
		if got := formatCentsInput(cents); got != want {
			t.Errorf("formatCentsInput(%d) = %q, quer %q", cents, got, want)
		}
	}
}

func newTestHandler(t *testing.T) (*Handler, *database.DB) {
	t.Helper()
	db, err := database.InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	h, err := NewHandler(db, os.DirFS("../.."))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return h, db
}

func post(handler http.HandlerFunc, path string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

func seed(t *testing.T, db *database.DB, cents int64) models.Transaction {
	t.Helper()
	tx := models.Transaction{Description: "Mercado", AmountCents: cents, Type: "expense", Category: "Alimentação", Date: "2026-10-05"}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return tx
}

func TestCreateTransaction_RedirectsToTransactionMonth(t *testing.T) {
	h, db := newTestHandler(t)

	rec := post(h.CreateTransaction, "/transactions", validForm())

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?month=2026-10" {
		t.Errorf("Location = %q, quer /?month=2026-10", loc)
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].AmountCents != 4590 {
		t.Errorf("lançamento não gravado corretamente: %+v", list)
	}
}

func TestCreateTransaction_InvalidInputReturns400(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("amount", "abc")

	rec := post(h.CreateTransaction, "/transactions", form)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quer 400", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("entrada inválida não pode gravar: %+v", list)
	}
}

func TestCreateTransaction_RejectsGET(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.CreateTransaction(rec, httptest.NewRequest(http.MethodGet, "/transactions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, quer 405", rec.Code)
	}
}

func TestUpdateTransaction_FixesWrongAmount(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 99900) // digitado errado: R$ 999,00

	form := validForm()
	form.Set("id", strconv.FormatInt(tx.ID, 10))
	form.Set("amount", "99.90")
	rec := post(h.UpdateTransaction, "/transactions/update", form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].AmountCents != 9990 {
		t.Errorf("valor não corrigido: %+v", list)
	}
}

func TestUpdateTransaction_NotFoundReturns404(t *testing.T) {
	h, _ := newTestHandler(t)
	form := validForm()
	form.Set("id", "999")
	if rec := post(h.UpdateTransaction, "/transactions/update", form); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quer 404", rec.Code)
	}
}

func TestDeleteTransaction(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 100)

	rec := post(h.DeleteTransaction, "/transactions/delete", url.Values{"id": {strconv.FormatInt(tx.ID, 10)}, "month": {"2026-10"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("lançamento não foi excluído: %+v", list)
	}
}

func TestDeleteTransaction_NotFoundReturns404(t *testing.T) {
	h, _ := newTestHandler(t)
	if rec := post(h.DeleteTransaction, "/transactions/delete", url.Values{"id": {"999"}}); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quer 404", rec.Code)
	}
}

func TestDeleteTransaction_EscapesMonthInRedirect(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 100)

	rec := post(h.DeleteTransaction, "/transactions/delete", url.Values{"id": {strconv.FormatInt(tx.ID, 10)}, "month": {"2026-10&x=1"}})

	if loc := rec.Header().Get("Location"); loc != "/?month=2026-10%26x%3D1" {
		t.Errorf("Location = %q, mês deve ir escapado", loc)
	}
}

func TestDashboard_RendersAmountsInReais(t *testing.T) {
	h, db := newTestHandler(t)
	seed(t, db, 4590)

	rec := httptest.NewRecorder()
	h.Dashboard(rec, httptest.NewRequest(http.MethodGet, "/?month=2026-10", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quer 200; corpo: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "R$ 45,90") {
		t.Error("página deve exibir R$ 45,90")
	}
	if !strings.Contains(body, `data-amount="45.90"`) {
		t.Error("botão de editar deve carregar o valor em reais com ponto decimal (45.90)")
	}
}

func TestDashboard_DoesNotLeakInternalErrors(t *testing.T) {
	h, db := newTestHandler(t)
	db.Close() // força falha de banco

	rec := httptest.NewRecorder()
	h.Dashboard(rec, httptest.NewRequest(http.MethodGet, "/?month=2026-10", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, quer 500", rec.Code)
	}
	if strings.Contains(strings.ToLower(rec.Body.String()), "closed") {
		t.Errorf("resposta vaza detalhe interno: %q", rec.Body.String())
	}
}
