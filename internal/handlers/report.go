package handlers

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/report"
)

const (
	reportDateLayout = "2006-01-02"
	reportMaxYears   = 5
	reportMinYear    = 2000
	reportMaxYear    = 2100
)

// reportQuery é o estado normalizado da URL do relatório.
type reportQuery struct {
	Year      int // usado quando !Custom
	From, To  string
	Custom    bool
	Group     string // "dia", "semana", "mes" ou "" (automático)
	Projected bool
}

// URL monta o link do relatório com este estado, depois de aplicar mod (se houver).
func (q reportQuery) URL(mod func(*reportQuery)) string {
	if mod != nil {
		mod(&q)
	}
	v := url.Values{}
	if q.Custom {
		v.Set("de", q.From)
		v.Set("ate", q.To)
	} else {
		v.Set("ano", strconv.Itoa(q.Year))
	}
	if q.Group != "" {
		v.Set("agrupar", q.Group)
	}
	if q.Projected {
		v.Set("previstas", "1")
	}
	return "/relatorio?" + v.Encode()
}

func yearBounds(year int) (string, string) {
	return fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year)
}

// parseReportQuery lê os parâmetros. Entrada inválida devolve o ano atual e uma mensagem de aviso.
func parseReportQuery(v url.Values, now time.Time) (reportQuery, string) {
	q := reportQuery{Year: now.Year(), Projected: v.Get("previstas") == "1"}
	if g, ok := report.ParseGranularity(v.Get("agrupar")); ok {
		q.Group = string(g)
	}
	q.From, q.To = yearBounds(q.Year)

	de, ate := strings.TrimSpace(v.Get("de")), strings.TrimSpace(v.Get("ate"))
	switch {
	case de != "" || ate != "":
		if de == "" || ate == "" {
			return q, "Período inválido: informe as duas datas (de e até)."
		}
		from, err1 := time.Parse(reportDateLayout, de)
		to, err2 := time.Parse(reportDateLayout, ate)
		switch {
		case err1 != nil || err2 != nil:
			return q, "Período inválido: use datas no formato AAAA-MM-DD."
		case to.Before(from):
			return q, "Período inválido: a data inicial vem depois da final."
		case from.Year() < reportMinYear || to.Year() > reportMaxYear:
			return q, fmt.Sprintf("Período inválido: use datas entre %d e %d.", reportMinYear, reportMaxYear)
		case to.After(from.AddDate(reportMaxYears, 0, -1)):
			return q, fmt.Sprintf("Período inválido: no máximo %d anos.", reportMaxYears)
		}
		q.Custom, q.From, q.To = true, from.Format(reportDateLayout), to.Format(reportDateLayout)
	case v.Get("ano") != "":
		y, err := strconv.Atoi(strings.TrimSpace(v.Get("ano")))
		if err != nil || y < reportMinYear || y > reportMaxYear {
			return q, fmt.Sprintf("Período inválido: o ano deve estar entre %d e %d.", reportMinYear, reportMaxYear)
		}
		q.Year = y
		q.From, q.To = yearBounds(y)
	}
	return q, ""
}

// monthsBetween devolve os meses (AAAA-MM) que o período toca, do primeiro ao último.
func monthsBetween(from, to string) []string {
	f, err1 := time.Parse(reportDateLayout, from)
	t, err2 := time.Parse(reportDateLayout, to)
	if err1 != nil || err2 != nil {
		return nil
	}
	var out []string
	for cur := time.Date(f.Year(), f.Month(), 1, 0, 0, 0, 0, time.UTC); !cur.After(t); cur = cur.AddDate(0, 1, 0) {
		out = append(out, cur.Format("2006-01"))
	}
	return out
}

type reportCards struct {
	Income, Expense, Net int64
	NetPercent           float64
	HasNetPercent        bool
	AvgExpense           int64
	HasAvg               bool
	WithProjected        bool
}

func buildCards(buckets []report.Bucket, months int, withProjected bool) reportCards {
	var c reportCards
	for _, b := range buckets {
		c.Income += b.TotalIncome()
		c.Expense += b.TotalExpense()
	}
	c.Net = c.Income - c.Expense
	if c.Income > 0 {
		c.NetPercent, c.HasNetPercent = math.Round(float64(c.Net)*1000/float64(c.Income))/10, true
	}
	if months > 1 {
		c.AvgExpense, c.HasAvg = (c.Expense*2+int64(months))/(2*int64(months)), true
	}
	c.WithProjected = withProjected
	return c
}

type granOption struct {
	Label    string
	URL      string
	Active   bool
	Disabled bool
}

type reportPage struct {
	Version  string
	Warning  string
	Custom   bool
	Year     int
	PrevURL  string
	NextURL  string
	YearURL  string // volta ao ano completo (quando o período é personalizado)
	From, To string

	IncludeProjected bool
	ToggleProjected  string // link que liga/desliga as previstas
	Group            string // "dia", "semana" ou "mes" em uso
	GroupExplicit    bool
	AutoURL          string
	Options          []granOption

	Cards      reportCards
	Empty      bool
	Buckets    []report.Bucket
	Chart      report.Chart
	Recurrence report.Recurrence
	ChartLabel string
}

// Report renderiza a página do relatório por período (somente leitura).
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	q, warning := parseReportQuery(r.URL.Query(), time.Now())

	txs, err := h.db.GetTransactionsBetween(q.From, q.To)
	if err != nil {
		serverError(w, "Erro ao carregar lançamentos do período", err)
		return
	}

	months := monthsBetween(q.From, q.To)
	var projected []models.ProjectedInstallment
	if q.Projected {
		candidates, err := h.db.GetInstallmentCandidates()
		if err != nil {
			serverError(w, "Erro ao carregar parcelas", err)
			return
		}
		for _, m := range months {
			for _, p := range installments.Project(candidates, m) {
				if p.Date >= q.From && p.Date <= q.To {
					projected = append(projected, p)
				}
			}
		}
	}

	granularity := report.AutoGranularity(q.From, q.To)
	explicit := false
	if g, ok := report.ParseGranularity(q.Group); ok && report.Allowed(g, q.From, q.To) {
		granularity, explicit = g, true
	}

	buckets := report.BuildFlow(txs, projected, q.From, q.To, granularity)
	page := reportPage{
		Version: h.version, Warning: warning, Custom: q.Custom, Year: q.Year, From: q.From, To: q.To,
		YearURL:          q.URL(func(x *reportQuery) { x.Custom = false }), // em modo personalizado q.Year já é o ano atual
		IncludeProjected: q.Projected,
		ToggleProjected:  q.URL(func(x *reportQuery) { x.Projected = !x.Projected }),
		Group:            string(granularity), GroupExplicit: explicit,
		AutoURL:    q.URL(func(x *reportQuery) { x.Group = "" }),
		Empty:      len(txs) == 0 && len(projected) == 0,
		Buckets:    buckets,
		Cards:      buildCards(buckets, len(months), q.Projected && len(projected) > 0),
		Recurrence: report.Recurring(txs),
		ChartLabel: "Entradas, saídas e saldo acumulado por " + map[report.Granularity]string{report.Day: "dia", report.Week: "semana", report.Month: "mês"}[granularity],
	}
	for _, o := range []struct {
		g     report.Granularity
		label string
	}{{report.Day, "Dia"}, {report.Week, "Semana"}, {report.Month, "Mês"}} {
		page.Options = append(page.Options, granOption{
			Label:    o.label,
			URL:      q.URL(func(x *reportQuery) { x.Group = string(o.g) }),
			Active:   o.g == granularity,
			Disabled: !report.Allowed(o.g, q.From, q.To),
		})
	}
	// Nos limites da faixa aceita a seta correspondente fica desativada (URL vazia).
	if q.Year > reportMinYear {
		page.PrevURL = q.URL(func(x *reportQuery) { x.Custom, x.Year = false, q.Year-1 })
	}
	if q.Year < reportMaxYear {
		page.NextURL = q.URL(func(x *reportQuery) { x.Custom, x.Year = false, q.Year+1 })
	}
	if !page.Empty {
		page.Chart = report.BuildChart(buckets, formatCurrencyBRL)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "report.html", page); err != nil {
		serverError(w, "Erro ao renderizar relatório", err)
	}
}
