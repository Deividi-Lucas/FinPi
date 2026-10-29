package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestBulkMove_PreviewShowsDestinationCategoryAndNewMarker(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	if _, err := pj.CreateCategory("Alimentação"); err != nil {
		t.Fatal(err)
	}
	seed(t, db, 1000) // categoria Alimentação (existe no PJ)
	mustTx(t, db, "Escola", "Educação", "expense", 500, "2026-10-06")
	list, _ := db.GetTransactions("2026-10")
	ids := url.Values{"acao": {"mover"}, "livro_destino": {pjID}, "month": {"2026-10"}}
	for _, tx := range list {
		ids.Add("ids", strconv.FormatInt(tx.ID, 10))
	}
	rec := post(h.BulkPreview, "/bulk/preview", ids)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"PJ", "Alimentação", "Educação", "(nova)"} {
		if !strings.Contains(body, want) {
			t.Errorf("prévia sem %q", want)
		}
	}
}

func TestBulkMove_ApplyMovesAndUndoRestores(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	tx := seed(t, db, 1000)
	form := url.Values{"acao": {"mover"}, "livro_destino": {pjID}, "ids": {strconv.FormatInt(tx.ID, 10)}, "month": {"2026-10"}}
	rec := post(h.BulkApply, "/bulk/apply", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("aplicar: %d %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "movidos=1") {
		t.Errorf("aviso de movidos ausente: %s", loc)
	}
	if list, _ := pj.GetTransactions("2026-10"); len(list) != 1 {
		t.Errorf("destino sem o lançamento: %+v", list)
	}
	rec = post(h.BulkUndo, "/bulk/undo", url.Values{"month": {"2026-10"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("desfazer: %d", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 1 {
		t.Errorf("o lançamento não voltou ao Pessoal: %+v", list)
	}
}

func TestBulkMove_Validation(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 1000)
	id := strconv.FormatInt(tx.ID, 10)
	for name, dest := range map[string]string{"sem destino": "", "inexistente": "9999", "mesmo livro": "1"} {
		rec := post(h.BulkPreview, "/bulk/preview", url.Values{"acao": {"mover"}, "livro_destino": {dest}, "ids": {id}})
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, quer 400", name, rec.Code)
		}
	}
}
