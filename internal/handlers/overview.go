package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/database"
	"finpi/internal/models"
	"finpi/internal/textutil"
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
	State   template.URL // period.stateQuery(); template.URL para o "=" não ser escapado no href
	Error   string

	Overview  transfer.Overview
	Pending   int  // pares sugeridos
	Truncated bool // havia mais lançamentos do que o limite lido
	// preenchido pela revisão (Task 5)
	Review reviewData
}

const maxManualRows = 50 // linhas da aba "Ligar à mão"

// reviewTx é um lançamento mostrado na revisão, com o livro dele.
type reviewTx struct {
	transfer.Tx
	Book models.Book
}

// reviewPair é um par sugerido para a tela.
type reviewPair struct {
	Out, In       reviewTx
	DaysApart     int
	AltOut, AltIn []reviewTx
}

// reviewLink é uma ligação válida, com os dois lados.
type reviewLink struct {
	ID      int64
	Out, In reviewTx
}

// reviewData é o conteúdo do pop-up de revisão das transferências.
type reviewData struct {
	Open          bool
	Tab           string
	Suggestions   []reviewPair
	Linked        []reviewLink
	Solo          []reviewTx
	Manual        []reviewTx
	Q, Valor, Mes string
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
	active := map[int64]bool{}
	for _, b := range activeBooks(books) {
		active[b.ID] = true
	}
	linked, soloSet := map[int64]bool{}, map[int64]bool{}
	for _, l := range links {
		// ligação com um livro arquivado não desconta o lado ativo: ele volta a contar normalmente
		if l.Valid && active[l.Out.BookID] && active[l.In.BookID] {
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
		if models.IsInternal(t.Category) { // já fica fora dos totais
			continue
		}
		free = append(free, database.ToTransferTx(t))
	}
	pairs := transfer.Suggest(free)

	page := overviewPage{
		Version: h.version, Period: period, State: template.URL(period.stateQuery()), Error: errMsg,
		Overview: transfer.BuildOverview(inputs, linked, soloSet),
		Pending:  len(pairs), Truncated: truncated,
	}
	h.fillReview(&page, r, values, links, solo, free, pairs)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.tmpl.ExecuteTemplate(w, "overview.html", page); err != nil {
		serverError(w, "Erro ao renderizar a visão geral", err)
	}
}

// fillReview prepara o pop-up de revisão: abre com revisao=1 ou quando há uma mensagem de erro para mostrar.
func (h *Handler) fillReview(page *overviewPage, r *http.Request, values url.Values,
	links []database.TransferLink, solo []models.Transaction, free []transfer.Tx, pairs []transfer.Pair) {
	if values.Get("revisao") != "1" && page.Error == "" {
		return
	}
	rv := &page.Review
	rv.Open = true
	rv.Tab = values.Get("aba")
	switch rv.Tab {
	case "sugeridos", "ligados", "fora", "manual":
	default:
		rv.Tab = "sugeridos"
	}

	byBook := map[int64]models.Book{}
	if books, err := h.db.ListBooks(); err == nil {
		for _, b := range books {
			byBook[b.ID] = b
		}
	}
	conv := func(t transfer.Tx) reviewTx { return reviewTx{Tx: t, Book: byBook[t.BookID]} }
	convAll := func(list []transfer.Tx) []reviewTx {
		var out []reviewTx
		for _, t := range list {
			out = append(out, conv(t))
		}
		return out
	}

	for _, p := range pairs {
		rv.Suggestions = append(rv.Suggestions, reviewPair{
			Out: conv(p.Out), In: conv(p.In), DaysApart: p.DaysApart,
			AltOut: convAll(p.AltOut), AltIn: convAll(p.AltIn),
		})
	}
	for _, l := range links {
		if l.Valid {
			rv.Linked = append(rv.Linked, reviewLink{ID: l.ID,
				Out: conv(database.ToTransferTx(l.Out)), In: conv(database.ToTransferTx(l.In))})
		}
	}
	for _, s := range solo {
		rv.Solo = append(rv.Solo, conv(database.ToTransferTx(s)))
	}

	// Aba "Ligar à mão": lançamentos livres (já do mais recente para o mais antigo), com os filtros.
	rv.Q = strings.TrimSpace(values.Get("q"))
	rv.Valor = strings.TrimSpace(values.Get("valor"))
	rv.Mes = strings.TrimSpace(values.Get("mes"))
	q := textutil.Fold(rv.Q)
	cents, valorErr := parseLooseCents(rv.Valor)
	useValor := rv.Valor != "" && valorErr == nil
	_, mesErr := time.Parse("2006-01", rv.Mes)
	useMes := rv.Mes != "" && mesErr == nil
	for _, t := range free {
		if q != "" && !strings.Contains(textutil.Fold(t.Description), q) {
			continue
		}
		if useValor && t.AmountCents != cents {
			continue
		}
		if useMes && !strings.HasPrefix(t.Date, rv.Mes+"-") {
			continue
		}
		rv.Manual = append(rv.Manual, conv(t))
		if len(rv.Manual) >= maxManualRows {
			break
		}
	}
}

// parseLooseCents aceita "45,00", "45.00", "1.234,56" e "R$ 45" e devolve centavos.
func parseLooseCents(s string) (int64, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "R$"))
	s = strings.ReplaceAll(s, " ", "")
	if strings.Contains(s, ",") {
		s = strings.ReplaceAll(s, ".", "") // com vírgula, os pontos são de milhar
	} else if strings.Count(s, ".") > 1 {
		s = strings.ReplaceAll(s, ".", "") // "1.234.567" sem decimais
	}
	return parseAmountCents(s)
}

// markTransfers preenche o selo de transferência (TransferNote) das linhas do histórico:
// "↔ livro do outro lado" nas ligações válidas e "fora do FinPi" nos lançamentos marcados.
func (h *Handler) markTransfers(rows []models.Transaction) error {
	if len(rows) == 0 {
		return nil
	}
	links, err := h.db.ListTransferLinks()
	if err != nil {
		return err
	}
	solo, err := h.db.ListSolo()
	if err != nil {
		return err
	}
	if len(links) == 0 && len(solo) == 0 {
		return nil
	}
	books, err := h.db.ListBooks()
	if err != nil {
		return err
	}
	names := make(map[int64]string, len(books))
	for _, b := range books {
		names[b.ID] = b.Name
	}
	notes := make(map[int64]string, 2*len(links)+len(solo))
	for _, l := range links {
		if !l.Valid {
			continue
		}
		notes[l.Out.ID] = "↔ " + names[l.In.BookID]
		notes[l.In.ID] = "↔ " + names[l.Out.BookID]
	}
	for _, s := range solo {
		notes[s.ID] = "fora do FinPi"
	}
	for i := range rows {
		rows[i].TransferNote = notes[rows[i].ID]
	}
	return nil
}
