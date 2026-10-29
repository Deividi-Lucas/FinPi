// Package report calcula o relatório por período: fatias do fluxo de caixa, gastos recorrentes e a
// geometria do gráfico. São funções puras: não tocam o banco.
package report

import (
	"fmt"
	"sort"
	"time"

	"finpi/internal/models"
)

type Granularity string

const (
	Day   Granularity = "dia"
	Week  Granularity = "semana"
	Month Granularity = "mes"
)

const (
	autoDayMaxDays  = 62  // até aqui, o agrupamento automático é por dia
	autoWeekMaxDays = 120 // até aqui, por semana; acima, por mês
	allowedDayMax   = 93  // limite do botão Dia
	allowedWeekMax  = 731 // limite do botão Semana
	dateLayout      = "2006-01-02"
)

var monthAbbr = [...]string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}

// Days devolve o número de dias do período (inclusive); 0 se as datas forem inválidas.
func Days(from, to string) int {
	f, err1 := time.Parse(dateLayout, from)
	t, err2 := time.Parse(dateLayout, to)
	if err1 != nil || err2 != nil || t.Before(f) {
		return 0
	}
	return int(t.Sub(f).Hours()/24) + 1
}

// AutoGranularity escolhe a fatia pelo tamanho do período.
func AutoGranularity(from, to string) Granularity {
	switch d := Days(from, to); {
	case d == 0:
		return Month
	case d <= autoDayMaxDays:
		return Day
	case d <= autoWeekMaxDays:
		return Week
	}
	return Month
}

// Allowed diz se a fatia escolhida manualmente cabe no período (mês sempre cabe).
func Allowed(g Granularity, from, to string) bool {
	d := Days(from, to)
	switch g {
	case Day:
		return d > 0 && d <= allowedDayMax
	case Week:
		return d > 0 && d <= allowedWeekMax
	case Month:
		return true
	}
	return false
}

func ParseGranularity(s string) (Granularity, bool) {
	switch g := Granularity(s); g {
	case Day, Week, Month:
		return g, true
	}
	return "", false
}

// Bucket é uma fatia do fluxo de caixa. Income/Expense são realizados; Proj* são parcelas previstas.
type Bucket struct {
	Label, Start, End string
	Income, Expense   int64
	ProjIncome        int64
	ProjExpense       int64
	Net               int64 // entradas - saídas (realizado + previsto)
	Balance           int64 // saldo acumulado desde o início do período (começa em zero)
}

func (b Bucket) TotalIncome() int64  { return b.Income + b.ProjIncome }
func (b Bucket) TotalExpense() int64 { return b.Expense + b.ProjExpense }
func (b Bucket) HasProjection() bool { return b.ProjIncome != 0 || b.ProjExpense != 0 }

func label(g Granularity, start, end time.Time) string {
	switch g {
	case Day:
		return start.Format("02/01")
	case Week:
		return start.Format("02/01") + "–" + end.Format("02/01")
	}
	return fmt.Sprintf("%s/%02d", monthAbbr[start.Month()-1], start.Year()%100)
}

// endOfBucket devolve o último dia da fatia que começa em start (sem passar de limit).
func endOfBucket(g Granularity, start, limit time.Time) time.Time {
	var end time.Time
	switch g {
	case Day:
		end = start
	case Week:
		end = start.AddDate(0, 0, (7-int(start.Weekday()))%7) // até o domingo
	default:
		end = time.Date(start.Year(), start.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	}
	if end.After(limit) {
		return limit
	}
	return end
}

// BuildFlow divide o período em fatias contíguas (as vazias incluídas), soma os lançamentos e as parcelas
// previstas de cada uma e calcula o saldo acumulado.
func BuildFlow(txs []models.Transaction, proj []models.ProjectedInstallment, from, to string, g Granularity) []Bucket {
	start, err1 := time.Parse(dateLayout, from)
	limit, err2 := time.Parse(dateLayout, to)
	if err1 != nil || err2 != nil || limit.Before(start) {
		return nil
	}

	var buckets []Bucket
	for cur := start; !cur.After(limit); {
		end := endOfBucket(g, cur, limit)
		buckets = append(buckets, Bucket{
			Label: label(g, cur, end), Start: cur.Format(dateLayout), End: end.Format(dateLayout),
		})
		cur = end.AddDate(0, 0, 1)
	}

	find := func(date string) int {
		i := sort.Search(len(buckets), func(i int) bool { return buckets[i].Start > date }) - 1
		if i < 0 || date > buckets[i].End {
			return -1
		}
		return i
	}
	for _, t := range txs {
		if i := find(t.Date); i >= 0 {
			if t.Type == "income" {
				buckets[i].Income += t.AmountCents
			} else {
				buckets[i].Expense += t.AmountCents
			}
		}
	}
	for _, p := range proj {
		if i := find(p.Date); i >= 0 {
			if p.Type == "income" {
				buckets[i].ProjIncome += p.AmountCents
			} else {
				buckets[i].ProjExpense += p.AmountCents
			}
		}
	}

	var balance int64
	for i := range buckets {
		buckets[i].Net = buckets[i].TotalIncome() - buckets[i].TotalExpense()
		balance += buckets[i].Net
		buckets[i].Balance = balance
	}
	return buckets
}
