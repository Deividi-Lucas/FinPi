// Package dashboard calcula os números do dashboard do mês a partir dos lançamentos já carregados.
// São funções puras: não tocam o banco.
package dashboard

import (
	"math"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

// CategoryPriorities indexa a classificação de cada categoria pelo nome normalizado (textutil.Fold),
// o que cobre variações antigas como "saude" para "Saúde".
func CategoryPriorities(cats []models.Category) map[string]string {
	out := make(map[string]string, len(cats))
	for _, c := range cats {
		out[textutil.FoldCached(c.Name)] = c.Priority
	}
	return out
}

// EffectivePriority devolve a classificação do lançamento (se houver) ou a da categoria; "" = sem classificação.
func EffectivePriority(t models.Transaction, byKey map[string]string) string {
	if t.Priority != "" {
		return t.Priority
	}
	return byKey[textutil.FoldCached(t.Category)]
}

// Annotate preenche EffectivePriority das despesas. Receitas ficam sem classificação.
func Annotate(txs []models.Transaction, cats []models.Category) {
	byKey := CategoryPriorities(cats)
	for i := range txs {
		if txs[i].Type == "expense" {
			txs[i].EffectivePriority = EffectivePriority(txs[i], byKey)
		}
	}
}

// moreRecent diz se a é mais recente que b (data e, na igualdade, maior ID).
func moreRecent(a, b models.Transaction) bool {
	if a.Date != b.Date {
		return a.Date > b.Date
	}
	return a.ID > b.ID
}

// Compute resume o mês: contagens, maior e menor despesa e a divisão por classificação.
// Só lançamentos entram (parcelas previstas não). Empate de valor fica com o mais recente.
func Compute(txs []models.Transaction, cats []models.Category) models.MonthStats {
	byKey := CategoryPriorities(cats)
	stats := newStats()
	stats.TotalCount = len(txs)

	for i := range txs {
		t := txs[i]
		if t.Type != "expense" {
			stats.IncomeCount++
			continue
		}
		stats.ExpenseCount++
		stats.ExpenseTotal += t.AmountCents

		if stats.Largest == nil || t.AmountCents > stats.Largest.AmountCents ||
			(t.AmountCents == stats.Largest.AmountCents && moreRecent(t, *stats.Largest)) {
			c := t
			stats.Largest = &c
		}
		if stats.Smallest == nil || t.AmountCents < stats.Smallest.AmountCents ||
			(t.AmountCents == stats.Smallest.AmountCents && moreRecent(t, *stats.Smallest)) {
			c := t
			stats.Smallest = &c
		}

		idx := priorityIndex(EffectivePriority(t, byKey))
		stats.Groups[idx].Cents += t.AmountCents
		stats.Groups[idx].Count++
	}

	fillPercents(&stats)
	return stats
}

// newStats devolve o resumo vazio com os três grupos de classificação, na ordem exibida.
func newStats() models.MonthStats {
	return models.MonthStats{Groups: []models.PriorityGroup{
		{Key: "essential", Label: "Essencial"},
		{Key: "superfluous", Label: "Supérfluo"},
		{Key: "unclassified", Label: "Sem classificação"},
	}}
}

// priorityIndex é a posição do grupo da classificação em MonthStats.Groups.
func priorityIndex(p string) int {
	switch p {
	case models.PriorityEssential:
		return 0
	case models.PrioritySuperfluous:
		return 1
	}
	return 2
}

// fillPercents calcula a fatia de cada grupo nas despesas, com uma casa decimal.
func fillPercents(stats *models.MonthStats) {
	if stats.ExpenseTotal > 0 {
		for i := range stats.Groups {
			stats.Groups[i].Percent = math.Round(float64(stats.Groups[i].Cents)*1000/float64(stats.ExpenseTotal)) / 10
		}
	}
}

// Outside são as somas (em centavos) do que fica fora dos totais: Movimentação interna e Investimento.
type Outside struct {
	InternalIn, InternalOut int64 // Movimentação interna
	Invested, Redeemed      int64 // Investimento: saídas (aplicado) e entradas (resgatado)
}

// SplitOutside separa os lançamentos das categorias Movimentação interna e Investimento: devolve os demais
// e as somas do que ficou de fora.
func SplitOutside(txs []models.Transaction) (regular []models.Transaction, o Outside) {
	if len(txs) > 0 {
		regular = make([]models.Transaction, 0, len(txs)) // uma alocação só: a lista pode ter dezenas de milhares
	}
	return splitOutside(txs, regular)
}

// SplitOutsideInPlace é o SplitOutside que reaproveita a lista recebida para os demais (sem copiar o período
// inteiro). Só serve quando quem chama não usa mais a lista original.
func SplitOutsideInPlace(txs []models.Transaction) (regular []models.Transaction, o Outside) {
	return splitOutside(txs, txs[:0])
}

func splitOutside(txs, regular []models.Transaction) ([]models.Transaction, Outside) {
	var o Outside
	for _, t := range txs {
		income := t.Type == "income"
		switch {
		case models.IsInternal(t.Category):
			if income {
				o.InternalIn += t.AmountCents
			} else {
				o.InternalOut += t.AmountCents
			}
		case models.IsInvestment(t.Category):
			if income {
				o.Redeemed += t.AmountCents
			} else {
				o.Invested += t.AmountCents
			}
		default:
			regular = append(regular, t)
		}
	}
	return regular, o
}

// Summarize soma receitas, despesas e saldo (em centavos) dos lançamentos.
func Summarize(txs []models.Transaction) models.Summary {
	var s models.Summary
	for _, t := range txs {
		if t.Type == "income" {
			s.TotalIncome += t.AmountCents
		} else {
			s.TotalExpense += t.AmountCents
		}
	}
	s.Balance = s.TotalIncome - s.TotalExpense
	return s
}

// FromGroups dá o mesmo resultado de SplitOutside + Summarize + Compute a partir das somas do mês por categoria,
// tipo e classificação (database.MonthGroups), sem carregar cada lançamento. Largest e Smallest ficam nil: quem
// chama busca as duas despesas à parte, excluindo as categorias de OutsideCategories.
func FromGroups(groups []models.MonthGroup, cats []models.Category) (models.Summary, models.MonthStats, Outside) {
	byKey := CategoryPriorities(cats)
	var s models.Summary
	var o Outside
	stats := newStats()
	for _, g := range groups {
		income := g.Type == "income"
		switch {
		case models.IsInternal(g.Category):
			if income {
				o.InternalIn += g.Cents
			} else {
				o.InternalOut += g.Cents
			}
			continue
		case models.IsInvestment(g.Category):
			if income {
				o.Redeemed += g.Cents
			} else {
				o.Invested += g.Cents
			}
			continue
		}
		if income {
			s.TotalIncome += g.Cents
		} else {
			s.TotalExpense += g.Cents
		}
		stats.TotalCount += g.Count
		if g.Type != "expense" {
			stats.IncomeCount += g.Count
			continue
		}
		stats.ExpenseCount += g.Count
		stats.ExpenseTotal += g.Cents
		idx := priorityIndex(EffectivePriority(models.Transaction{Priority: g.Priority, Category: g.Category}, byKey))
		stats.Groups[idx].Cents += g.Cents
		stats.Groups[idx].Count += g.Count
	}
	s.Balance = s.TotalIncome - s.TotalExpense
	fillPercents(&stats)
	return s, stats, o
}

// OutsideCategories lista, sem repetir, os textos de categoria dos grupos que ficam fora dos totais.
func OutsideCategories(groups []models.MonthGroup) []string {
	var out []string
	seen := map[string]bool{}
	for _, g := range groups {
		if models.IsOutsideTotals(g.Category) && !seen[g.Category] {
			seen[g.Category] = true
			out = append(out, g.Category)
		}
	}
	return out
}
