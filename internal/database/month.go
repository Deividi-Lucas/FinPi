package database

import (
	"strings"

	"finpi/internal/models"
)

// MonthGroups soma os lançamentos do mês (AAAA-MM) por categoria, tipo e classificação. Mês inválido: nada.
func (d *DB) MonthGroups(yearMonth string) ([]models.MonthGroup, error) {
	from, to, ok := monthRange(yearMonth)
	if !ok {
		return nil, nil
	}
	return d.groups(`date >= ? AND date < ?`, from, to)
}

// GroupsBetween soma os lançamentos com data de from a to (inclusive, AAAA-MM-DD) por categoria, tipo e
// classificação: o mesmo período de GetTransactionsBetween.
func (d *DB) GroupsBetween(from, to string) ([]models.MonthGroup, error) {
	return d.groups(`date >= ? AND date <= ?`, from, to)
}

func (d *DB) groups(dateCond string, from, to string) ([]models.MonthGroup, error) {
	rows, err := d.conn.Query(`
		SELECT category, type, priority, COUNT(*), SUM(amount_cents)
		FROM transactions
		WHERE book_id = ? AND `+dateCond+`
		GROUP BY category, type, priority
		ORDER BY category, type, priority
	`, d.book, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.MonthGroup
	for rows.Next() {
		var g models.MonthGroup
		if err := rows.Scan(&g.Category, &g.Type, &g.Priority, &g.Count, &g.Cents); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// MonthExpenseExtremes devolve a maior e a menor despesa do mês, ignorando as categorias em exclude (texto exato).
// No empate de valor fica a mais recente (data e, na igualdade, maior ID), como em dashboard.Compute.
// Sem despesas, devolve nil nas duas.
func (d *DB) MonthExpenseExtremes(yearMonth string, exclude []string) (largest, smallest *models.Transaction, err error) {
	from, to, ok := monthRange(yearMonth)
	if !ok {
		return nil, nil, nil
	}
	where := `book_id = ? AND date >= ? AND date < ? AND type = 'expense'`
	args := []any{d.book, from, to}
	if len(exclude) > 0 {
		where += ` AND category NOT IN (?` + strings.Repeat(", ?", len(exclude)-1) + `)`
		for _, c := range exclude {
			args = append(args, c)
		}
	}
	one := func(order string) (*models.Transaction, error) {
		// O ID sai do índice de cobertura (sem abrir a tabela); só a linha escolhida é lida inteira.
		rows, err := d.conn.Query(`SELECT `+transactionColumns+` FROM transactions WHERE id = (
			SELECT id FROM transactions INDEXED BY idx_transactions_month_sums WHERE `+where+` ORDER BY `+order+` LIMIT 1)`, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		list, err := scanTransactions(rows)
		if err != nil || len(list) == 0 {
			return nil, err
		}
		return &list[0], nil
	}
	if largest, err = one(`amount_cents DESC, date DESC, id DESC`); err != nil {
		return nil, nil, err
	}
	if smallest, err = one(`amount_cents ASC, date DESC, id DESC`); err != nil {
		return nil, nil, err
	}
	return largest, smallest, nil
}

// LatestTransactions devolve os limit lançamentos mais recentes do mês, na ordem de GetTransactions.
func (d *DB) LatestTransactions(yearMonth string, limit int) ([]models.Transaction, error) {
	from, to, ok := monthRange(yearMonth)
	if !ok {
		return nil, nil
	}
	rows, err := d.conn.Query(`
		SELECT `+transactionColumns+`
		FROM transactions
		WHERE book_id = ? AND date >= ? AND date < ?
		ORDER BY date DESC, id DESC
		LIMIT ?
	`, d.book, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTransactions(rows)
}
