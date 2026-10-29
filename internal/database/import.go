package database

import (
	"errors"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

// ImportedExternalIDs devolve o conjunto de identificadores de origem já gravados.
func (d *DB) ImportedExternalIDs() (map[string]bool, error) {
	rows, err := d.conn.Query(`SELECT external_id FROM transactions WHERE external_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ManualTransactionKeys devolve as chaves (data|centavos|tipo) dos lançamentos feitos à mão no intervalo.
func (d *DB) ManualTransactionKeys(minDate, maxDate string) (map[string]bool, error) {
	rows, err := d.conn.Query(
		`SELECT date, amount_cents, type FROM transactions WHERE book_id = ? AND external_id = '' AND date BETWEEN ? AND ?`,
		d.book, minDate, maxDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var (
			date, typ string
			cents     int64
		)
		if err := rows.Scan(&date, &cents, &typ); err != nil {
			return nil, err
		}
		out[models.TransactionKey(date, cents, typ)] = true
	}
	return out, rows.Err()
}

// LatestCategoryByDescription mapeia a descrição normalizada (textutil.Fold) para a categoria do
// lançamento mais recente (data, depois ID) com essa descrição.
func (d *DB) LatestCategoryByDescription() (map[string]string, error) {
	rows, err := d.conn.Query(`SELECT description, category FROM transactions WHERE book_id = ? ORDER BY date ASC, id ASC`, d.book)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]string{}
	for rows.Next() {
		var desc, category string
		if err := rows.Scan(&desc, &category); err != nil {
			return nil, err
		}
		out[textutil.Fold(desc)] = category
	}
	return out, rows.Err()
}

// ImportTransactions grava as linhas, no livro do DB, numa única transação. Linhas cujo external_id já existe são
// ignoradas (skipped). Qualquer outro erro desfaz tudo.
func (d *DB) ImportTransactions(rows []models.Transaction) (inserted, skipped int, err error) {
	for _, t := range rows {
		if t.ExternalID == "" {
			return 0, 0, errors.New("external_id é obrigatório na importação")
		}
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO transactions (description, amount_cents, type, category, date, priority, external_id, book_id)
		VALUES (?, ?, ?, ?, ?, '', ?, ?)
		ON CONFLICT(external_id) WHERE external_id <> '' DO NOTHING`)
	if err != nil {
		return 0, 0, err
	}
	defer stmt.Close()

	for _, t := range rows {
		res, err := stmt.Exec(t.Description, t.AmountCents, t.Type, t.Category, t.Date, t.ExternalID, d.book)
		if err != nil {
			return 0, 0, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return 0, 0, err
		}
		if n == 0 {
			skipped++
		} else {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return inserted, skipped, nil
}
