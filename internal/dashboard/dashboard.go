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
		out[textutil.Fold(c.Name)] = c.Priority
	}
	return out
}

// EffectivePriority devolve a classificação do lançamento (se houver) ou a da categoria; "" = sem classificação.
func EffectivePriority(t models.Transaction, byKey map[string]string) string {
	if t.Priority != "" {
		return t.Priority
	}
	return byKey[textutil.Fold(t.Category)]
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
	stats := models.MonthStats{
		TotalCount: len(txs),
		Groups: []models.PriorityGroup{
			{Key: "essential", Label: "Essencial"},
			{Key: "superfluous", Label: "Supérfluo"},
			{Key: "unclassified", Label: "Sem classificação"},
		},
	}

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

		idx := 2
		switch EffectivePriority(t, byKey) {
		case models.PriorityEssential:
			idx = 0
		case models.PrioritySuperfluous:
			idx = 1
		}
		stats.Groups[idx].Cents += t.AmountCents
		stats.Groups[idx].Count++
	}

	if stats.ExpenseTotal > 0 {
		for i := range stats.Groups {
			stats.Groups[i].Percent = math.Round(float64(stats.Groups[i].Cents)*1000/float64(stats.ExpenseTotal)) / 10
		}
	}
	return stats
}
