package transfer

import (
	"finpi/internal/dashboard"
	"finpi/internal/models"
)

// BookInput são os lançamentos de um livro ativo no período da visão geral.
type BookInput struct {
	Book models.Book
	Txs  []models.Transaction
}

// BookRow são os totais de um livro (iguais aos que ele mostra sozinho: sem a Movimentação interna).
type BookRow struct {
	Book    models.Book
	Summary models.Summary
}

// Overview é o resumo da visão geral. Total = soma dos livros − transferências ligadas − fora do FinPi.
type Overview struct {
	Rows                    []BookRow
	LinkedIn, LinkedOut     int64 // lados ligados dentro do período (entradas e saídas)
	LinkedSides             int
	SoloIn, SoloOut         int64 // marcados "outro lado fora do FinPi" dentro do período
	SoloSides               int
	InternalIn, InternalOut int64 // Movimentação interna (já fora dos totais de cada livro)
	Total                   models.Summary
}

// BuildOverview soma os livros e subtrai os lados ligados (linked) e os marcados (solo), que são conjuntos de ID
// de lançamento. Cada lado é tratado individualmente: se só um lado cai no período, só ele é subtraído.
func BuildOverview(books []BookInput, linked, solo map[int64]bool) Overview {
	var o Overview
	var income, expense int64
	for _, b := range books {
		regular, internalIn, internalOut := dashboard.SplitInternal(b.Txs)
		o.InternalIn += internalIn
		o.InternalOut += internalOut
		s := dashboard.Summarize(regular)
		o.Rows = append(o.Rows, BookRow{Book: b.Book, Summary: s})
		income += s.TotalIncome
		expense += s.TotalExpense
		for _, t := range regular {
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
	}
	o.Total.TotalIncome = income - o.LinkedIn - o.SoloIn
	o.Total.TotalExpense = expense - o.LinkedOut - o.SoloOut
	o.Total.Balance = o.Total.TotalIncome - o.Total.TotalExpense
	return o
}
