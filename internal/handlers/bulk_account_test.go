package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
	"finpi/internal/models"
)

func contaForm(list []string, conta string) url.Values {
	return url.Values{"acao": {"conta"}, "ids": list, "conta": {conta}, "month": {"2026-10"}}
}

func accountOf(t *testing.T, db *database.DB, id int64) int64 {
	t.Helper()
	got, err := db.GetTransactionsByIDs([]int64{id})
	if err != nil || len(got) != 1 {
		t.Fatalf("lançamento %d: %v %v", id, got, err)
	}
	return got[0].AccountID
}

func TestBulkConta_PreviewShowsChangeAndWritesNothing(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, c := seedBulk(t, db)
	acc, err := db.CreateAccount("Conta Teste", "")
	if err != nil {
		t.Fatal(err)
	}

	rec := post(h.BulkPreview, "/bulk/preview", contaForm(ids(a, b, c), strconv.FormatInt(acc.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Conta Teste", "Aplicar a 3 lançamentos", `name="acao" value="conta"`, `name="conta" value="` + strconv.FormatInt(acc.ID, 10) + `"`, "0 sem mudança"} {
		if !strings.Contains(body, want) {
			t.Errorf("prévia sem %q", want)
		}
	}
	if accountOf(t, db, a.ID) != 0 {
		t.Error("a prévia não pode gravar")
	}
	if batch, _ := db.LatestUndoableBatch(); batch != nil {
		t.Error("a prévia não cria lote")
	}
}

func TestBulkConta_PreviewCountsAlreadyThereAsUnchanged(t *testing.T) {
	h, db := newTestHandler(t)
	acc, _ := db.CreateAccount("Conta Teste", "")
	a := models.Transaction{Description: "Aluguel", Category: "Outros", Type: "expense", AmountCents: 1000, Date: "2026-10-05", AccountID: acc.ID}
	if err := db.CreateTransaction(&a); err != nil {
		t.Fatal(err)
	}
	b := models.Transaction{Description: "Luz", Category: "Outros", Type: "expense", AmountCents: 1000, Date: "2026-10-06"}
	if err := db.CreateTransaction(&b); err != nil {
		t.Fatal(err)
	}
	rec := post(h.BulkPreview, "/bulk/preview", contaForm(ids(a, b), strconv.FormatInt(acc.ID, 10)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"1 será alterado", "1 sem mudança", "Aplicar a 1 lançamento"} {
		if !strings.Contains(body, want) {
			t.Errorf("prévia sem %q: %s", want, body)
		}
	}
}

func TestBulkConta_ApplySetsAccountAndUndoRestores(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, c := seedBulk(t, db)
	acc, _ := db.CreateAccount("Conta Teste", "")
	prev := models.Transaction{Description: "Antigo", Category: "Outros", Type: "expense", AmountCents: 100, Date: "2026-10-08", AccountID: acc.ID}
	if err := db.CreateTransaction(&prev); err != nil {
		t.Fatal(err)
	}
	other, _ := db.CreateAccount("Outra Conta", "")
	if rec := post(h.BulkApply, "/bulk/apply", contaForm(ids(prev), strconv.FormatInt(other.ID, 10))); rec.Code != http.StatusSeeOther {
		t.Fatalf("seed: %d %s", rec.Code, rec.Body.String())
	}

	ch, unsub, _ := h.events.Subscribe()
	defer unsub()
	rec := post(h.BulkApply, "/bulk/apply", contaForm(ids(a, c, prev), strconv.FormatInt(acc.ID, 10)))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "editados=3") || !strings.Contains(loc, "ignorados=0") {
		t.Errorf("Location = %q", loc)
	}
	if !pending(ch) {
		t.Error("aplicar deveria publicar evento")
	}
	for _, id := range []int64{a.ID, c.ID, prev.ID} {
		if accountOf(t, db, id) != acc.ID {
			t.Errorf("lançamento %d sem a conta", id)
		}
	}
	if accountOf(t, db, b.ID) != 0 {
		t.Error("o não selecionado não pode mudar")
	}
	if batch, _ := db.LatestUndoableBatch(); batch == nil || batch.Count != 3 || batch.Action != "edit" {
		t.Errorf("lote = %+v", batch)
	}

	if rec := post(h.BulkUndo, "/bulk/undo", url.Values{"month": {"2026-10"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("undo: %d %s", rec.Code, rec.Body.String())
	}
	if accountOf(t, db, a.ID) != 0 || accountOf(t, db, c.ID) != 0 {
		t.Error("o desfazer deveria devolver 'sem conta'")
	}
	if accountOf(t, db, prev.ID) != other.ID {
		t.Error("o desfazer deveria devolver a conta anterior")
	}
}

func TestBulkConta_NoAccount(t *testing.T) {
	h, db := newTestHandler(t)
	a, b, _ := seedBulk(t, db)
	acc, _ := db.CreateAccount("Conta Teste", "")
	post(h.BulkApply, "/bulk/apply", contaForm(ids(a), strconv.FormatInt(acc.ID, 10)))

	rec := post(h.BulkPreview, "/bulk/preview", contaForm(ids(a, b), "0"))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Sem conta") || !strings.Contains(rec.Body.String(), "1 sem mudança") {
		t.Fatalf("prévia: %d %s", rec.Code, rec.Body.String())
	}
	rec = post(h.BulkApply, "/bulk/apply", contaForm(ids(a, b), "0"))
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "editados=1&ignorados=1") {
		t.Fatalf("status %d Location %q", rec.Code, rec.Header().Get("Location"))
	}
	if accountOf(t, db, a.ID) != 0 {
		t.Error("deveria ficar sem conta")
	}
}

func TestBulkConta_InvalidAccountIs400(t *testing.T) {
	h, db := newTestHandler(t)
	a, _, _ := seedBulk(t, db)
	archived, _ := db.CreateAccount("Velha", "")
	if err := db.SetAccountArchived(archived.ID, true); err != nil {
		t.Fatal(err)
	}
	pj, _ := db.CreateBook("PJ", "sky", 0)
	foreign, err := db.ForBook(pj.ID).CreateAccount("De Outro Livro", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, conta := range map[string]string{
		"arquivada": strconv.FormatInt(archived.ID, 10), "outro livro": strconv.FormatInt(foreign.ID, 10),
		"inexistente": "9999", "negativa": "-1", "lixo": "abc", "vazia": "",
	} {
		for _, fn := range []http.HandlerFunc{h.BulkPreview, h.BulkApply} {
			if rec := post(fn, "/bulk", contaForm(ids(a), conta)); rec.Code != http.StatusBadRequest {
				t.Errorf("%s: status = %d, quer 400", name, rec.Code)
			}
		}
	}
	if accountOf(t, db, a.ID) != 0 {
		t.Error("nada pode ser gravado")
	}
	if batch, _ := db.LatestUndoableBatch(); batch != nil {
		t.Error("nenhum lote")
	}
}

func TestDashboard_BulkContaControls(t *testing.T) {
	h, db := newTestHandler(t)
	seedBulk(t, db)
	if _, err := db.CreateAccount("Conta Teste", ""); err != nil {
		t.Fatal(err)
	}
	body := getDashboard(h, "/?month=2026-10")
	for _, want := range []string{`openBulkAccount()`, `id="bulk-account-form"`, `name="conta"`, `Conta Teste`, `Sem conta`} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
}
