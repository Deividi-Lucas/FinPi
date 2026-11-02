package database

import (
	"database/sql"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

// ImportAccount diz em qual conta do livro a importação grava. Exatamente um dos campos vale:
// ID (conta existente), NewName (criar conta com esse nome) ou Bank (conta padrão do banco, criada se faltar).
type ImportAccount struct {
	ID      int64
	NewName string
	Bank    string
}

// ImportTransactionsToAccount grava as linhas como ImportTransactions, atribuindo todas à conta escolhida. Criar a
// conta e gravar as linhas acontecem na mesma transação: se a gravação falhar, nenhuma conta é criada.
func (d *DB) ImportTransactionsToAccount(rows []models.Transaction, acc ImportAccount) (inserted, skipped int, err error) {
	return d.importRows(rows, func(tx *sql.Tx) (int64, error) {
		switch {
		case acc.ID > 0:
			if err := checkAccount(tx, d.book, acc.ID, 0); err != nil {
				return 0, err
			}
			return acc.ID, nil
		case acc.NewName != "":
			name, err := cleanAccountName(acc.NewName)
			if err != nil {
				return 0, err
			}
			key := textutil.Fold(name)
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM accounts WHERE book_id = ? AND name_key = ?`, d.book, key).Scan(&n); err != nil {
				return 0, err
			}
			if n > 0 {
				return 0, ErrAccountDuplicate
			}
			res, err := tx.Exec(`INSERT INTO accounts (book_id, name, name_key, bank) VALUES (?, ?, ?, '')`, d.book, name, key)
			if err != nil {
				return 0, err
			}
			return res.LastInsertId()
		case acc.Bank != "":
			return bankAccountTx(tx, d.book, acc.Bank)
		}
		return 0, ErrAccountNotFound
	})
}
