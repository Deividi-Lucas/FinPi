package transfer

import (
	"finpi/internal/dashboard"
	"finpi/internal/models"
)

// BookTotals são as somas do período de um livro por categoria, tipo e classificação (database.GroupsBetween).
type BookTotals struct {
	Book   models.Book
	Groups []models.MonthGroup
}

// BuildOverviewFromTotals dá o mesmo resultado de BuildOverview sem carregar os lançamentos do período: os totais
// vêm somados e os lados ligados ou marcados vêm de sides (os lançamentos das ligações e dos marcados, de qualquer
// livro e data; repetidos contam uma vez). Só entram os lados do período, de um dos livros e fora de Movimentação
// interna e Investimento, como em BuildOverview.
func BuildOverviewFromTotals(books []BookTotals, from, to string, sides []models.Transaction, linked, solo map[int64]bool) Overview {
	var o Overview
	var income, expense int64
	inBooks := map[int64]bool{}
	for _, b := range books {
		inBooks[b.Book.ID] = true
		s, _, out := dashboard.FromGroups(b.Groups, nil)
		o.InternalIn += out.InternalIn
		o.InternalOut += out.InternalOut
		o.Invested += out.Invested
		o.Redeemed += out.Redeemed
		o.Rows = append(o.Rows, BookRow{Book: b.Book, Summary: s})
		income += s.TotalIncome
		expense += s.TotalExpense
	}
	seen := map[int64]bool{}
	for _, t := range sides {
		if seen[t.ID] || !inBooks[t.BookID] || t.Date < from || t.Date > to || models.IsOutsideTotals(t.Category) {
			continue
		}
		seen[t.ID] = true
		switch {
		case linked[t.ID]:
			o.LinkedSides++
			if t.Type == "income" {
				o.LinkedIn += t.AmountCents
			} else {
				o.LinkedOut += t.AmountCents
			}
		case solo[t.ID]:
			o.SoloSides++
			if t.Type == "income" {
				o.SoloIn += t.AmountCents
			} else {
				o.SoloOut += t.AmountCents
			}
		}
	}
	o.Total.TotalIncome = income - o.LinkedIn - o.SoloIn
	o.Total.TotalExpense = expense - o.LinkedOut - o.SoloOut
	o.Total.Balance = o.Total.TotalIncome - o.Total.TotalExpense
	return o
}
