package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
	"finpi/internal/models"
)

func ids(list ...models.Transaction) []string {
	var out []string
	for _, t := range list {
		out = append(out, strconv.FormatInt(t.ID, 10))
	}
	return out
}

func seedBulk(t *testing.T, db *database.DB) (a, b, c models.Transaction) {
	t.Helper()
	mk := func(desc, cat, typ, date string) models.Transaction {
		tx := models.Transaction{Description: desc, Category: cat, Type: typ, AmountCents: 5000, Date: date}
		if err := db.CreateTransaction(&tx); err != nil {
			t.Fatal(err)
		}
		return tx
	}
	return mk("Pix recebido Ana", "Outros", "income", "2026-10-05"),
		mk("Pix recebido Beto", "Outros", "income", "2026-10-06"),
		mk("Mercado", "Outros", "expense", "2026-10-07")
}

func editForm(list []string, extra url.Values) url.Values {
	f := url.Values{"acao": {"editar"}, "ids": list, "month": {"2026-10"}}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestBulkPreview_ShowsBeforeAfterAndWritesNothing(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, c := seedBulk(t, db)

	rec := post(h.BulkPreview, "/bulk/preview", editForm(ids(a, b, c), url.Values{
		"categoria": {"Lazer"}, "localizar": {"pix recebido"}, "substituir": {"Pix"},
	}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Pix Ana", "Pix Beto", "Pix recebido Ana", "Lazer", "Aplicar a 3 lançamentos", `name="ids"`, `name="localizar" value="pix recebido"`} {
		if !strings.Contains(body, want) {
			t.Errorf("prévia sem %q", want)
		}
	}
	list, _ := db.GetTransactions("2026-10")
	for _, tx := range list {
		if tx.Category != "Outros" || strings.HasPrefix(tx.Description, "Pix ") && !strings.HasPrefix(tx.Description, "Pix recebido") {
			t.Errorf("a prévia não pode gravar nada: %+v", tx)
		}
	}
	if batch, _ := db.LatestUndoableBatch(); batch != nil {
		t.Error("a prévia não cria lote")
	}
}

func TestBulkPreview_UnchangedRowsAreLockedAndMissingIDsIgnored(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, c := seedBulk(t, db)
	f := editForm(append(ids(a, c), "99999"), url.Values{"localizar": {"pix recebido"}, "substituir": {"Pix"}})
	body := post(h.BulkPreview, "/bulk/preview", f).Body.String()
	if !strings.Contains(body, "Aplicar a 1 lançamento") || !strings.Contains(body, "1 sem mudança") || !strings.Contains(body, "disabled") {
		t.Error("a linha sem mudança deveria ficar travada e contada")
	}
	if !strings.Contains(body, "1 lançamento selecionado não existe mais") {
		t.Error("deveria avisar do id inexistente")
	}
}

func TestBulkPreview_DeleteListsWhatWillBeDeleted(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, _ := seedBulk(t, db)
	body := post(h.BulkPreview, "/bulk/preview", url.Values{"acao": {"excluir"}, "ids": ids(a, b), "month": {"2026-10"}}).Body.String()
	for _, want := range []string{"Pix recebido Ana", "Pix recebido Beto", "Excluir 2 lançamentos"} {
		if !strings.Contains(body, want) {
			t.Errorf("prévia da exclusão sem %q", want)
		}
	}
	if got, _ := db.GetTransactionsByIDs([]int64{a.ID, b.ID}); len(got) != 2 {
		t.Error("a prévia não pode excluir")
	}
}

func TestBulkPreview_Validation(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, _ := seedBulk(t, db)
	many := make([]string, 501)
	for i := range many {
		many[i] = strconv.Itoa(i + 1)
	}
	cases := map[string]url.Values{
		"sem ids":               {"acao": {"editar"}, "categoria": {"Lazer"}},
		"mais de 500":           editForm(many, url.Values{"categoria": {"Lazer"}}),
		"id inválido":           editForm([]string{"x"}, url.Values{"categoria": {"Lazer"}}),
		"ação inválida":         {"acao": {"apagar"}, "ids": ids(a)},
		"sem alteração":         editForm(ids(a), nil),
		"categoria inexistente": editForm(ids(a), url.Values{"categoria": {"Inventada"}}),
		"prioridade inválida":   editForm(ids(a), url.Values{"prioridade": {"urgente"}}),
		"data inválida":         editForm(ids(a), url.Values{"data_modo": {"definir"}, "data": {"2026-13-40"}}),
		"deslocar sem número":   editForm(ids(a), url.Values{"data_modo": {"deslocar"}, "desloca": {"abc"}, "desloca_unidade": {"dias"}}),
		"deslocar zero":         editForm(ids(a), url.Values{"data_modo": {"deslocar"}, "desloca": {"0"}, "desloca_unidade": {"dias"}}),
		"unidade inválida":      editForm(ids(a), url.Values{"data_modo": {"deslocar"}, "desloca": {"1"}, "desloca_unidade": {"anos"}}),
		"modo de data":          editForm(ids(a), url.Values{"data_modo": {"x"}}),
		"descrição vazia":       editForm(ids(a), url.Values{"localizar": {"Pix recebido Ana"}, "substituir": {""}}),
		"todos inexistentes":    editForm([]string{"99998", "99999"}, url.Values{"categoria": {"Lazer"}}),
	}
	for name, f := range cases {
		if rec := post(h.BulkPreview, "/bulk/preview", f); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quer 400 (%s)", name, rec.Code, rec.Body.String())
		}
	}
	rec := post(h.BulkPreview, "/bulk/preview", editForm(ids(a), url.Values{"localizar": {"Pix recebido Ana"}}))
	if !strings.Contains(rec.Body.String(), "Pix recebido Ana") || !strings.Contains(rec.Body.String(), "vazia") {
		t.Errorf("a mensagem de descrição vazia deveria citar o lançamento: %s", rec.Body.String())
	}
	get := httptest.NewRecorder()
	h.BulkPreview(get, httptest.NewRequest(http.MethodGet, "/bulk/preview", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", get.Code)
	}
}

func TestBulkApply_EditsOnlySelectedKeepsFiltersAndPublishesEvent(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, c := seedBulk(t, db)
	ch, unsub, err := h.events.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	f := editForm(ids(a, c), url.Values{"categoria": {"saude"}, "prioridade": {"essential"}, "filtros": {"q=pix&tipo=receita&lixo=1"}, "previstas": {"1"}})
	rec := post(h.BulkApply, "/bulk/apply", f)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	for _, want := range []string{"/?month=2026-10", "previstas=1", "q=pix", "tipo=receita", "editados=2", "ignorados=0"} {
		if !strings.Contains(loc, want) {
			t.Errorf("Location %q sem %q", loc, want)
		}
	}
	if strings.Contains(loc, "lixo") {
		t.Errorf("parâmetros desconhecidos não passam: %q", loc)
	}
	got, _ := db.GetTransactionsByIDs([]int64{a.ID, b.ID, c.ID})
	for _, tx := range got {
		switch tx.ID {
		case a.ID: // receita: categoria muda, prioridade não existe
			if tx.Category != "Saúde" || tx.Priority != "" {
				t.Errorf("receita = %+v", tx)
			}
		case c.ID:
			if tx.Category != "Saúde" || tx.Priority != models.PriorityEssential {
				t.Errorf("despesa = %+v", tx)
			}
		case b.ID:
			if tx.Category != "Outros" {
				t.Errorf("o não selecionado não pode mudar: %+v", tx)
			}
		}
	}
	if !pending(ch) {
		t.Error("aplicar deveria publicar evento")
	}
	if batch, _ := db.LatestUndoableBatch(); batch == nil || batch.Count != 2 || batch.Action != "edit" {
		t.Errorf("lote = %+v", batch)
	}
}

func TestBulkApply_InvalidDescriptionRefusesEverything(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, c := seedBulk(t, db)
	ch, unsub, _ := h.events.Subscribe()
	defer unsub()
	// "Mercado" ficaria vazio; "Pix recebido Ana" também seria alterado, mas nada pode ser gravado.
	f := editForm(ids(a, c), url.Values{"categoria": {"Lazer"}, "localizar": {"Mercado"}, "substituir": {""}})
	rec := post(h.BulkApply, "/bulk/apply", f)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	got, _ := db.GetTransactionsByIDs([]int64{a.ID, c.ID})
	for _, tx := range got {
		if tx.Category != "Outros" {
			t.Errorf("nada deveria ter sido gravado: %+v", tx)
		}
	}
	if pending(ch) {
		t.Error("erro não publica evento")
	}
}

func TestBulkApply_Delete(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, _ := seedBulk(t, db)
	rec := post(h.BulkApply, "/bulk/apply", url.Values{"acao": {"excluir"}, "ids": ids(a, b), "month": {"2026-10"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "excluidos=2") {
		t.Fatalf("status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if got, _ := db.GetTransactionsByIDs([]int64{a.ID, b.ID}); len(got) != 0 {
		t.Error("deveriam ter sido excluídos")
	}
}

func TestBulkApply_NothingChangedIsReportedNotAnError(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, _ := seedBulk(t, db)
	rec := post(h.BulkApply, "/bulk/apply", editForm(ids(a), url.Values{"categoria": {"Outros"}}))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "editados=0&ignorados=1") {
		t.Errorf("status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestBulkUndo_RestoresAndRedirects(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, c := seedBulk(t, db)
	post(h.BulkApply, "/bulk/apply", editForm(ids(a, c), url.Values{"categoria": {"Lazer"}}))

	ch, unsub, _ := h.events.Subscribe()
	defer unsub()
	rec := post(h.BulkUndo, "/bulk/undo", url.Values{"month": {"2026-10"}, "filtros": {"q=pix"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "desfeitos=2") || !strings.Contains(loc, "pulados=0") || !strings.Contains(loc, "q=pix") {
		t.Errorf("Location = %q", loc)
	}
	got, _ := db.GetTransactionsByIDs([]int64{a.ID, c.ID})
	for _, tx := range got {
		if tx.Category != "Outros" {
			t.Errorf("deveria ter voltado: %+v", tx)
		}
	}
	if !pending(ch) {
		t.Error("desfazer deveria publicar evento")
	}
	if rec := post(h.BulkUndo, "/bulk/undo", url.Values{}); rec.Code != http.StatusNotFound {
		t.Errorf("sem lote: status = %d, quer 404", rec.Code)
	}
	get := httptest.NewRecorder()
	h.BulkUndo(get, httptest.NewRequest(http.MethodGet, "/bulk/undo", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", get.Code)
	}
}

func TestDashboard_BulkBannerAndUndoControls(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, _ := seedBulk(t, db)
	post(h.BulkApply, "/bulk/apply", editForm(ids(a, b), url.Values{"categoria": {"Lazer"}}))

	body := getDashboard(h, "/?month=2026-10&editados=2&ignorados=1")
	for _, want := range []string{"2 lançamentos alterados", "1 sem mudança ou inexistente", `id="bulk-result"`, `action="/bulk/undo"`, `id="undo-bar"`, "Desfazer última edição em lote", "categoria → Lazer"} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
	for _, c := range []struct{ q, want string }{
		{"excluidos=3", "3 lançamentos excluídos"},
		{"desfeitos=2&pulados=1", "2 lançamentos restaurados"},
	} {
		if b := getDashboard(h, "/?month=2026-10&"+c.q); !strings.Contains(b, c.want) {
			t.Errorf("%s: sem %q", c.q, c.want)
		}
	}
	for _, bad := range []string{"editados=abc", "editados=-1", "editados=501", "excluidos=999999"} {
		if strings.Contains(getDashboard(h, "/?month=2026-10&"+bad), `id="bulk-result"`) {
			t.Errorf("%s não deveria mostrar o aviso", bad)
		}
	}
}

func TestDashboard_NoUndoBarWithoutBatch(t *testing.T) {
	h, _ := newTestHandler(t)
	if strings.Contains(getDashboard(h, "/"), `id="undo-bar"`) {
		t.Error("sem lote não há barra de desfazer")
	}
}

func TestDashboard_BulkSelectionControls(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, _ := seedBulk(t, db)
	body := getDashboard(h, "/?month=2026-10")
	for _, want := range []string{
		`id="bulk-all"`, `data-bulk-id="` + strconv.FormatInt(a.ID, 10) + `"`, `id="bulk-bar"`, `id="bulk-modal"`, `action="/bulk/preview"`,
		`name="categoria"`, `name="prioridade"`, `name="data_modo"`, `name="desloca"`, `name="desloca_unidade"`, `name="localizar"`, `name="substituir"`,
		"hasBulkPending",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
	if empty := getDashboard(h, "/?month=2030-01"); !strings.Contains(empty, `colspan="7"`) {
		t.Error("a linha de lista vazia deveria ocupar as 7 colunas")
	}
}
