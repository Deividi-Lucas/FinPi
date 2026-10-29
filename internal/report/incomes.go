package report

import (
	"sort"

	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/textutil"
)

// IncomeRow é uma fonte de receita no período.
type IncomeRow struct {
	Name       string
	Count      int
	TotalCents int64
	AvgCents   int64
	Percent    float64 // do total de entradas lançadas no período, com uma casa decimal
	Months     int     // quantos meses do período tiveram entrada desta fonte
	LastDate   string  // AAAA-MM-DD da última entrada no período
}

// IncomeReport resume de onde vem o dinheiro que entrou no período (só receitas lançadas).
type IncomeReport struct {
	Total            int64
	BySource         []IncomeRow // por descrição (sem o número da parcela); todas as fontes, inclusive as de uma só entrada
	ByCategory       []IncomeRow
	Sources          int        // quantidade de fontes (descrições diferentes)
	Largest          *IncomeRow // maior fonte por total; nil sem receitas
	MonthsWithIncome int        // meses do período com alguma entrada
	MonthsTouched    int        // meses que o período toca
	AvgMonthly       int64      // Total ÷ MonthsTouched, arredondada
	HasMonthly       bool       // false em período de um só mês
}

// Incomes agrupa as receitas por descrição e por categoria. months é o número de meses que o período toca.
func Incomes(txs []models.Transaction, months int) IncomeReport {
	var incomes []models.Transaction
	var total int64
	for _, t := range txs {
		if t.Type == "income" {
			incomes = append(incomes, t)
			total += t.AmountCents
		}
	}
	sort.SliceStable(incomes, func(i, j int) bool {
		if incomes[i].Date != incomes[j].Date {
			return incomes[i].Date < incomes[j].Date
		}
		return incomes[i].ID < incomes[j].ID
	})

	sources := map[string]*group{}
	cats := map[string]*group{}
	active := map[string]bool{}
	for _, t := range incomes {
		name := collapse(t.Description)
		if info, ok := installments.Parse(t.Description); ok {
			name = collapse(info.Name)
		}
		addTo(sources, textutil.Fold(name), name, t.AmountCents, t.Date)

		cat := collapse(t.Category)
		addTo(cats, textutil.Fold(cat), cat, t.AmountCents, t.Date)

		if len(t.Date) >= 7 {
			active[t.Date[:7]] = true
		}
	}

	r := IncomeReport{
		Total: total, MonthsWithIncome: len(active), MonthsTouched: months,
		BySource: incomeRows(sources, total), ByCategory: incomeRows(cats, total),
	}
	r.Sources = len(r.BySource)
	if len(r.BySource) > 0 {
		top := r.BySource[0]
		r.Largest = &top
	}
	if months > 1 && total > 0 {
		r.AvgMonthly, r.HasMonthly = avgCents(total, months), true
	}
	return r
}

// incomeRows monta as linhas ordenadas por total (maior primeiro), depois por vezes e por nome.
func incomeRows(m map[string]*group, total int64) []IncomeRow {
	var out []IncomeRow
	for _, g := range m {
		out = append(out, IncomeRow{
			Name: g.name(), Count: g.count, TotalCents: g.total, AvgCents: avgCents(g.total, g.count),
			Percent: percentOf(g.total, total), Months: len(g.months), LastDate: g.last,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TotalCents != out[j].TotalCents {
			return out[i].TotalCents > out[j].TotalCents
		}
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}
