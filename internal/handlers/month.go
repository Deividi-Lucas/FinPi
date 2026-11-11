package handlers

import (
	"finpi/internal/dashboard"
	"finpi/internal/database"
	"finpi/internal/filter"
	"finpi/internal/installments"
	"finpi/internal/models"
)

// monthData reúne o que a tela principal mostra do mês: totais, resumo e a lista do histórico.
type monthData struct {
	Summary                 models.Summary
	Stats                   models.MonthStats
	Outside                 dashboard.Outside
	Shown                   []models.Transaction // histórico exibido (até maxHistoryRows)
	Count                   int                  // lançamentos que passam pelos filtros
	HistIncome, HistExpense int64
}

// loadMonth escolhe o caminho: sem filtros, as somas vêm prontas do SQL e só as linhas exibidas são lidas
// (um mês com milhares de lançamentos não é carregado inteiro); com filtros, lê tudo e filtra em memória.
func loadMonth(db *database.DB, month string, cats []models.Category, f filter.Filter) (monthData, error) {
	if f.Active() == 0 && !f.AllMonths {
		return loadMonthFast(db, month, cats)
	}
	return loadMonthFull(db, month, cats, f)
}

// loadMonthFull carrega todos os lançamentos do mês (ou de todos os meses, em f.AllMonths) e calcula em memória.
func loadMonthFull(db *database.DB, month string, cats []models.Category, f filter.Filter) (monthData, error) {
	transactions, err := db.GetTransactions(month)
	if err != nil {
		return monthData{}, err
	}
	installments.Annotate(transactions)
	dashboard.Annotate(transactions, cats)
	// Movimentação interna (resgates, transferências entre contas) fica fora dos totais e do dashboard.
	regular, outside := dashboard.SplitOutside(transactions)
	m := monthData{Summary: dashboard.Summarize(regular), Stats: dashboard.Compute(regular, cats), Outside: outside}

	// A lista do histórico segue os filtros; os cartões e o dashboard do mês continuam sendo do mês inteiro.
	list := transactions
	if f.AllMonths {
		all, err := db.GetTransactionsBetween("0001-01-01", "9999-12-31")
		if err != nil {
			return monthData{}, err
		}
		for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 { // do mais recente para o mais antigo
			all[i], all[j] = all[j], all[i]
		}
		installments.Annotate(all)
		dashboard.Annotate(all, cats)
		list = all
	}
	m.Shown, m.Count, m.HistIncome, m.HistExpense = applyFilter(f, list)
	return m, nil
}

// loadMonthFast calcula o mesmo que loadMonthFull sem filtros, com somas no SQL e só as linhas exibidas.
func loadMonthFast(db *database.DB, month string, cats []models.Category) (monthData, error) {
	groups, err := db.MonthGroups(month)
	if err != nil {
		return monthData{}, err
	}
	var m monthData
	m.Summary, m.Stats, m.Outside = dashboard.FromGroups(groups, cats)
	for _, g := range groups {
		m.Count += g.Count
	}
	// Sem filtro, as somas do histórico são as dos totais (o que fica fora dos totais também fica fora delas).
	m.HistIncome, m.HistExpense = m.Summary.TotalIncome, m.Summary.TotalExpense

	largest, smallest, err := db.MonthExpenseExtremes(month, dashboard.OutsideCategories(groups))
	if err != nil {
		return monthData{}, err
	}
	for _, p := range []*models.Transaction{largest, smallest} {
		if p != nil {
			one := []models.Transaction{*p}
			installments.Annotate(one)
			dashboard.Annotate(one, cats)
			*p = one[0]
		}
	}
	m.Stats.Largest, m.Stats.Smallest = largest, smallest

	if m.Shown, err = db.LatestTransactions(month, maxHistoryRows); err != nil {
		return monthData{}, err
	}
	installments.Annotate(m.Shown)
	dashboard.Annotate(m.Shown, cats)
	return m, nil
}
