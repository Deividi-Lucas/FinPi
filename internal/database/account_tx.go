package database

import (
	"database/sql"
	"errors"
	"fmt"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

type rowQuerierAny interface {
	QueryRow(query string, args ...any) *sql.Row
}

// checkAccount valida a conta de um lançamento do livro: 0 vale; a conta tem de existir no livro
// (ErrAccountNotFound) e, se arquivada, só vale quando é a mesma que o lançamento já tinha (current).
func checkAccount(q rowQuerierAny, book, accountID, current int64) error {
	if accountID == 0 {
		return nil
	}
	var archived int
	err := q.QueryRow(`SELECT archived FROM accounts WHERE id = ? AND book_id = ?`, accountID, book).Scan(&archived)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAccountNotFound
	}
	if err != nil {
		return err
	}
	if archived != 0 && accountID != current {
		return ErrAccountArchived
	}
	return nil
}

// currentAccount devolve a conta atual de um lançamento do livro (0 se não existir).
func currentAccount(q rowQuerierAny, book, id int64) (int64, error) {
	var acc int64
	err := q.QueryRow(`SELECT account_id FROM transactions WHERE id = ? AND book_id = ?`, id, book).Scan(&acc)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return acc, err
}

// bankAccountTx reaproveita a conta ativa do banco no livro ou a cria, dentro da transação.
func bankAccountTx(tx *sql.Tx, book int64, bank string) (int64, error) {
	var id int64
	err := tx.QueryRow(`SELECT id FROM accounts WHERE book_id = ? AND bank = ? AND archived = 0 ORDER BY id LIMIT 1`, book, bank).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	base := models.BankName(bank)
	if base == "" {
		return 0, ErrAccountName
	}
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			name = fmt.Sprintf("%s (%d)", base, i)
		}
		var n int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM accounts WHERE book_id = ? AND name_key = ?`, book, textutil.Fold(name)).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			continue
		}
		res, err := tx.Exec(`INSERT INTO accounts (book_id, name, name_key, bank) VALUES (?, ?, ?, ?)`, book, name, textutil.Fold(name), bank)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
}
