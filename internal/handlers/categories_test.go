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

func findCat(t *testing.T, db *database.DB, name string) models.Category {
	t.Helper()
	list, err := db.ListCategories()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("categoria %q não encontrada", name)
	return models.Category{}
}

func TestCreateCategory_RedirectsAndKeepsState(t *testing.T) {
	h, db := newTestHandler(t)

	rec := post(h.CreateCategory, "/categories", url.Values{"name": {"Pets"}, "month": {"2026-10"}, "previstas": {"1"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?month=2026-10&categorias=1&previstas=1" {
		t.Errorf("Location = %q", loc)
	}
	if !categoryExists(t, db, "Pets") {
		t.Error("categoria deveria ter sido criada")
	}
}

func TestCreateCategory_DuplicateShowsErrorInsideModal(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := post(h.CreateCategory, "/categories", url.Values{"name": {"saude"}, "month": {"2026-10"}})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, quer 409", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `id="categories-modal" data-open="true"`) {
		t.Error("o pop-up de categorias deveria voltar aberto")
	}
	if !strings.Contains(body, "já existe uma categoria") {
		t.Error("a mensagem de erro deveria aparecer na página")
	}
}

func TestCreateCategory_InvalidNameReturns400(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := post(h.CreateCategory, "/categories", url.Values{"name": {"  "}})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, quer 400", rec.Code)
	}
}

func TestRenameCategory_PropagatesToTransactions(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 100) // categoria "Alimentação"
	cat := findCat(t, db, "Alimentação")

	rec := post(h.RenameCategory, "/categories/rename", url.Values{
		"id": {strconv.FormatInt(cat.ID, 10)}, "name": {"Comida"}, "month": {"2026-10"},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].ID != tx.ID || list[0].Category != "Comida" {
		t.Errorf("lançamento deveria acompanhar a renomeação: %+v", list)
	}
}

func TestRenameCategory_ErrorsRenderModal(t *testing.T) {
	h, db := newTestHandler(t)
	cat := findCat(t, db, "Lazer")

	dup := post(h.RenameCategory, "/categories/rename", url.Values{"id": {strconv.FormatInt(cat.ID, 10)}, "name": {"Moradia"}})
	if dup.Code != http.StatusConflict {
		t.Errorf("nome repetido: status = %d, quer 409", dup.Code)
	}
	missing := post(h.RenameCategory, "/categories/rename", url.Values{"id": {"9999"}, "name": {"X"}})
	if missing.Code != http.StatusNotFound {
		t.Errorf("categoria inexistente: status = %d, quer 404", missing.Code)
	}
	badID := post(h.RenameCategory, "/categories/rename", url.Values{"id": {"abc"}, "name": {"X"}})
	if badID.Code != http.StatusBadRequest {
		t.Errorf("id inválido: status = %d, quer 400", badID.Code)
	}
}

func TestSetCategoryVisibility(t *testing.T) {
	h, db := newTestHandler(t)
	id := strconv.FormatInt(findCat(t, db, "Lazer").ID, 10)

	if rec := post(h.SetCategoryVisibility, "/categories/visibility", url.Values{"id": {id}, "hidden": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if !findCat(t, db, "Lazer").Hidden {
		t.Error("deveria estar oculta")
	}

	post(h.SetCategoryVisibility, "/categories/visibility", url.Values{"id": {id}, "hidden": {"0"}})
	if findCat(t, db, "Lazer").Hidden {
		t.Error("deveria estar visível")
	}
}

func TestDeleteCategory_InUseIsBlockedWithMessage(t *testing.T) {
	h, db := newTestHandler(t)
	seed(t, db, 100) // usa "Alimentação"
	id := strconv.FormatInt(findCat(t, db, "Alimentação").ID, 10)

	rec := post(h.DeleteCategory, "/categories/delete", url.Values{"id": {id}, "month": {"2026-10"}})

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, quer 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "está em uso por 1 lançamento") {
		t.Error("a mensagem deveria informar quantos lançamentos usam a categoria")
	}
	if !categoryExists(t, db, "Alimentação") {
		t.Error("categoria em uso não pode ser excluída")
	}
}

func TestDeleteCategory_UnusedIsDeleted(t *testing.T) {
	h, db := newTestHandler(t)
	id := strconv.FormatInt(findCat(t, db, "Outros").ID, 10)

	rec := post(h.DeleteCategory, "/categories/delete", url.Values{"id": {id}, "month": {"2026-10"}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, quer 303", rec.Code)
	}
	if categoryExists(t, db, "Outros") {
		t.Error("categoria sem uso deveria ser excluída")
	}
}

func TestCategoryMutationsPublishEvents(t *testing.T) {
	h, db := newTestHandler(t)
	ch, unsub, err := h.events.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	post(h.CreateCategory, "/categories", url.Values{"name": {"Pets"}})
	if !pending(ch) {
		t.Error("criar categoria deveria publicar evento")
	}
	id := strconv.FormatInt(findCat(t, db, "Pets").ID, 10)
	post(h.SetCategoryVisibility, "/categories/visibility", url.Values{"id": {id}, "hidden": {"1"}})
	if !pending(ch) {
		t.Error("ocultar deveria publicar evento")
	}
	post(h.CreateCategory, "/categories", url.Values{"name": {"  "}})
	if pending(ch) {
		t.Error("erro não deve publicar evento")
	}
}

func TestCategoryHandlers_RejectGET(t *testing.T) {
	h, _ := newTestHandler(t)
	for name, handler := range map[string]http.HandlerFunc{
		"create": h.CreateCategory, "rename": h.RenameCategory,
		"visibility": h.SetCategoryVisibility, "delete": h.DeleteCategory,
	} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodGet, "/categories", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, quer 405", name, rec.Code)
		}
	}
}

func TestDashboard_OpensCategoriesModalFromQuery(t *testing.T) {
	h, _ := newTestHandler(t)

	if body := getDashboard(h, "/?month=2026-10&categorias=1"); !strings.Contains(body, `id="categories-modal" data-open="true"`) {
		t.Error("categorias=1 deveria abrir o pop-up")
	}
	if body := getDashboard(h, "/?month=2026-10"); !strings.Contains(body, `id="categories-modal" data-open="false"`) {
		t.Error("por padrão o pop-up fica fechado")
	}
}

func TestDashboard_CategorySelectsComeFromDatabase(t *testing.T) {
	h, db := newTestHandler(t)
	post(h.CreateCategory, "/categories", url.Values{"name": {"Pets"}})
	if err := db.SetCategoryHidden(findCat(t, db, "Lazer").ID, true); err != nil {
		t.Fatal(err)
	}

	body := getDashboard(h, "/?month=2026-10")

	if !strings.Contains(body, `<option value="Pets" data-priority="">Pets</option>`) {
		t.Error("a categoria criada deveria aparecer na lista de escolha")
	}
	if strings.Contains(body, `<option value="Lazer">`) {
		t.Error("categoria oculta não deve aparecer na lista de escolha")
	}
	if !strings.Contains(body, `<option value="__new__">`) {
		t.Error("a lista deveria oferecer '+ Nova categoria…'")
	}
	if !strings.Contains(body, `name="new_category"`) {
		t.Error("deveria existir o campo do nome da nova categoria")
	}
}
