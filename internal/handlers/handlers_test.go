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

	h, err := NewHandler(db, os.DirFS("../.."), "")
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

func TestDashboard_ShowsBuildVersionInFooter(t *testing.T) {
	h, _ := newTestHandler(t)
	h.version = "build-42"

	rec := httptest.NewRecorder()
	h.Dashboard(rec, httptest.NewRequest(http.MethodGet, "/?month=2026-10", nil))

	if !strings.Contains(rec.Body.String(), "build-42") {
		t.Error("o rodapé deve mostrar a versão da build")
	}
}

func TestNewHandler_EmptyVersionFallsBackToDev(t *testing.T) {
	h, _ := newTestHandler(t)
	if h.version != "dev" {
		t.Errorf("version = %q, quer \"dev\" quando não informada", h.version)
	}
}

func TestProjectedTotals_AndWithProjected(t *testing.T) {
	p := []models.ProjectedInstallment{
		{Type: "expense", AmountCents: 25000},
		{Type: "expense", AmountCents: 5000},
		{Type: "income", AmountCents: 10000},
	}
	income, expense := projectedTotals(p)
	if income != 10000 || expense != 30000 {
		t.Errorf("projectedTotals = %d, %d; quer 10000, 30000", income, expense)
	}

	base := models.Summary{TotalIncome: 100000, TotalExpense: 40000, Balance: 60000}
	got := withProjected(base, income, expense)
	if got.TotalIncome != 110000 || got.TotalExpense != 70000 || got.Balance != 40000 {
		t.Errorf("withProjected = %+v", got)
	}
	if base.Balance != 60000 {
		t.Error("withProjected não pode alterar o resumo original")
	}
}

func seedInstallment(t *testing.T, db *database.DB) {
	t.Helper()
	tx := models.Transaction{Description: "Geladeira 3/10", AmountCents: 25000, Type: "expense", Category: "Casa", Date: "2026-10-15"}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatalf("seedInstallment: %v", err)
	}
}

func getDashboard(h *Handler, target string) string {
	rec := httptest.NewRecorder()
	h.Dashboard(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Body.String()
}

func TestDashboard_ShowsProjectedInstallmentsSeparately(t *testing.T) {
	h, db := newTestHandler(t)
	seedInstallment(t, db)

	body := getDashboard(h, "/?month=2026-11")

	if !strings.Contains(body, "Parcelas previstas") || !strings.Contains(body, "4/10") {
		t.Error("novembro deveria listar a 4/10 como prevista")
	}
	if strings.Contains(body, "(com previstas)") {
		t.Error("sem previstas=1 os totais não incluem as previstas")
	}
}

func TestDashboard_IncludesProjectedInTotalsWhenAsked(t *testing.T) {
	h, db := newTestHandler(t)
	seedInstallment(t, db)

	body := getDashboard(h, "/?month=2026-11&previstas=1")

	if !strings.Contains(body, "(com previstas)") {
		t.Error("com previstas=1 os cartões devem indicar '(com previstas)'")
	}
	if !strings.Contains(body, formatCurrencyBRL(25000)) {
		t.Error("a despesa total deveria somar a parcela prevista de R$ 250,00")
	}
}

func TestDashboard_HidesProjectedBlockWhenThereAreNone(t *testing.T) {
	h, _ := newTestHandler(t)

	if body := getDashboard(h, "/?month=2026-11"); strings.Contains(body, "Parcelas previstas") {
		t.Error("sem parcelas o bloco não deve aparecer")
	}
}

func TestDashboard_ShowsInstallmentBadgeAndRemaining(t *testing.T) {
	h, db := newTestHandler(t)
	seedInstallment(t, db)

	body := getDashboard(h, "/?month=2026-10")

	if !strings.Contains(body, "3/10") || !strings.Contains(body, "restam 7 parcelas") {
		t.Error("o lançamento deveria mostrar o selo 3/10 e 'restam 7 parcelas'")
	}
}

func TestMutations_PreserveProjectedToggleInRedirect(t *testing.T) {
	h, db := newTestHandler(t)

	form := validForm()
	form.Set("previstas", "1")
	if loc := post(h.CreateTransaction, "/transactions", form).Header().Get("Location"); loc != "/?month=2026-10&previstas=1" {
		t.Errorf("criar: Location = %q", loc)
	}

	tx := seed(t, db, 100)
	form.Set("id", strconv.FormatInt(tx.ID, 10))
	if loc := post(h.UpdateTransaction, "/transactions/update", form).Header().Get("Location"); loc != "/?month=2026-10&previstas=1" {
		t.Errorf("editar: Location = %q", loc)
	}

	del := url.Values{"id": {strconv.FormatInt(tx.ID, 10)}, "month": {"2026-10"}, "previstas": {"1"}}
	if loc := post(h.DeleteTransaction, "/transactions/delete", del).Header().Get("Location"); loc != "/?month=2026-10&previstas=1" {
		t.Errorf("excluir: Location = %q", loc)
	}
}

func categoryExists(t *testing.T, db *database.DB, name string) bool {
	t.Helper()
	ok, err := db.CategoryExists(name)
	if err != nil {
		t.Fatalf("CategoryExists: %v", err)
	}
	return ok
}

func TestCreateTransaction_UnknownCategoryReturns400(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("category", "Inventada")

	rec := post(h.CreateTransaction, "/transactions", form)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quer 400", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("categoria inexistente não pode gravar: %+v", list)
	}
}

func TestCreateTransaction_NewCategoryIsCreatedAndUsed(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("category", "__new__")
	form.Set("new_category", "  Pets ")

	rec := post(h.CreateTransaction, "/transactions", form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if !categoryExists(t, db, "Pets") {
		t.Error("a nova categoria deveria ter sido criada")
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].Category != "Pets" {
		t.Errorf("lançamento deveria usar a nova categoria: %+v", list)
	}
}

func TestCreateTransaction_NewCategoryReusesExistingIgnoringCaseAndAccents(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("category", "__new__")
	form.Set("new_category", "saude")

	post(h.CreateTransaction, "/transactions", form)

	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].Category != "Saúde" {
		t.Errorf("deveria usar a categoria existente 'Saúde': %+v", list)
	}
	cats, _ := db.ListCategories()
	if len(cats) != 9 {
		t.Errorf("não pode criar duplicata: %d categorias", len(cats))
	}
}

func TestCreateTransaction_BlankNewCategoryReturns400(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("category", "__new__")
	form.Set("new_category", "   ")

	if rec := post(h.CreateTransaction, "/transactions", form); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quer 400", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("nada deveria ser gravado: %+v", list)
	}
}

func TestCreateTransaction_InvalidTransactionDoesNotCreateCategory(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("amount", "abc")
	form.Set("category", "__new__")
	form.Set("new_category", "Pets")

	if rec := post(h.CreateTransaction, "/transactions", form); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, quer 400", rec.Code)
	}
	if categoryExists(t, db, "Pets") {
		t.Error("lançamento inválido não pode criar categoria")
	}
}

func TestUpdateTransaction_KeepsHiddenCategory(t *testing.T) {
	h, db := newTestHandler(t)
	cats, _ := db.ListCategories()
	for _, c := range cats {
		if c.Name == "Lazer" {
			if err := db.SetCategoryHidden(c.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	tx := seed(t, db, 100)

	form := validForm()
	form.Set("id", strconv.FormatInt(tx.ID, 10))
	form.Set("category", "Lazer")
	rec := post(h.UpdateTransaction, "/transactions/update", form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303 (categoria oculta continua válida)", rec.Code)
	}
	list, _ := db.GetTransactions("2026-10")
	if list[0].Category != "Lazer" {
		t.Errorf("categoria oculta deveria ser mantida: %+v", list[0])
	}
}

func TestUpdateTransaction_UnknownCategoryReturns400(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 100)

	form := validForm()
	form.Set("id", strconv.FormatInt(tx.ID, 10))
	form.Set("category", "Inventada")

	if rec := post(h.UpdateTransaction, "/transactions/update", form); rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quer 400", rec.Code)
	}
}

func TestDashboard_NewTransactionIsAPopupWithDraftSupport(t *testing.T) {
	h, _ := newTestHandler(t)

	body := getDashboard(h, "/?month=2026-10")

	for _, want := range []string{
		`onclick="openNewModal()"`,                      // botão que abre o pop-up
		`id="new-modal"`,                                // o pop-up
		`id="new-form"`,                                 // o formulário dentro dele
		`action="/transactions"`,                        // continua enviando para a mesma rota
		`data-draft-key="finpi:draft:novo-lancamento"`,  // chave do rascunho no navegador
		`id="new-form-error"`,                           // erro exibido dentro do pop-up
		`id="draft-notice"`,                             // aviso "Rascunho restaurado"
		`onclick="discardDraft()"`,                      // botão Descartar
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a página deveria conter %s", want)
		}
	}
}

func TestDashboard_NewTransactionPopupStartsClosed(t *testing.T) {
	h, _ := newTestHandler(t)

	body := getDashboard(h, "/?month=2026-10")

	if !strings.Contains(body, `id="new-modal" class="fixed inset-0 z-50 hidden`) {
		t.Error("o pop-up de novo lançamento deve começar fechado")
	}
}

func TestDashboard_NewTransactionFormKeepsMonthAndProjectedState(t *testing.T) {
	h, _ := newTestHandler(t)

	body := getDashboard(h, "/?month=2026-11&previstas=1")

	idx := strings.Index(body, `id="new-form"`)
	if idx < 0 {
		t.Fatal("formulário de novo lançamento ausente")
	}
	form := body[idx : idx+1500]
	if !strings.Contains(form, `name="month" value="2026-11"`) || !strings.Contains(form, `name="previstas" value="1"`) {
		t.Error("o formulário deve manter o mês e o interruptor de previstas")
	}
}

func TestDashboard_EditPopupSupportsDrafts(t *testing.T) {
	h, db := newTestHandler(t)
	seed(t, db, 100)

	body := getDashboard(h, "/?month=2026-10")

	for _, want := range []string{
		`id="edit-form"`,                                           // formulário da edição
		`data-draft-prefix="finpi:draft:editar-lancamento:"`,       // prefixo da chave (um rascunho por lançamento)
		`id="edit-form-error"`,                                     // erro exibido dentro do pop-up
		`id="edit-draft-notice"`,                                   // aviso do rascunho
		`onclick="discardEditDraft()"`,                             // botão Descartar
		`class="edit-draft-dot`,                                    // indicador no lápis da linha
		`action="/transactions/update"`,                            // continua enviando para a mesma rota
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a página deveria conter %s", want)
		}
	}
}
