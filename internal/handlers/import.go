package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"finpi/internal/models"
	"finpi/internal/statement"
	"finpi/internal/textutil"
)

const (
	maxImportBody    = statement.MaxFileBytes + 1<<20 // arquivo + folga do multipart
	maxConfirmBody   = 8 << 20
	maxImportDescLen = 200
	maxExternalIDLen = 200
	maxImportOrigLen = 300 // runas da descrição original guardada
)

// importView são os dados do trecho de HTML da revisão.
type importView struct {
	FileName         string
	Preview          statement.Preview
	Options          []string // categorias da lista de cada linha
	SelectedMonth    string
	IncludeProjected bool
	Filter           models.FilterView // vazio: o trecho da revisão reaproveita o stateFields da página
	Book             models.Book       // livro que recebe a importação
	BookID           int64
	Bank             string          // rótulo do banco do extrato
	Format           string          // formato do arquivo, em maiúsculas
	NewOptions       map[string]bool // opções que ainda não existem no livro (criadas na confirmação)
}

func importError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, statement.ErrTooLarge) {
		status = http.StatusRequestEntityTooLarge
	}
	http.Error(w, err.Error(), status)
}

// ImportPreview lê o extrato enviado e devolve o trecho de HTML da revisão. Não grava nada.
func (h *Handler) ImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxImportBody)
	// O limite de memória cobre o corpo inteiro: assim o upload nunca vai para arquivo temporário (cartão SD).
	if err := r.ParseMultipartForm(maxImportBody); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			importError(w, statement.ErrTooLarge)
			return
		}
		http.Error(w, "Envie um arquivo de extrato (.xlsx, .csv ou .ofx).", http.StatusBadRequest)
		return
	}
	db, book, ok := h.postScope(w, r)
	if !ok {
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Envie um arquivo de extrato (.xlsx, .csv ou .ofx).", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, statement.MaxFileBytes+1))
	if err != nil {
		http.Error(w, "Não foi possível ler o arquivo enviado.", http.StatusBadRequest)
		return
	}

	st, err := statement.Parse(header.Filename, data)
	if err != nil {
		importError(w, err)
		return
	}

	categories, err := db.ListCategories()
	if err != nil {
		serverError(w, "Erro ao carregar categorias", err)
		return
	}
	var options []string
	for _, c := range visibleCategories(categories) {
		options = append(options, c.Name)
	}
	// "Outros" e "Movimentação interna" sempre aparecem: se o livro não as tem, a confirmação as cria.
	newOptions := map[string]bool{}
	for _, name := range []string{"Outros", models.InternalCategory} {
		if hasFold(options, name) {
			continue
		}
		if hasCategory(categories, name) { // existe, mas está oculta
			options = append(options, name)
			continue
		}
		options = append(options, name)
		newOptions[name] = true
	}

	existing, err := db.ImportedExternalIDs()
	if err != nil {
		serverError(w, "Erro ao consultar importações anteriores", err)
		return
	}
	minDate, maxDate := st.Rows[0].Date, st.Rows[0].Date
	for _, row := range st.Rows {
		if row.Date < minDate {
			minDate = row.Date
		}
		if row.Date > maxDate {
			maxDate = row.Date
		}
	}
	manual, err := db.ManualTransactionKeys(minDate, maxDate)
	if err != nil {
		serverError(w, "Erro ao consultar lançamentos existentes", err)
		return
	}
	byDesc, err := db.LatestCategoryByDescription()
	if err != nil {
		serverError(w, "Erro ao consultar o histórico de categorias", err)
		return
	}

	preview := statement.BuildPreview(st, statement.PreviewInput{
		Bank: st.Bank, ExistingIDs: existing, ManualKeys: manual, CategoryByDesc: byDesc, Categories: options,
	})
	preview.Bank = st.Bank
	preview.Format = st.Format

	// A categoria sugerida pode estar oculta: precisa existir como opção da linha.
	have := map[string]bool{}
	for _, o := range options {
		have[o] = true
	}
	for _, row := range preview.Rows {
		if row.Category != "" && !have[row.Category] {
			options = append(options, row.Category)
			have[row.Category] = true
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := importView{
		FileName: header.Filename, Preview: preview, Options: options,
		SelectedMonth: requestMonth(r), IncludeProjected: wantsProjected(r), Book: book, BookID: book.ID,
		Bank: st.Bank.Label(), Format: strings.ToUpper(st.Format), NewOptions: newOptions,
	}
	if err := h.tmpl.ExecuteTemplate(w, "importPreview", view); err != nil {
		serverError(w, "Erro ao renderizar a revisão", err)
	}
}

// hasFold diz se a lista tem o nome, ignorando maiúsculas e acentos.
func hasFold(list []string, name string) bool {
	key := textutil.Fold(name)
	for _, s := range list {
		if textutil.Fold(s) == key {
			return true
		}
	}
	return false
}

// hasCategory diz se o livro já tem a categoria (visível ou oculta), ignorando maiúsculas e acentos.
func hasCategory(cats []models.Category, name string) bool {
	key := textutil.Fold(name)
	for _, c := range cats {
		if textutil.Fold(c.Name) == key {
			return true
		}
	}
	return false
}

// parseImportRow valida a linha i do formulário de confirmação (nunca confia no que veio do navegador).
func parseImportRow(form url.Values, i int, canonical map[string]string) (models.Transaction, error) {
	var t models.Transaction
	s := strconv.Itoa(i)

	date := strings.TrimSpace(form.Get("date_" + s))
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return t, errors.New("data inválida")
	}
	desc := strings.Join(strings.Fields(form.Get("desc_"+s)), " ")
	if desc == "" {
		return t, errors.New("descrição é obrigatória")
	}
	if utf8.RuneCountInString(desc) > maxImportDescLen {
		return t, errors.New("descrição longa demais")
	}
	cents, err := strconv.ParseInt(strings.TrimSpace(form.Get("amount_"+s)), 10, 64)
	if err != nil || cents == 0 || cents > maxAmountCents || cents < -maxAmountCents {
		return t, errors.New("valor inválido")
	}
	ref := strings.TrimSpace(form.Get("ref_" + s))
	if !statement.ValidExternalID(ref) || len(ref) > maxExternalIDLen {
		return t, errors.New("identificador inválido")
	}
	category, ok := canonical[textutil.Fold(form.Get("cat_"+s))]
	if !ok {
		return t, errors.New("categoria inexistente")
	}
	orig := strings.Join(strings.Fields(form.Get("orig_"+s)), " ")
	if utf8.RuneCountInString(orig) > maxImportOrigLen {
		orig = string([]rune(orig)[:maxImportOrigLen])
	}

	t = models.Transaction{Description: desc, Date: date, Category: category, ExternalID: ref, Type: "income", AmountCents: cents, OriginalDescription: orig}
	if cents < 0 {
		t.Type, t.AmountCents = "expense", -cents
	}
	return t, nil
}

// ImportConfirm revalida as linhas selecionadas e as grava numa única transação.
func (h *Handler) ImportConfirm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConfirmBody)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Dados da importação inválidos ou grandes demais", http.StatusBadRequest)
		return
	}

	db, _, ok := h.postScope(w, r)
	if !ok {
		return
	}

	n, err := strconv.Atoi(strings.TrimSpace(r.PostForm.Get("n")))
	if err != nil || n < 0 || n > statement.MaxRows {
		http.Error(w, "Dados da importação inválidos", http.StatusBadRequest)
		return
	}

	categories, err := db.ListCategories()
	if err != nil {
		serverError(w, "Erro ao carregar categorias", err)
		return
	}
	canonical := map[string]string{}
	for _, c := range categories {
		canonical[textutil.Fold(c.Name)] = c.Name
	}
	// Só estas duas podem nascer na confirmação (a prévia as oferece mesmo num livro sem categorias).
	creatable := map[string]string{textutil.Fold("Outros"): "Outros", textutil.Fold(models.InternalCategory): models.InternalCategory}
	for i := 0; i < n; i++ {
		if r.PostForm.Get("sel_"+strconv.Itoa(i)) != "1" {
			continue
		}
		key := textutil.Fold(r.PostForm.Get("cat_" + strconv.Itoa(i)))
		if _, have := canonical[key]; have {
			continue
		}
		if name, ok := creatable[key]; ok {
			c, err := db.GetOrCreateCategory(name)
			if err != nil {
				serverError(w, "Erro ao criar a categoria", err)
				return
			}
			canonical[key] = c.Name
		}
	}

	var rows []models.Transaction
	latest := ""
	for i := 0; i < n; i++ {
		if r.PostForm.Get("sel_"+strconv.Itoa(i)) != "1" {
			continue
		}
		t, err := parseImportRow(r.PostForm, i, canonical)
		if err != nil {
			http.Error(w, fmt.Sprintf("Linha %d: %v", i+1, err), http.StatusBadRequest)
			return
		}
		rows = append(rows, t)
		if t.Date > latest {
			latest = t.Date
		}
	}
	if len(rows) == 0 {
		http.Error(w, "Nenhuma linha selecionada", http.StatusBadRequest)
		return
	}

	inserted, skipped, err := db.ImportTransactions(rows)
	if err != nil {
		serverError(w, "Erro ao gravar a importação", err)
		return
	}
	if inserted > 0 {
		h.events.Publish()
	}

	target := fmt.Sprintf("/?month=%s&importados=%d&ignorados=%d", url.QueryEscape(latest[:7]), inserted, skipped) + bookQuery(r)
	if r.PostForm.Get("previstas") == "1" {
		target += "&previstas=1"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}
