package report

import "sort"

// MaxRankedRows é o máximo de linhas de cada tabela ranqueada (gastos recorrentes e fontes de receita) que a
// página mostra em "Ver todas". A página mostra 10 e reordena no navegador; mandar milhares de linhas (um ano com
// muitas descrições diferentes) deixava o relatório com megabytes de HTML e lento de gerar.
const MaxRankedRows = 100

// Limited corta as tabelas para as n primeiras linhas de cada ordenação da página (vezes e total) e guarda o
// tamanho original em CategoryTotal e DescriptionTotal.
func (r Recurrence) Limited(n int) Recurrence {
	r.CategoryTotal, r.DescriptionTotal = len(r.ByCategory), len(r.ByDescription)
	r.ByCategory = keepTop(r.ByCategory, n, recurringSorts)
	r.ByDescription = keepTop(r.ByDescription, n, recurringSorts)
	return r
}

// Limited corta as tabelas para as n primeiras linhas de cada ordenação da página (total, vezes e meses) e guarda
// o tamanho original em SourceTotal e CategoryTotal.
func (r IncomeReport) Limited(n int) IncomeReport {
	r.SourceTotal, r.CategoryTotal = len(r.BySource), len(r.ByCategory)
	r.BySource = keepTop(r.BySource, n, incomeSorts)
	r.ByCategory = keepTop(r.ByCategory, n, incomeSorts)
	return r
}

// Ordenações oferecidas na página (report.html, setupRanked): a chave escolhida e, no empate, as demais na ordem
// da configuração; todas decrescentes.
var (
	recurringSorts = [][]func(RecurringRow) int64{
		{rCount, rTotal},
		{rTotal, rCount},
	}
	incomeSorts = [][]func(IncomeRow) int64{
		{iTotal, iCount, iMonths},
		{iCount, iTotal, iMonths},
		{iMonths, iTotal, iCount},
	}
)

func rCount(r RecurringRow) int64 { return int64(r.Count) }
func rTotal(r RecurringRow) int64 { return r.TotalCents }
func iTotal(r IncomeRow) int64    { return r.TotalCents }
func iCount(r IncomeRow) int64    { return int64(r.Count) }
func iMonths(r IncomeRow) int64   { return int64(r.Months) }

// keepTop mantém, na ordem original, as linhas que ficam entre as n primeiras em alguma das ordenações (empate
// final pela posição original, como na página). Assim qualquer ordenação escolhida mostra as mesmas n primeiras.
func keepTop[T any](rows []T, n int, sorts [][]func(T) int64) []T {
	if len(rows) <= n {
		return rows
	}
	keep := make([]bool, len(rows))
	idx := make([]int, len(rows))
	for _, keys := range sorts {
		for i := range idx {
			idx[i] = i
		}
		sort.SliceStable(idx, func(a, b int) bool {
			for _, k := range keys {
				if x, y := k(rows[idx[a]]), k(rows[idx[b]]); x != y {
					return x > y
				}
			}
			return idx[a] < idx[b]
		})
		for _, i := range idx[:n] {
			keep[i] = true
		}
	}
	out := make([]T, 0, n*len(sorts))
	for i, ok := range keep {
		if ok {
			out = append(out, rows[i])
		}
	}
	return out
}
