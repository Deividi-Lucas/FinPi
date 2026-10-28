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

	"finpi/internal/database"
	"finpi/internal/models"
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

func redirectToMonth(w http.ResponseWriter, r *http.Request, month string) {
	http.Redirect(w, r, "/?month="+url.QueryEscape(month), http.StatusSeeOther)
}

// Dashboard renderiza a tela principal com o resumo e histórico de transações.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	now := time.Now()
	selectedMonth := r.URL.Query().Get("month")
	if selectedMonth == "" {
		selectedMonth = now.Format("2006-01")
	}

	summary, err := h.db.GetSummaryByMonth(selectedMonth)
	if err != nil {
		serverError(w, "Erro ao carregar resumo financeiro", err)
		return
	}

	transactions, err := h.db.GetTransactions(selectedMonth)
	if err != nil {
		serverError(w, "Erro ao carregar lançamentos", err)
		return
	}

	data := models.PageData{
		SelectedMonth: selectedMonth,
		DisplayMonth:  formatDisplayMonth(selectedMonth),
		Summary:       summary,
		Transactions:  transactions,
		TodayDate:     now.Format("2006-01-02"),
		Version:       h.version,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
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

	t, err := parseTransactionForm(r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := h.db.CreateTransaction(&t); err != nil {
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

	if err := h.db.UpdateTransaction(&t); err != nil {
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

	id, err := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get("id")), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteTransaction(id); err != nil {
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
