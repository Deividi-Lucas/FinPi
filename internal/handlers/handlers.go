package handlers

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/dashboard"
	"finpi/internal/database"
	"finpi/internal/filter"
	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/statement"
)

type Handler struct {
	db        *database.DB
	tmpl      *template.Template
	events    *Broker
	heartbeat time.Duration
	version   string // versão da build exibida no rodapé
}

// NewHandler compila os templates HTML embutidos uma única vez na inicialização,
// garantindo alta performance e baixo consumo contínuo de memória.
func NewHandler(db *database.DB, templateFS fs.FS, version string) (*Handler, error) {
	if version == "" {
		version = "dev"
	}

	funcMap := template.FuncMap{
		"formatCurrency": formatCurrencyBRL,
		"centsInput":     formatCentsInput,
		"formatDate":     formatDateBR,
		"plural": func(n int, one, many string) string {
			if n == 1 {
				return one
			}
			return many
		},
		"formatPercent": func(p float64) string {
			return strings.Replace(strconv.FormatFloat(p, 'f', 1, 64), ".", ",", 1) + "%"
		},
		"priorityLabel": func(p string) string {
			switch p {
			case "essential":
				return "Essencial"
			case "superfluous":
				return "Supérfluo"
			}
			return "—"
		},
		"dict": func(pairs ...any) map[string]any {
			m := map[string]any{}
			for i := 0; i+1 < len(pairs); i += 2 {
				if k, ok := pairs[i].(string); ok {
					m[k] = pairs[i+1]
				}
			}
			return m
		},
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("erro ao compilar templates HTML: %w", err)
	}

	return &Handler{
		db:        db,
		tmpl:      tmpl,
		events:    NewBroker(maxEventClients),
		heartbeat: defaultHeartbeat,
		version:   version,
	}, nil
}

// serverError registra o erro real no log e devolve ao usuário apenas uma mensagem genérica.
func serverError(w http.ResponseWriter, msg string, err error) {
	log.Printf("[ERRO] %s: %v", msg, err)
	http.Error(w, msg, http.StatusInternalServerError)
}

// dbError traduz o erro do banco: lançamento inexistente vira 404; o resto, 500.
func dbError(w http.ResponseWriter, msg string, err error) {
	if errors.Is(err, database.ErrNotFound) {
		http.Error(w, "Lançamento não encontrado", http.StatusNotFound)
		return
	}
	serverError(w, msg, err)
}

// newCategoryOption é o valor do <select> que indica "criar categoria nova" (nome em new_category).
const newCategoryOption = "__new__"

var errUnknownCategory = errors.New("categoria inexistente")

// resolveCategory confere a categoria do lançamento. Com "__new__", cria a categoria (ou reutiliza a
// existente, ignorando caixa e acentos) e passa a usar o nome canônico. Ocultas continuam válidas.
func (h *Handler) resolveCategory(db *database.DB, form url.Values, t *models.Transaction) error {
	if t.Category == newCategoryOption {
		c, err := db.GetOrCreateCategory(form.Get("new_category"))
		if err != nil {
			return err
		}
		t.Category = c.Name
		return nil
	}

	ok, err := db.CategoryExists(t.Category)
	if err != nil {
		return err
	}
	if !ok {
		return errUnknownCategory
	}
	return nil
}

// categoryFormError traduz erros de categoria no envio do lançamento: entrada inválida vira 400.
func categoryFormError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnknownCategory) || errors.Is(err, database.ErrInvalidCategoryName) {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	serverError(w, "Erro ao validar categoria", err)
}

func redirectToMonth(w http.ResponseWriter, r *http.Request, month string) {
	target := "/?month=" + url.QueryEscape(month)
	if r.PostForm.Get("previstas") == "1" {
		target += "&previstas=1"
	}
	http.Redirect(w, r, withFilters(target, r), http.StatusSeeOther)
}

// formFilters lê os filtros enviados num formulário (campo "filtros"), já normalizados: só os parâmetros
// conhecidos passam, então o valor nunca injeta nada no redirecionamento.
func formFilters(r *http.Request) filter.Filter {
	v, err := url.ParseQuery(r.PostForm.Get("filtros"))
	if err != nil {
		return filter.Filter{}
	}
	f, _ := filter.Parse(v)
	return f
}

// withFilters acrescenta os filtros do formulário ao endereço de volta, para a lista continuar filtrada.
func withFilters(target string, r *http.Request) string {
	target += bookQuery(r)
	if q := formFilters(r).Query(); q != "" {
		return target + "&" + q
	}
	return target
}

// requestFilter lê os filtros da URL (GET) ou, quando a página é renderizada por um envio de formulário,
// do campo "filtros".
func requestFilter(r *http.Request) (filter.Filter, []string) {
	if r.Method == http.MethodPost {
		return filter.Parse(mustQuery(r.PostForm.Get("filtros")))
	}
	return filter.Parse(r.URL.Query())
}

func mustQuery(raw string) url.Values {
	v, err := url.ParseQuery(raw)
	if err != nil {
		return url.Values{}
	}
	return v
}

const maxHistoryRows = 500 // limite de lançamentos mostrados na lista

// applyFilter separa os lançamentos que passam pelos filtros e resume a lista (contagem e somas);
// a lista devolvida mostra no máximo maxHistoryRows (os mais recentes, pois vêm em ordem decrescente).
func applyFilter(f filter.Filter, list []models.Transaction) (shown []models.Transaction, count int, income, expense int64) {
	for _, t := range list {
		if !f.Match(t.Category, t.Description, t.Type, t.AmountCents) {
			continue
		}
		count++
		if t.Type == "income" {
			income += t.AmountCents
		} else {
			expense += t.AmountCents
		}
		if len(shown) < maxHistoryRows {
			shown = append(shown, t)
		}
	}
	return shown, count, income, expense
}

// filterView monta os dados do formulário de filtros.
func filterView(f filter.Filter, warnings []string, categories []models.Category) models.FilterView {
	selected := map[string]bool{}
	for _, c := range categories {
		if f.Selected(c.Name) {
			selected[c.Name] = true
		}
	}
	return models.FilterView{
		Text: f.Text, Type: f.TypeParam(), Min: f.MinText(), Max: f.MaxText(), AllMonths: f.AllMonths,
		Active: f.Active(), Query: f.Query(), Warning: strings.Join(warnings, " "),
		Categories: f.Categories, Selected: selected,
	}
}

// Dashboard renderiza a tela principal com o resumo e histórico de transações.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	h.renderPage(w, r, pageOpts{Status: http.StatusOK, ShowCategories: q.Get("categorias") == "1", ShowBooks: q.Get("livros") == "1"})
}

// requestMonth lê o mês (AAAA-MM) da URL ou, em envios de formulário, do corpo.
func requestMonth(r *http.Request) string {
	if m := r.URL.Query().Get("month"); m != "" {
		return m
	}
	return r.PostForm.Get("month")
}

// wantsProjected diz se os totais devem somar as parcelas previstas (?previstas=1, na URL ou no formulário).
func wantsProjected(r *http.Request) bool {
	return r.URL.Query().Get("previstas") == "1" || r.PostForm.Get("previstas") == "1"
}

// visibleCategories filtra as categorias que aparecem nas listas de escolha ao lançar.
func visibleCategories(all []models.Category) []models.Category {
	var out []models.Category
	for _, c := range all {
		if !c.Hidden {
			out = append(out, c)
		}
	}
	return out
}

// pageOpts reúne o que muda numa renderização da tela principal além do pedido.
type pageOpts struct {
	Status         int
	CategoryError  string
	ShowCategories bool
	BookError      string
	ShowBooks      bool
}

// renderDashboard monta e renderiza a página. Em erros do menu de categorias, devolve o status
// correspondente com a mensagem dentro do pop-up (aberto).
func (h *Handler) renderDashboard(w http.ResponseWriter, r *http.Request, status int, categoryError string, showCategories bool) {
	h.renderPage(w, r, pageOpts{Status: status, CategoryError: categoryError, ShowCategories: showCategories})
}

// renderPage monta e renderiza a tela principal; erros dos pop-ups (categorias, livros) vêm em o, com o pop-up aberto.
func (h *Handler) renderPage(w http.ResponseWriter, r *http.Request, o pageOpts) {
	now := time.Now()
	selectedMonth := requestMonth(r)
	if selectedMonth == "" {
		selectedMonth = now.Format("2006-01")
	}

	db, book, books, _, err := h.bookScope(r)
	if err != nil {
		serverError(w, "Erro ao carregar os livros", err)
		return
	}

	summary, err := db.GetSummaryByMonth(selectedMonth)
	if err != nil {
		serverError(w, "Erro ao carregar resumo financeiro", err)
		return
	}

	transactions, err := db.GetTransactions(selectedMonth)
	if err != nil {
		serverError(w, "Erro ao carregar lançamentos", err)
		return
	}

	includeProjected := wantsProjected(r)

	candidates, err := db.GetInstallmentCandidates()
	if err != nil {
		serverError(w, "Erro ao carregar parcelas", err)
		return
	}
	projected := installments.Project(candidates, selectedMonth)
	projectedIncome, projectedExpense := projectedTotals(projected)
	if includeProjected {
		summary = withProjected(summary, projectedIncome, projectedExpense)
	}
	installments.Annotate(transactions)

	categories, err := db.ListCategories()
	if err != nil {
		serverError(w, "Erro ao carregar categorias", err)
		return
	}
	dashboard.Annotate(transactions, categories)
	stats := dashboard.Compute(transactions, categories)

	// A lista do histórico segue os filtros; os cartões e o dashboard do mês continuam sendo do mês inteiro.
	f, filterWarnings := requestFilter(r)
	list := transactions
	if f.AllMonths {
		all, err := db.GetTransactionsBetween("0001-01-01", "9999-12-31")
		if err != nil {
			serverError(w, "Erro ao carregar lançamentos", err)
			return
		}
		for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 { // do mais recente para o mais antigo
			all[i], all[j] = all[j], all[i]
		}
		installments.Annotate(all)
		dashboard.Annotate(all, categories)
		list = all
	}
	shown, count, histIncome, histExpense := applyFilter(f, list)

	var importResult *models.ImportResult
	if v := r.URL.Query().Get("importados"); v != "" {
		imported, err1 := strconv.Atoi(v)
		skipped, _ := strconv.Atoi(r.URL.Query().Get("ignorados"))
		if err1 == nil && imported >= 0 && imported <= statement.MaxRows && skipped >= 0 && skipped <= statement.MaxRows {
			importResult = &models.ImportResult{Imported: imported, Skipped: skipped}
		}
	}

	var bulkResult *models.BulkResult
	query := r.URL.Query()
	for _, k := range []struct{ param, skip, kind string }{
		{"editados", "ignorados", "edit"}, {"excluidos", "ignorados", "delete"}, {"desfeitos", "pulados", "undo"},
	} {
		v := query.Get(k.param)
		if v == "" {
			continue
		}
		n, err1 := strconv.Atoi(v)
		sk, _ := strconv.Atoi(query.Get(k.skip))
		if err1 == nil && n >= 0 && n <= maxBulkIDs && sk >= 0 && sk <= maxBulkIDs {
			bulkResult = &models.BulkResult{Kind: k.kind, Count: n, Skipped: sk}
		}
		break
	}

	var undo *models.UndoView
	if b, err := db.LatestUndoableBatch(); err != nil {
		serverError(w, "Erro ao consultar a última edição em lote", err)
		return
	} else if b != nil {
		undo = &models.UndoView{Action: b.Action, Summary: b.Summary, Count: b.Count, At: b.CreatedAt.Local().Format("02/01/2006 15:04")}
	}

	data := models.PageData{
		SelectedMonth: selectedMonth,
		DisplayMonth:  formatDisplayMonth(selectedMonth),
		Summary:       summary,
		Transactions:  transactions,
		TodayDate:     now.Format("2006-01-02"),
		Version:       h.version,
		Stats:         stats,
		ImportResult:  importResult,
		BulkResult:    bulkResult,
		Undo:          undo,

		History:          shown,
		HistoryCount:     count,
		HistoryIncome:    histIncome,
		HistoryExpense:   histExpense,
		HistoryTruncated: count > len(shown),
		Filter:           filterView(f, filterWarnings, categories),

		Projected:       projected,
		ProjectedIncome:  projectedIncome,
		ProjectedExpense: projectedExpense,
		IncludeProjected: includeProjected,

		Categories:     visibleCategories(categories),
		AllCategories:  categories,
		ShowCategories: o.ShowCategories,
		CategoryError:  o.CategoryError,

		Book:       book,
		BookID:     book.ID,
		Books:      activeBooks(books),
		AllBooks:   books,
		BookPath:   "/",
		BookError:  o.BookError,
		ShowBooks:  o.ShowBooks,
		BookColors: models.BookColors,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(o.Status)
	if err := h.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		serverError(w, "Erro ao renderizar página", err)
	}
}

// CreateTransaction processa o envio do formulário de novo lançamento.
func (h *Handler) CreateTransaction(w http.ResponseWriter, r *http.Request) {
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

	t, err := parseTransactionForm(r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.resolveCategory(db, r.PostForm, &t); err != nil {
		categoryFormError(w, err)
		return
	}

	if err := db.CreateTransaction(&t); err != nil {
		serverError(w, "Erro ao gravar transação", err)
		return
	}
	h.events.Publish()

	// Volta para o mês do lançamento (data já validada como AAAA-MM-DD).
	redirectToMonth(w, r, t.Date[:7])
}

// UpdateTransaction processa a edição de um lançamento existente.
func (h *Handler) UpdateTransaction(w http.ResponseWriter, r *http.Request) {
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

	id, err := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("id")), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	t, err := parseTransactionForm(r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	t.ID = id

	if err := h.resolveCategory(db, r.PostForm, &t); err != nil {
		categoryFormError(w, err)
		return
	}

	if err := db.UpdateTransaction(&t); err != nil {
		dbError(w, "Erro ao atualizar transação", err)
		return
	}
	h.events.Publish()

	redirectToMonth(w, r, t.Date[:7])
}

// DeleteTransaction remove um lançamento existente pelo ID.
func (h *Handler) DeleteTransaction(w http.ResponseWriter, r *http.Request) {
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

	id, err := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("id")), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	if err := db.DeleteTransaction(id); err != nil {
		dbError(w, "Erro ao excluir transação", err)
		return
	}
	h.events.Publish()

	month := r.PostForm.Get("month")
	if month == "" {
		month = time.Now().Format("2006-01")
	}
	redirectToMonth(w, r, month)
}

// projectedTotals soma, em centavos, as receitas e as despesas previstas.
func projectedTotals(p []models.ProjectedInstallment) (income, expense int64) {
	for _, item := range p {
		if item.Type == "income" {
			income += item.AmountCents
		} else {
			expense += item.AmountCents
		}
	}
	return income, expense
}

// withProjected devolve uma cópia do resumo somando as receitas e despesas previstas.
func withProjected(s models.Summary, income, expense int64) models.Summary {
	s.TotalIncome += income
	s.TotalExpense += expense
	s.Balance = s.TotalIncome - s.TotalExpense
	return s
}

// Helpers de formatação

// formatCurrencyBRL formata centavos como moeda brasileira (ex: 123456 -> "R$ 1.234,56").
func formatCurrencyBRL(cents int64) string {
	neg := cents < 0
	if neg {
		cents = -cents
	}

	s := strconv.FormatInt(cents/100, 10)
	var formattedInt strings.Builder
	n := len(s)
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			formattedInt.WriteRune('.')
		}
		formattedInt.WriteRune(c)
	}

	res := fmt.Sprintf("R$ %s,%02d", formattedInt.String(), cents%100)
	if neg {
		res = "-" + res
	}
	return res
}

// formatCentsInput formata centavos como número para campos de formulário (ex: 123456 -> "1234.56").
func formatCentsInput(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func formatDateBR(dateStr string) string {
	parts := strings.Split(dateStr, "-")
	if len(parts) == 3 {
		return fmt.Sprintf("%s/%s/%s", parts[2], parts[1], parts[0])
	}
	return dateStr
}

func formatDisplayMonth(yearMonth string) string {
	months := map[string]string{
		"01": "Janeiro", "02": "Fevereiro", "03": "Março",
		"04": "Abril", "05": "Maio", "06": "Junho",
		"07": "Julho", "08": "Agosto", "09": "Setembro",
		"10": "Outubro", "11": "Novembro", "12": "Dezembro",
	}

	parts := strings.Split(yearMonth, "-")
	if len(parts) == 2 {
		if name, ok := months[parts[1]]; ok {
			return fmt.Sprintf("%s de %s", name, parts[0])
		}
	}
	return yearMonth
}
