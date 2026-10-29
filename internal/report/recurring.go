package report

import (
	"math"
	"sort"
	"strings"

	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/textutil"
)

// RecurringRow é um grupo de despesas que se repetiram no período.
type RecurringRow struct {
	Name       string
	Count      int
	TotalCents int64
	AvgCents   int64
	Percent    float64 // do total de despesas lançadas no período, com uma casa decimal
}

type Recurrence struct {
	ByCategory    []RecurringRow
	ByDescription []RecurringRow // só grupos com 2 ou mais lançamentos
	ExpenseTotal  int64
}

type group struct {
	count    int
	total    int64
	spelling map[string]int
	order    []string // grafias na ordem em que apareceram (por data, depois ID)
}

func (g *group) add(spelling string, cents int64) {
	if g.spelling == nil {
		g.spelling = map[string]int{}
	}
	if g.spelling[spelling] == 0 {
		g.order = append(g.order, spelling)
	}
	g.spelling[spelling]++
	g.count++
	g.total += cents
}

// name devolve a grafia mais frequente; em empate, a que apareceu primeiro.
func (g *group) name() string {
	best, bestN := "", 0
	for _, s := range g.order {
		if g.spelling[s] > bestN {
			best, bestN = s, g.spelling[s]
		}
	}
	return best
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Recurring agrupa as despesas por categoria e por descrição (sem o número da parcela).
func Recurring(txs []models.Transaction) Recurrence {
	var expenses []models.Transaction
	var total int64
	for _, t := range txs {
		if t.Type == "expense" {
			expenses = append(expenses, t)
			total += t.AmountCents
		}
	}
	sort.SliceStable(expenses, func(i, j int) bool {
		if expenses[i].Date != expenses[j].Date {
			return expenses[i].Date < expenses[j].Date
		}
		return expenses[i].ID < expenses[j].ID
	})

	cats := map[string]*group{}
	descs := map[string]*group{}
	for _, t := range expenses {
		cat := collapse(t.Category)
		addTo(cats, textutil.Fold(cat), cat, t.AmountCents)

		name := collapse(t.Description)
		if info, ok := installments.Parse(t.Description); ok {
			name = collapse(info.Name)
		}
		addTo(descs, textutil.Fold(name), name, t.AmountCents)
	}

	r := Recurrence{ExpenseTotal: total}
	r.ByCategory = rows(cats, total, 1)
	r.ByDescription = rows(descs, total, 2)
	return r
}

func addTo(m map[string]*group, key, spelling string, cents int64) {
	g := m[key]
	if g == nil {
		g = &group{}
		m[key] = g
	}
	g.add(spelling, cents)
}

func rows(m map[string]*group, total int64, minCount int) []RecurringRow {
	var out []RecurringRow
	for _, g := range m {
		if g.count < minCount {
			continue
		}
		row := RecurringRow{
			Name: g.name(), Count: g.count, TotalCents: g.total,
			AvgCents: (g.total*2 + int64(g.count)) / (2 * int64(g.count)),
		}
		if total > 0 {
			row.Percent = math.Round(float64(g.total)*1000/float64(total)) / 10
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].TotalCents != out[j].TotalCents {
			return out[i].TotalCents > out[j].TotalCents
		}
		return out[i].Name < out[j].Name
	})
	return out
}
