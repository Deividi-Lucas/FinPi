package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"finpi/internal/database"
	"finpi/internal/models"
)

var errNoActiveBook = errors.New("nenhum livro ativo")

// resolveBook descobre o livro do pedido: parâmetro livro da URL ou, em envios de formulário, do corpo. Sem
// livro, ou com livro inexistente/arquivado, usa o primeiro livro ativo (o Pessoal); neste último caso invalid é true.
func (h *Handler) resolveBook(r *http.Request) (db *database.DB, book models.Book, invalid bool, err error) {
	db, book, _, invalid, err = h.bookScope(r)
	return db, book, invalid, err
}

// bookScope é o resolveBook que também devolve a lista de todos os livros (para o seletor), sem consultar duas vezes.
func (h *Handler) bookScope(r *http.Request) (db *database.DB, book models.Book, books []models.Book, invalid bool, err error) {
	raw := requestBook(r)
	books, err = h.db.ListBooks()
	if err != nil {
		return nil, book, nil, false, err
	}
	var def, found *models.Book
	for i := range books {
		b := &books[i]
		if b.Archived {
			continue
		}
		if def == nil {
			def = b
		}
		if raw == strconv.FormatInt(b.ID, 10) {
			found = b
		}
	}
	if def == nil {
		return nil, book, books, false, errNoActiveBook
	}
	if raw != "" && found == nil {
		invalid = true
	}
	if found == nil {
		found = def
	}
	return h.db.ForBook(found.ID), *found, books, invalid, nil
}

// postScope resolve o livro de um envio de formulário (já lido com ParseForm). Livro inválido vira 400.
func (h *Handler) postScope(w http.ResponseWriter, r *http.Request) (*database.DB, models.Book, bool) {
	db, book, invalid, err := h.resolveBook(r)
	if err != nil {
		serverError(w, "Erro ao carregar os livros", err)
		return nil, book, false
	}
	if invalid {
		http.Error(w, "Livro inválido ou arquivado.", http.StatusBadRequest)
		return nil, book, false
	}
	return db, book, true
}

// requestBook lê o livro pedido: parâmetro livro da URL ou, sem ele, do corpo do formulário.
func requestBook(r *http.Request) string {
	if raw := strings.TrimSpace(r.URL.Query().Get("livro")); raw != "" {
		return raw
	}
	return strings.TrimSpace(r.PostForm.Get("livro"))
}

// bookQuery devolve "&livro=ID" para um endereço de retorno, ou "" se o pedido não trouxe um livro. Lê da mesma
// fonte que resolveBook, para o retorno nunca levar um livro diferente do usado na ação.
func bookQuery(r *http.Request) string {
	raw := requestBook(r)
	if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
		return "&livro=" + strconv.FormatInt(id, 10)
	}
	return ""
}

// activeBooks filtra os livros não arquivados (os que aparecem no seletor).
func activeBooks(books []models.Book) []models.Book {
	var out []models.Book
	for _, b := range books {
		if !b.Archived {
			out = append(out, b)
		}
	}
	return out
}
