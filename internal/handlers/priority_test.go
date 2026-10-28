package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/models"
)

func TestParseTransactionForm_Priority(t *testing.T) {
	form := validForm()
	form.Set("priority", " essential ")
	tx, err := parseTransactionForm(form)
	if err != nil || tx.Priority != models.PriorityEssential {
		t.Errorf("tx = %+v, err = %v", tx, err)
	}

	form.Set("priority", "urgente")
	if _, err := parseTransactionForm(form); err == nil {
		t.Error("prioridade inválida deveria dar erro")
	}

	form.Set("priority", "superfluous")
	form.Set("type", "income")
	tx, err = parseTransactionForm(form)
	if err != nil || tx.Priority != "" {
		t.Errorf("receita deveria normalizar para vazio: %+v, %v", tx, err)
	}

	form.Del("priority")
	form.Set("type", "expense")
	if tx, _ := parseTransactionForm(form); tx.Priority != "" {
		t.Errorf("sem campo: %q", tx.Priority)
	}
}

func TestCreateAndUpdateTransaction_StorePriority(t *testing.T) {
	h, db := newTestHandler(t)

	form := validForm()
	form.Set("priority", "superfluous")
	if rec := post(h.CreateTransaction, "/transactions", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("criar: status %d", rec.Code)
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].Priority != models.PrioritySuperfluous {
		t.Fatalf("lista = %+v", list)
	}

	form.Set("id", strconv.FormatInt(list[0].ID, 10))
	form.Set("priority", "essential")
	if rec := post(h.UpdateTransaction, "/transactions/update", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("editar: status %d", rec.Code)
	}
	list, _ = db.GetTransactions("2026-10")
	if list[0].Priority != models.PriorityEssential {
		t.Errorf("após editar = %q", list[0].Priority)
	}
}

func TestCreateTransaction_InvalidPriorityReturns400(t *testing.T) {
	h, db := newTestHandler(t)
	form := validForm()
	form.Set("priority", "urgente")
	rec := post(h.CreateTransaction, "/transactions", form)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quer 400", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("nada deveria ter sido gravado: %+v", list)
	}
}

func TestSetCategoryPriority_Success(t *testing.T) {
	h, db := newTestHandler(t)
	id := strconv.FormatInt(findCat(t, db, "Lazer").ID, 10)

	rec := post(h.SetCategoryPriority, "/categories/priority", url.Values{
		"id": {id}, "priority": {"superfluous"}, "month": {"2026-10"}, "previstas": {"1"},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?month=2026-10&categorias=1&previstas=1" {
		t.Errorf("Location = %q", loc)
	}
	if got := findCat(t, db, "Lazer").Priority; got != models.PrioritySuperfluous {
		t.Errorf("priority = %q", got)
	}
}

func TestSetCategoryPriority_Errors(t *testing.T) {
	h, db := newTestHandler(t)
	id := strconv.FormatInt(findCat(t, db, "Lazer").ID, 10)

	cases := map[string]struct {
		form url.Values
		want int
	}{
		"inválida":    {url.Values{"id": {id}, "priority": {"urgente"}}, http.StatusBadRequest},
		"inexistente": {url.Values{"id": {"99999"}, "priority": {"essential"}}, http.StatusNotFound},
		"id ruim":     {url.Values{"id": {"x"}, "priority": {"essential"}}, http.StatusBadRequest},
	}
	for name, c := range cases {
		rec := post(h.SetCategoryPriority, "/categories/priority", c.form)
		if rec.Code != c.want {
			t.Errorf("%s: status = %d, quer %d", name, rec.Code, c.want)
		}
	}
}

func TestSetCategoryPriority_RejectsGETAndPublishesEvent(t *testing.T) {
	h, db := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.SetCategoryPriority(rec, httptest.NewRequest(http.MethodGet, "/categories/priority", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: status = %d, quer 405", rec.Code)
	}

	ch, unsub, err := h.events.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	id := strconv.FormatInt(findCat(t, db, "Lazer").ID, 10)
	post(h.SetCategoryPriority, "/categories/priority", url.Values{"id": {id}, "priority": {"essential"}})
	if !pending(ch) {
		t.Error("classificar categoria deveria publicar evento")
	}
}

func seedMonth(t *testing.T, h *Handler) {
	t.Helper()
	db := h.db
	lazer := findCat(t, db, "Lazer")
	saude := findCat(t, db, "Saúde")
	if err := db.SetCategoryPriority(lazer.ID, models.PrioritySuperfluous); err != nil {
		t.Fatal(err)
	}
	if err := db.SetCategoryPriority(saude.ID, models.PriorityEssential); err != nil {
		t.Fatal(err)
	}
	for _, tx := range []models.Transaction{
		{Description: "Salário de outubro", AmountCents: 500000, Type: "income", Category: "Salário", Date: "2026-10-01"},
		{Description: "Plano de saúde", AmountCents: 30000, Type: "expense", Category: "Saúde", Date: "2026-10-02"},
		{Description: "Cinema", AmountCents: 4500, Type: "expense", Category: "Lazer", Date: "2026-10-03"},
		{Description: "Padaria", AmountCents: 1200, Type: "expense", Category: "Outros", Date: "2026-10-04"},
	} {
		tx := tx
		if err := db.CreateTransaction(&tx); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDashboard_ShowsMonthStats(t *testing.T) {
	h, _ := newTestHandler(t)
	seedMonth(t, h)

	body := getDashboard(h, "/?month=2026-10")

	for _, want := range []string{
		"Dashboard do mês", "Maior gasto", "Menor gasto", "Plano de saúde", "Padaria",
		"1 receita", "3 despesas", "Essencial", "Supérfluo", "Sem classificação", "R$ 300,00", "R$ 12,00",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
	// 30000 de 35700 = 84,0%; 4500 = 12,6%; 1200 = 3,4%
	for _, want := range []string{"84,0%", "12,6%", "3,4%"} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem percentual %q", want)
		}
	}
	if !strings.Contains(body, "ainda sem classificação") {
		t.Error("deveria avisar sobre gastos sem classificação")
	}
}

func TestDashboard_NoWarningWhenAllClassified(t *testing.T) {
	h, db := newTestHandler(t)
	outros := findCat(t, db, "Outros")
	if err := db.SetCategoryPriority(outros.ID, models.PrioritySuperfluous); err != nil {
		t.Fatal(err)
	}
	tx := models.Transaction{Description: "Padaria", AmountCents: 1200, Type: "expense", Category: "Outros", Date: "2026-10-04"}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	body := getDashboard(h, "/?month=2026-10")
	if !strings.Contains(body, "Dashboard do mês") {
		t.Fatal("seção do dashboard ausente")
	}
	if strings.Contains(body, "ainda sem classificação") {
		t.Error("não deveria avisar quando tudo está classificado")
	}
}

func TestDashboard_EmptyMonthMessages(t *testing.T) {
	h, db := newTestHandler(t)
	if body := getDashboard(h, "/?month=2026-09"); !strings.Contains(body, "Sem lançamentos neste mês") {
		t.Error("mês vazio sem mensagem")
	}

	tx := models.Transaction{Description: "Salário", AmountCents: 1000, Type: "income", Category: "Salário", Date: "2026-08-01"}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	if body := getDashboard(h, "/?month=2026-08"); !strings.Contains(body, "Sem despesas neste mês") {
		t.Error("mês só com receitas sem mensagem")
	}
}

func TestDashboard_ProjectedInstallmentsDoNotCount(t *testing.T) {
	h, db := newTestHandler(t)
	seedInstallment(t, db) // 3/10 em 2026-10 → previsão em 2026-11
	body := getDashboard(h, "/?month=2026-11")
	if !strings.Contains(body, "Sem lançamentos neste mês") {
		t.Error("parcelas previstas não deveriam contar como lançamentos")
	}
}

func TestDashboard_ShowsEffectivePriorityBadgeInHistory(t *testing.T) {
	h, _ := newTestHandler(t)
	seedMonth(t, h)
	body := getDashboard(h, "/?month=2026-10")
	if !strings.Contains(body, `data-priority-badge="essential"`) || !strings.Contains(body, `data-priority-badge="superfluous"`) {
		t.Error("histórico sem selos de classificação")
	}
}

func TestTemplate_PriorityControls(t *testing.T) {
	h, db := newTestHandler(t)
	lazer := findCat(t, db, "Lazer")
	if err := db.SetCategoryPriority(lazer.ID, models.PrioritySuperfluous); err != nil {
		t.Fatal(err)
	}
	body := getDashboard(h, "/?month=2026-10")

	if !strings.Contains(body, `action="/categories/priority"`) {
		t.Error("menu de categorias sem o seletor de prioridade")
	}
	if !strings.Contains(body, `name="priority"`) || strings.Count(body, `data-priority-field`) < 2 {
		t.Error("formulários de lançamento sem o campo Prioridade")
	}
	if !strings.Contains(body, `data-priority="superfluous"`) {
		t.Error("opções de categoria sem data-priority")
	}
	if !strings.Contains(body, "Seguir a categoria") {
		t.Error("sem a opção 'Seguir a categoria'")
	}
}

func TestTemplate_EditButtonCarriesPriority(t *testing.T) {
	h, db := newTestHandler(t)
	tx := models.Transaction{Description: "Cinema", AmountCents: 4500, Type: "expense", Category: "Lazer", Date: "2026-10-03", Priority: models.PrioritySuperfluous}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	if body := getDashboard(h, "/?month=2026-10"); !strings.Contains(body, `data-priority="superfluous"`) {
		t.Error("botão de editar sem data-priority do lançamento")
	}
}
