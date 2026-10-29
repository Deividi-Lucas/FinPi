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

func bookForm(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Dados do formulário inválidos", http.StatusBadRequest)
		return false
	}
	return true
}

func parseID(raw string) int64 {
	id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// redirectToBooks volta para a página com o cadastro de livros aberto, no livro indicado.
func redirectToBooks(w http.ResponseWriter, r *http.Request, bookID int64, keepOpen bool) {
	month := r.PostForm.Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	target := "/?month=" + url.QueryEscape(month)
	if bookID > 0 {
		target += "&livro=" + strconv.FormatInt(bookID, 10)
	}
	if keepOpen {
		target += "&livros=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// finishBookAction conclui uma ação do cadastro: sucesso avisa as outras telas; erro volta ao pop-up com a mensagem.
func (h *Handler) finishBookAction(w http.ResponseWriter, r *http.Request, err error, onOK func()) {
	if err == nil {
		h.events.Publish()
		onOK()
		return
	}
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, database.ErrInvalidBookName), errors.Is(err, database.ErrInvalidBookColor):
		status = http.StatusBadRequest
	case errors.Is(err, database.ErrBookExists), errors.Is(err, database.ErrLastBook):
		status = http.StatusConflict
	case errors.Is(err, database.ErrBookNotFound):
		status = http.StatusNotFound
	}
	if status == http.StatusInternalServerError {
		serverError(w, "Erro ao salvar o livro", err)
		return
	}
	h.renderPage(w, r, pageOpts{Status: status, BookError: err.Error(), ShowBooks: true})
}

// CreateBook cria um livro (vazio ou copiando as categorias de outro) e abre o livro novo.
func (h *Handler) CreateBook(w http.ResponseWriter, r *http.Request) {
	if !bookForm(w, r) {
		return
	}
	b, err := h.db.CreateBook(r.PostForm.Get("name"), strings.TrimSpace(r.PostForm.Get("color")), parseID(r.PostForm.Get("copiar_de")))
	h.finishBookAction(w, r, err, func() { redirectToBooks(w, r, b.ID, false) })
}

// UpdateBook renomeia o livro e troca a cor.
func (h *Handler) UpdateBook(w http.ResponseWriter, r *http.Request) {
	if !bookForm(w, r) {
		return
	}
	id := parseID(r.PostForm.Get("id"))
	if id == 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	err := h.db.UpdateBook(id, r.PostForm.Get("name"), strings.TrimSpace(r.PostForm.Get("color")))
	h.finishBookAction(w, r, err, func() { redirectToBooks(w, r, parseID(r.PostForm.Get("livro")), true) })
}

// ArchiveBook arquiva (arquivar=1) ou reativa (arquivar=0) um livro. Arquivar o livro aberto volta ao Pessoal.
func (h *Handler) ArchiveBook(w http.ResponseWriter, r *http.Request) {
	if !bookForm(w, r) {
		return
	}
	id := parseID(r.PostForm.Get("id"))
	if id == 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}
	archive := r.PostForm.Get("arquivar") == "1"
	err := h.db.SetBookArchived(id, archive)
	h.finishBookAction(w, r, err, func() {
		current := parseID(r.PostForm.Get("livro"))
		if archive && current == id {
			current = 0 // volta ao livro padrão
		}
		redirectToBooks(w, r, current, true)
	})
}
