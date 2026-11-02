package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/database"
	"finpi/internal/models"
	"finpi/internal/transfer"
)

const maxTransferTxs = 20000 // lançamentos lidos por requisição para sugerir pares

// timeNow existe para os testes poderem fixar a data.
var timeNow = time.Now

// overviewPeriod é o período escolhido: um mês (AAAA-MM) ou um ano inteiro.
type overviewPeriod struct {
	Year     bool   // true = ano inteiro
	Month    string // AAAA-MM (modo mês)
	YearNum  int    // AAAA (modo ano)
	From, To string
	Label    string
	PrevURL  string
	NextURL  string
	MonthURL string // alterna para o mês
	YearURL  string // alterna para o ano
}

func parseOverviewPeriod(v url.Values, now time.Time) overviewPeriod {
	var p overviewPeriod
	if y, err := strconv.Atoi(strings.TrimSpace(v.Get("ano"))); err == nil && y >= reportMinYear && y <= reportMaxYear {
		p.Year, p.YearNum = true, y
		p.From, p.To = fmt.Sprintf("%04d-01-01", y), fmt.Sprintf("%04d-12-31", y)
		p.Label = strconv.Itoa(y)
		p.PrevURL = fmt.Sprintf("/visao-geral?ano=%d", y-1)
		p.NextURL = fmt.Sprintf("/visao-geral?ano=%d", y+1)
		p.MonthURL = "/visao-geral?month=" + now.Format("2006-01")
		p.YearURL = fmt.Sprintf("/visao-geral?ano=%d", y)
		return p
	}
	month := strings.TrimSpace(v.Get("month"))
	first, err := time.Parse("2006-01", month)
	if err != nil {
		first, _ = time.Parse("2006-01", now.Format("2006-01"))
		month = first.Format("2006-01")
	}
	last := first.AddDate(0, 1, -1)
	p.Month = month
	p.From, p.To = first.Format("2006-01-02"), last.Format("2006-01-02")
	p.Label = formatDisplayMonth(month)
	p.PrevURL = "/visao-geral?month=" + first.AddDate(0, -1, 0).Format("2006-01")
	p.NextURL = "/visao-geral?month=" + first.AddDate(0, 1, 0).Format("2006-01")
	p.MonthURL = "/visao-geral?month=" + month
	p.YearURL = fmt.Sprintf("/visao-geral?ano=%d", first.Year())
	return p
}

// stateQuery devolve o período como parâmetros de URL ("month=…" ou "ano=…") para os formulários e links da revisão.
func (p overviewPeriod) stateQuery() string {
	if p.Year {
		return "ano=" + strconv.Itoa(p.YearNum)
	}
	return "month=" + p.Month
}

// overviewPage são os dados do template overview.html.
type overviewPage struct {
	Version string
	Period  overviewPeriod
	State   string // period.stateQuery()
	Error   string

	Overview  transfer.Overview
	Pending   int  // pares sugeridos
	Truncated bool // havia mais lançamentos do que o limite lido
	// preenchido pela revisão (Task 5)
	Review reviewData
}

// reviewData é o conteúdo do pop-up de revisão (preenchido na Task 5; vazio na Task 4).
type reviewData struct {
	Open bool
	Tab  string
}

// freeTransactions devolve os lançamentos de livros ativos que ainda não estão ligados (ligação válida) nem marcados.
func freeTransactions(all []models.Transaction, links []database.TransferLink, solo []models.Transaction) []models.Transaction {
	busy := map[int64]bool{}
	for _, l := range links {
		if l.Valid {
			busy[l.Out.ID], busy[l.In.ID] = true, true
		}
	}
	for _, s := range solo {
		busy[s.ID] = true
	}
	var out []models.Transaction
	for _, t := range all {
		if !busy[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

// Overview renderiza a visão geral (somente leitura).
func (h *Handler) Overview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}
	h.renderOverview(w, r, http.StatusOK, "")
}

func (h *Handler) renderOverview(w http.ResponseWriter, r *http.Request, status int, errMsg string) {
	values := r.URL.Query()
	if r.Method == http.MethodPost {
		values = r.PostForm
	}
	period := parseOverviewPeriod(values, timeNow())

	books, err := h.db.ListBooks()
	if err != nil {
		serverError(w, "Erro ao carregar os livros", err)
		return
	}
	links, err := h.db.ListTransferLinks()
	if err != nil {
		serverError(w, "Erro ao carregar as ligações", err)
		return
	}
	solo, err := h.db.ListSolo()
	if err != nil {
		serverError(w, "Erro ao carregar as marcas", err)
		return
	}
	linked, soloSet := map[int64]bool{}, map[int64]bool{}
	for _, l := range links {
		if l.Valid {
			linked[l.Out.ID], linked[l.In.ID] = true, true
		}
	}
	for _, s := range solo {
		soloSet[s.ID] = true
	}

	var inputs []transfer.BookInput
	for _, b := range activeBooks(books) {
		txs, err := h.db.ForBook(b.ID).GetTransactionsBetween(period.From, period.To)
		if err != nil {
			serverError(w, "Erro ao carregar os lançamentos", err)
			return
		}
		inputs = append(inputs, transfer.BookInput{Book: b, Txs: txs})
	}

	all, truncated, err := h.db.AllTransactions(maxTransferTxs)
	if err != nil {
		serverError(w, "Erro ao carregar os lançamentos", err)
		return
	}
	var free []transfer.Tx
	for _, t := range freeTransactions(all, links, solo) {
		free = append(free, database.ToTransferTx(t))
	}

	page := overviewPage{
		Version: h.version, Period: period, State: period.stateQuery(), Error: errMsg,
		Overview: transfer.BuildOverview(inputs, linked, soloSet),
		Pending:  len(transfer.Suggest(free)), Truncated: truncated,
	}
	h.fillReview(&page, r, values, all, links, solo, free) // Task 5; na Task 4 é um no-op

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.tmpl.ExecuteTemplate(w, "overview.html", page); err != nil {
		serverError(w, "Erro ao renderizar a visão geral", err)
	}
}

// fillReview prepara o pop-up de revisão. Na Task 4 não faz nada.
func (h *Handler) fillReview(page *overviewPage, r *http.Request, values url.Values,
	all []models.Transaction, links []database.TransferLink, solo []models.Transaction, free []transfer.Tx) {
}
