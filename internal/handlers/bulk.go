package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/bulk"
	"finpi/internal/database"
	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/textutil"
)

const (
	maxBulkIDs  = 500
	maxBulkBody = 1 << 20
)

type bulkRequest struct {
	Action string // "editar", "excluir" ou "mover"
	IDs    []int64
	Spec   bulk.Spec
	Dest   int64 // livro de destino (só em "mover")
}

type bulkValidationError struct{ msg string }

func (e *bulkValidationError) Error() string { return e.msg }

// serverFault marca uma falha do servidor (não do pedido) dentro de parseBulkRequest: vira HTTP 500.
type serverFault struct{ err error }

func (e *serverFault) Error() string { return e.err.Error() }

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// parseBulkRequest lê e valida o pedido (ação, ids e a edição). Nunca confia no que veio do navegador.
func parseBulkRequest(db *database.DB, form url.Values) (bulkRequest, error) {
	var req bulkRequest

	switch a := strings.TrimSpace(form.Get("acao")); a {
	case "editar", "excluir", "mover":
		req.Action = a
	default:
		return req, errors.New("Ação inválida.")
	}

	seen := map[int64]bool{}
	for _, raw := range form["ids"] {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 {
			return req, errors.New("Lançamentos inválidos.")
		}
		if !seen[id] {
			seen[id] = true
			req.IDs = append(req.IDs, id)
		}
		if len(req.IDs) > maxBulkIDs {
			return req, fmt.Errorf("Selecione no máximo %d lançamentos por vez.", maxBulkIDs)
		}
	}
	if len(req.IDs) == 0 {
		return req, errors.New("Selecione ao menos um lançamento.")
	}
	if req.Action == "mover" {
		dest, err := strconv.ParseInt(strings.TrimSpace(form.Get("livro_destino")), 10, 64)
		if err != nil || dest <= 0 {
			return req, errors.New("Escolha o livro de destino.")
		}
		if dest == db.BookID() {
			return req, errors.New("O livro de destino é o mesmo de origem.")
		}
		if b, err := db.GetBook(dest); err != nil || b.Archived {
			return req, errors.New("Livro de destino inválido ou arquivado.")
		}
		req.Dest = dest
		return req, nil
	}
	if req.Action == "excluir" {
		return req, nil
	}

	spec := bulk.Spec{Find: form.Get("localizar"), Replace: form.Get("substituir")}

	if c := strings.TrimSpace(form.Get("categoria")); c != "" {
		categories, err := db.ListCategories()
		if err != nil {
			return req, &serverFault{err}
		}
		for _, cat := range categories {
			if textutil.Fold(cat.Name) == textutil.Fold(c) {
				spec.Category = cat.Name
			}
		}
		if spec.Category == "" {
			return req, errors.New("Categoria inexistente.")
		}
	}

	switch p := strings.TrimSpace(form.Get("prioridade")); p {
	case "":
	case "seguir":
		spec.Priority = bulk.PriorityFollow
	case models.PriorityEssential, models.PrioritySuperfluous:
		spec.Priority = p
	default:
		return req, errors.New("Prioridade inválida.")
	}

	switch m := strings.TrimSpace(form.Get("data_modo")); m {
	case "":
	case "definir":
		spec.DateMode, spec.DateSet = bulk.DateSet, strings.TrimSpace(form.Get("data"))
	case "deslocar":
		n, err := strconv.Atoi(strings.TrimSpace(form.Get("desloca")))
		if err != nil {
			return req, errors.New("Deslocamento inválido: informe um número, como 1 ou -3.")
		}
		spec.DateMode, spec.ShiftN = bulk.DateShift, n
		switch strings.TrimSpace(form.Get("desloca_unidade")) {
		case "dias":
			spec.ShiftUnit = bulk.UnitDays
		case "meses":
			spec.ShiftUnit = bulk.UnitMonths
		}
	default:
		return req, errors.New("Modo de data inválido.")
	}

	if err := spec.Validate(); err != nil {
		return req, errors.New(capitalize(err.Error()) + ".")
	}
	if spec.Empty() {
		return req, errors.New("Escolha ao menos uma alteração.")
	}
	req.Spec = spec
	return req, nil
}

// moveRow é uma linha da prévia de movimentação: o lançamento e a categoria que ele terá no destino.
type moveRow struct {
	Tx      models.Transaction
	DestCat string
	NewCat  bool // a categoria não existe no destino e será criada
}

// bulkParam é um campo da operação que a prévia repete em campos ocultos para o "aplicar".
type bulkParam struct{ Name, Value string }

// bulkView são os dados do trecho de HTML da prévia.
type bulkView struct {
	Action           string
	Rows             []bulk.Row           // edição
	Delete           []models.Transaction // exclusão
	Move             []moveRow            // movimentação
	DestName         string               // nome do livro de destino
	PartialSeries    []string             // séries de parcelas que ficariam divididas
	Changed          int
	Unchanged        int
	Missing          int
	Params           []bulkParam
	SelectedMonth    string
	IncludeProjected bool
	Filter           models.FilterView
	BookID           int64 // livro da operação, repetido no "aplicar"
}

var bulkParamNames = []string{"acao", "categoria", "prioridade", "data_modo", "data", "desloca", "desloca_unidade", "localizar", "substituir", "livro_destino"}

func bulkMonth(r *http.Request) string {
	if m := r.PostForm.Get("month"); m != "" {
		return m
	}
	return time.Now().Format("2006-01")
}

// bulkForm confere o método, lê o formulário, resolve o livro e valida o pedido; devolve false se já respondeu.
func (h *Handler) bulkForm(w http.ResponseWriter, r *http.Request) (bulkRequest, *database.DB, models.Book, bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return bulkRequest{}, nil, models.Book{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBulkBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Dados do formulário inválidos ou grandes demais", http.StatusBadRequest)
		return bulkRequest{}, nil, models.Book{}, false
	}
	db, book, ok := h.postScope(w, r)
	if !ok {
		return bulkRequest{}, nil, models.Book{}, false
	}
	req, err := parseBulkRequest(db, r.PostForm)
	if err != nil {
		var sf *serverFault
		if errors.As(err, &sf) {
			serverError(w, "Erro ao carregar categorias", sf.err)
			return bulkRequest{}, nil, models.Book{}, false
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return bulkRequest{}, nil, models.Book{}, false
	}
	return req, db, book, true
}

// BulkPreview devolve a prévia (antes → depois) da edição ou da exclusão em lote. Não grava nada.
func (h *Handler) BulkPreview(w http.ResponseWriter, r *http.Request) {
	req, db, book, ok := h.bulkForm(w, r)
	if !ok {
		return
	}
	list, err := db.GetTransactionsByIDs(req.IDs)
	if err != nil {
		serverError(w, "Erro ao carregar lançamentos", err)
		return
	}
	if len(list) == 0 {
		http.Error(w, "Os lançamentos selecionados não existem mais.", http.StatusBadRequest)
		return
	}

	view := bulkView{
		Action: req.Action, Missing: len(req.IDs) - len(list), SelectedMonth: bulkMonth(r),
		IncludeProjected: wantsProjected(r), Filter: filterView(formFilters(r), nil, nil), BookID: book.ID,
	}
	for _, name := range bulkParamNames {
		if v := r.PostForm.Get(name); v != "" {
			view.Params = append(view.Params, bulkParam{name, v})
		}
	}
	if req.Action == "mover" {
		dest := db.ForBook(req.Dest)
		destBook, _ := db.GetBook(req.Dest)
		cats, err := dest.ListCategories()
		if err != nil {
			serverError(w, "Erro ao carregar categorias do destino", err)
			return
		}
		byKey := map[string]string{}
		for _, c := range cats {
			byKey[textutil.Fold(c.Name)] = c.Name
		}
		for _, t := range list {
			name, ok := byKey[textutil.Fold(t.Category)]
			if !ok {
				name = t.Category
			}
			view.Move = append(view.Move, moveRow{Tx: t, DestCat: name, NewCat: !ok})
		}
		view.DestName = destBook.Name
		if cands, err := db.GetInstallmentCandidates(); err == nil {
			view.PartialSeries = installments.PartialSeries(list, cands)
		}
		view.Changed = len(list)
	} else if req.Action == "excluir" {
		view.Delete = list
	} else {
		view.Rows = bulk.Preview(list, req.Spec)
		for _, row := range view.Rows {
			if row.Err != "" {
				http.Error(w, fmt.Sprintf("Lançamento %q: %s.", row.Before.Description, row.Err), http.StatusBadRequest)
				return
			}
			if row.Changed {
				view.Changed++
			} else {
				view.Unchanged++
			}
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "bulkPreview", view); err != nil {
		serverError(w, "Erro ao renderizar a prévia", err)
	}
}

// bulkRedirect volta para a página mantendo o mês, as previstas e os filtros, e acrescenta o aviso.
func bulkRedirect(w http.ResponseWriter, r *http.Request, params string) {
	target := "/?month=" + url.QueryEscape(bulkMonth(r))
	if r.PostForm.Get("previstas") == "1" {
		target += "&previstas=1"
	}
	http.Redirect(w, r, withFilters(target, r)+"&"+params, http.StatusSeeOther)
}

// BulkApply revalida a operação e a grava numa única transação (com o lote que permite desfazer).
func (h *Handler) BulkApply(w http.ResponseWriter, r *http.Request) {
	req, db, _, ok := h.bulkForm(w, r)
	if !ok {
		return
	}

	var (
		applied, skipped int
		err              error
		param            string
	)
	if req.Action == "mover" {
		applied, skipped, err = db.MoveToBook(req.IDs, req.Dest)
		param = "movidos"
	} else if req.Action == "excluir" {
		summary := fmt.Sprintf("Exclusão de %d %s", len(req.IDs), pluralWord(len(req.IDs), "lançamento", "lançamentos"))
		applied, skipped, err = db.ApplyDeletes(summary, req.IDs)
		param = "excluidos"
	} else {
		applied, skipped, err = db.ApplyEdits(bulk.Summary(req.Spec), req.IDs, func(t models.Transaction) (models.Transaction, error) {
			after, err := bulk.Apply(t, req.Spec)
			if err != nil {
				return t, &bulkValidationError{fmt.Sprintf("Lançamento %q: %s.", t.Description, err)}
			}
			return after, nil
		})
		param = "editados"
	}
	if err != nil {
		var ve *bulkValidationError
		if errors.As(err, &ve) {
			http.Error(w, ve.msg, http.StatusBadRequest)
			return
		}
		if errors.Is(err, database.ErrBookNotFound) || errors.Is(err, database.ErrSameBook) {
			http.Error(w, "Livro de destino inválido.", http.StatusBadRequest)
			return
		}
		if errors.Is(err, database.ErrInvalidPriority) {
			http.Error(w, "Prioridade inválida.", http.StatusBadRequest)
			return
		}
		serverError(w, "Erro ao gravar a edição em lote", err)
		return
	}
	if applied > 0 {
		h.events.Publish()
	}
	bulkRedirect(w, r, fmt.Sprintf("%s=%d&ignorados=%d", param, applied, skipped))
}

// BulkUndo desfaz a edição em lote mais recente que ainda não foi desfeita.
func (h *Handler) BulkUndo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Dados do formulário inválidos", http.StatusBadRequest)
		return
	}
	db, _, ok := h.postScope(w, r)
	if !ok {
		return
	}
	restored, skipped, err := db.UndoLatestBatch()
	if errors.Is(err, database.ErrNoBatch) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w, "Erro ao desfazer a edição em lote", err)
		return
	}
	if restored > 0 {
		h.events.Publish()
	}
	bulkRedirect(w, r, fmt.Sprintf("desfeitos=%d&pulados=%d", restored, skipped))
}
