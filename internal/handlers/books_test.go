package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func TestCreateBook_RedirectsToTheNewBook(t *testing.T) {
	h, db := newTestHandler(t)
	rec := post(h.CreateBook, "/livros", url.Values{"name": {"PJ"}, "color": {"sky"}, "copiar_de": {""}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	books, _ := db.ListBooks()
	if len(books) != 2 {
		t.Fatalf("livros: %+v", books)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "livro="+strconv.FormatInt(books[1].ID, 10)) {
		t.Errorf("deveria abrir o livro novo: %s", loc)
	}
}

func TestCreateBook_CopiesCategories(t *testing.T) {
	h, db := newTestHandler(t)
	_, _ = db.CreateCategory("Mercado")
	rec := post(h.CreateBook, "/livros", url.Values{"name": {"PJ"}, "color": {"sky"}, "copiar_de": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d", rec.Code)
	}
	books, _ := db.ListBooks()
	if ok, _ := db.ForBook(books[1].ID).CategoryExists("Mercado"); !ok {
		t.Error("categorias deveriam ter sido copiadas")
	}
}

func TestCreateBook_ErrorsShowInTheDialog(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := post(h.CreateBook, "/livros", url.Values{"name": {"pessoal"}, "color": {"sky"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "já existe um livro") {
		t.Errorf("status %d, corpo sem o aviso: %.200s", rec.Code, rec.Body.String())
	}
	rec = post(h.CreateBook, "/livros", url.Values{"name": {"X"}, "color": {"magenta"}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("cor inválida: %d", rec.Code)
	}
}

func TestUpdateAndArchiveBook(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := db.CreateBook("PJ", "sky", 0)
	id := strconv.FormatInt(pj.ID, 10)
	if rec := post(h.UpdateBook, "/livros/editar", url.Values{"id": {id}, "name": {"Empresa"}, "color": {"amber"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("editar: %d %s", rec.Code, rec.Body.String())
	}
	if b, _ := db.GetBook(pj.ID); b.Name != "Empresa" || b.Color != "amber" {
		t.Errorf("não gravou: %+v", b)
	}
	rec := post(h.ArchiveBook, "/livros/arquivar", url.Values{"id": {id}, "arquivar": {"1"}, "livro": {id}})
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "livro="+id) {
		t.Errorf("arquivar o livro atual deveria voltar ao Pessoal: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	if rec := post(h.ArchiveBook, "/livros/arquivar", url.Values{"id": {"1"}, "arquivar": {"1"}}); rec.Code != http.StatusConflict {
		t.Errorf("arquivar o último ativo: %d", rec.Code)
	}
}

func TestDashboard_ShowsTheBookSelector(t *testing.T) {
	h, db := newTestHandler(t)
	_, _ = db.CreateBook("PJ", "sky", 0)
	body := getDashboard(h, "/?month=2026-10")
	for _, want := range []string{`data-book-bar`, "Pessoal", "PJ", `name="livro"`, `action="/livros"`} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
	_, report := getReport(h, "/relatorio?ano=2026")
	if !strings.Contains(report, `data-book-bar`) {
		t.Error("o relatório deveria ter o seletor de livro")
	}
}

func TestBookQuery_UsesTheSameSourceAsResolveBook(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/transactions?livro=2", strings.NewReader("livro=3"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_ = r.ParseForm()
	if got := bookQuery(r); got != "&livro=2" {
		t.Errorf("bookQuery = %q, quer o livro da URL (como resolveBook)", got)
	}
}

func TestReport_InvalidBookIsNotCarriedInLinks(t *testing.T) {
	h, _ := newTestHandler(t)
	_, body := getReport(h, "/relatorio?ano=2026&livro=9999")
	if !strings.Contains(body, `href="/relatorio?ano=2025"`) {
		t.Errorf("livro inválido não deveria entrar nos links: %.300s", body)
	}
}

func TestCreateBook_RejectsOversizedBody(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := post(h.CreateBook, "/livros", url.Values{"name": {strings.Repeat("a", 100<<10)}, "color": {"sky"}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, quero 400", rec.Code)
	}
}
