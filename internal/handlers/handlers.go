package handlers

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"finpi/internal/database"
	"finpi/internal/models"
)

type Handler struct {
	db   *database.DB
	tmpl *template.Template
}

// NewHandler compila os templates HTML embutidos uma única vez na inicialização,
// garantindo alta performance e baixo consumo contínuo de memória.
func NewHandler(db *database.DB, templateFS fs.FS) (*Handler, error) {
	funcMap := template.FuncMap{
		"formatCurrency": formatCurrencyBRL,
		"formatDate":     formatDateBR,
	}

	tmpl, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("erro ao compilar templates HTML: %w", err)
	}

	return &Handler{
		db:   db,
		tmpl: tmpl,
	}, nil
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
		http.Error(w, "Erro ao carregar resumo financeiro: "+err.Error(), http.StatusInternalServerError)
		return
	}

	transactions, err := h.db.GetTransactions(selectedMonth)
	if err != nil {
		http.Error(w, "Erro ao carregar lançamentos: "+err.Error(), http.StatusInternalServerError)
		return
	}

	data := models.PageData{
		SelectedMonth: selectedMonth,
		DisplayMonth:  formatDisplayMonth(selectedMonth),
		Summary:       summary,
		Transactions:  transactions,
		TodayDate:     now.Format("2006-01-02"),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "index.html", data); err != nil {
		http.Error(w, "Erro ao renderizar página: "+err.Error(), http.StatusInternalServerError)
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

	desc := strings.TrimSpace(r.FormValue("description"))
	amountStr := strings.TrimSpace(r.FormValue("amount"))
	txType := strings.TrimSpace(r.FormValue("type"))
	category := strings.TrimSpace(r.FormValue("category"))
	date := strings.TrimSpace(r.FormValue("date"))
	redirectMonth := r.FormValue("month")

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil || amount <= 0 {
		http.Error(w, "Valor numérico inválido", http.StatusBadRequest)
		return
	}

	if txType != "income" && txType != "expense" {
		http.Error(w, "Tipo inválido: use 'income' ou 'expense'", http.StatusBadRequest)
		return
	}

	if desc == "" || date == "" {
		http.Error(w, "Descrição e data são obrigatórios", http.StatusBadRequest)
		return
	}

	t := &models.Transaction{
		Description: desc,
		Amount:      amount,
		Type:        txType,
		Category:    category,
		Date:        date,
	}

	if err := h.db.CreateTransaction(t); err != nil {
		http.Error(w, "Erro ao gravar transação: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Redireciona de volta para o mês da transação ou o mês atual selecionado
	targetMonth := redirectMonth
	if len(date) >= 7 {
		targetMonth = date[:7]
	}
	http.Redirect(w, r, "/?month="+targetMonth, http.StatusSeeOther)
}

// DeleteTransaction remove um lançamento existente pelo ID.
func (h *Handler) DeleteTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	idStr := r.FormValue("id")
	month := r.FormValue("month")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "ID inválido", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteTransaction(id); err != nil {
		http.Error(w, "Erro ao excluir transação: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if month == "" {
		month = time.Now().Format("2006-01")
	}
	http.Redirect(w, r, "/?month="+month, http.StatusSeeOther)
}

// Helpers de formatação

func formatCurrencyBRL(val float64) string {
	neg := false
	if val < 0 {
		neg = true
		val = -val
	}

	intPart := int64(val)
	fracPart := int64((val-float64(intPart))*100 + 0.5)
	if fracPart >= 100 {
		intPart++
		fracPart -= 100
	}

	s := fmt.Sprintf("%d", intPart)
	var formattedInt strings.Builder
	n := len(s)
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			formattedInt.WriteRune('.')
		}
		formattedInt.WriteRune(c)
	}

	res := fmt.Sprintf("R$ %s,%02d", formattedInt.String(), fracPart)
	if neg {
		res = "-" + res
	}
	return res
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
