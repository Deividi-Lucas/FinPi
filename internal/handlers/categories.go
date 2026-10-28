package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/database"
)

// categoryForm confere o método e lê o formulário; devolve false (já respondido) se não puder seguir.
func categoryForm(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return false
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Dados do formulário inválidos", http.StatusBadRequest)
		return false
	}
	return true
}

func categoryID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("id")), 10, 64)
	return id, err == nil && id > 0
}

// redirectToCategories volta para a página com o pop-up de categorias aberto, mantendo mês e previstas.
func redirectToCategories(w http.ResponseWriter, r *http.Request) {
	month := r.PostForm.Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	target := "/?month=" + url.QueryEscape(month) + "&categorias=1"
	if r.PostForm.Get("previstas") == "1" {
		target += "&previstas=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// finishCategoryAction conclui uma ação do menu: sucesso avisa as outras telas e redireciona;
// erro do usuário volta para a página com a mensagem dentro do pop-up.
func (h *Handler) finishCategoryAction(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		h.events.Publish()
		redirectToCategories(w, r)
		return
	}

	var inUse *database.CategoryInUseError
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, database.ErrInvalidCategoryName), errors.Is(err, database.ErrInvalidPriority):
		status = http.StatusBadRequest
	case errors.Is(err, database.ErrCategoryExists), errors.As(err, &inUse):
		status = http.StatusConflict
	case errors.Is(err, database.ErrCategoryNotFound):
		status = http.StatusNotFound
	}
	if status == http.StatusInternalServerError {
		serverError(w, "Erro ao salvar categoria", err)
		return
	}
	h.renderDashboard(w, r, status, err.Error(), true)
}

// CreateCategory cria uma categoria pelo menu.
func (h *Handler) CreateCategory(w http.ResponseWriter, r *http.Request) {
	if !categoryForm(w, r) {
		return
	}
	_, err := h.db.CreateCategory(r.PostForm.Get("name"))
	h.finishCategoryAction(w, r, err)
}

// RenameCategory renomeia uma categoria (e os lançamentos que a usam).
func (h *Handler) RenameCategory(w http.ResponseWriter, r *http.Request) {
	if !categoryForm(w, r) {
		return
	}
	id, ok := categoryID(r)
	if !ok {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	h.finishCategoryAction(w, r, h.db.RenameCategory(id, r.PostForm.Get("name")))
}

// SetCategoryVisibility oculta (hidden=1) ou reexibe (hidden=0) uma categoria nas listas de escolha.
func (h *Handler) SetCategoryVisibility(w http.ResponseWriter, r *http.Request) {
	if !categoryForm(w, r) {
		return
	}
	id, ok := categoryID(r)
	if !ok {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	h.finishCategoryAction(w, r, h.db.SetCategoryHidden(id, r.PostForm.Get("hidden") == "1"))
}

// SetCategoryPriority classifica a categoria como essencial, supérfluo ou sem classificação (vazio).
func (h *Handler) SetCategoryPriority(w http.ResponseWriter, r *http.Request) {
	if !categoryForm(w, r) {
		return
	}
	id, ok := categoryID(r)
	if !ok {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	h.finishCategoryAction(w, r, h.db.SetCategoryPriority(id, strings.TrimSpace(r.PostForm.Get("priority"))))
}

// DeleteCategory exclui uma categoria sem lançamentos.
func (h *Handler) DeleteCategory(w http.ResponseWriter, r *http.Request) {
	if !categoryForm(w, r) {
		return
	}
	id, ok := categoryID(r)
	if !ok {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	h.finishCategoryAction(w, r, h.db.DeleteCategory(id))
}
