package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"finpi/internal/database"
	"finpi/internal/models"
)

// accountStatus traduz um erro de conta que é culpa do pedido em status HTTP (ok=false: erro interno).
func accountStatus(err error) (status int, ok bool) {
	switch {
	case errors.Is(err, database.ErrAccountName), errors.Is(err, database.ErrAccountArchived):
		return http.StatusBadRequest, true
	case errors.Is(err, database.ErrAccountDuplicate):
		return http.StatusConflict, true
	case errors.Is(err, database.ErrAccountNotFound):
		return http.StatusNotFound, true
	}
	return 0, false
}

// finishAccountAction conclui uma ação de contas: sucesso avisa as outras telas; erro volta à tela de Livros
// (pop-up aberto) com a mensagem.
func (h *Handler) finishAccountAction(w http.ResponseWriter, r *http.Request, err error, onOK func()) {
	if err == nil {
		h.events.Publish()
		onOK()
		return
	}
	status, ok := accountStatus(err)
	if !ok {
		serverError(w, "Erro ao salvar a conta", err)
		return
	}
	h.renderPage(w, r, pageOpts{Status: status, BookError: err.Error(), ShowBooks: true})
}

// CreateAccount cria uma conta no livro indicado.
func (h *Handler) CreateAccount(w http.ResponseWriter, r *http.Request) {
	if !bookForm(w, r) {
		return
	}
	db, book, ok := h.postScope(w, r)
	if !ok {
		return
	}
	_, err := db.CreateAccount(r.PostForm.Get("name"), "")
	h.finishAccountAction(w, r, err, func() { redirectToBooks(w, r, book.ID, true) })
}

// UpdateAccount renomeia uma conta do livro.
func (h *Handler) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	if !bookForm(w, r) {
		return
	}
	db, book, ok := h.postScope(w, r)
	if !ok {
		return
	}
	id := parseID(r.PostForm.Get("id"))
	if id == 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	err := db.RenameAccount(id, r.PostForm.Get("name"))
	h.finishAccountAction(w, r, err, func() { redirectToBooks(w, r, book.ID, true) })
}

// ArchiveAccount arquiva (arquivar=1) ou reativa (arquivar=0) uma conta do livro.
func (h *Handler) ArchiveAccount(w http.ResponseWriter, r *http.Request) {
	if !bookForm(w, r) {
		return
	}
	db, book, ok := h.postScope(w, r)
	if !ok {
		return
	}
	id := parseID(r.PostForm.Get("id"))
	if id == 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	err := db.SetAccountArchived(id, r.PostForm.Get("arquivar") == "1")
	h.finishAccountAction(w, r, err, func() { redirectToBooks(w, r, book.ID, true) })
}

// transactionAccount lê o campo conta do formulário de lançamento. present diz se o campo veio no envio:
// vazio ou 0 = sem conta; número inválido vira ErrAccountNotFound.
func transactionAccount(form url.Values) (id int64, present bool, err error) {
	vals, present := form["conta"]
	if !present {
		return 0, false, nil
	}
	raw := ""
	if len(vals) > 0 {
		raw = strings.TrimSpace(vals[0])
	}
	if raw == "" || raw == "0" {
		return 0, true, nil
	}
	n, perr := strconv.ParseInt(raw, 10, 64)
	if perr != nil || n < 0 {
		return 0, true, database.ErrAccountNotFound
	}
	return n, true, nil
}

// accountNameMap devolve id → nome das contas do livro (para o selo do histórico).
func accountNameMap(accounts []models.Account) map[int64]string {
	names := make(map[int64]string, len(accounts))
	for _, a := range accounts {
		names[a.ID] = a.Name
	}
	return names
}

// activeAccounts filtra as contas não arquivadas (as que aparecem nas listas de escolha).
func activeAccounts(all []models.Account) []models.Account {
	var out []models.Account
	for _, a := range all {
		if !a.Archived {
			out = append(out, a)
		}
	}
	return out
}

// accountsByBook agrupa as contas de todos os livros pelo id do livro.
func accountsByBook(all []models.Account) map[int64][]models.Account {
	m := map[int64][]models.Account{}
	for _, a := range all {
		m[a.BookID] = append(m[a.BookID], a)
	}
	return m
}
