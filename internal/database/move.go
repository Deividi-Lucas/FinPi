package database

import (
	"database/sql"
	"errors"
	"fmt"

	"finpi/internal/textutil"
)

// ErrSameBook indica uma movimentação para o próprio livro de origem.
var ErrSameBook = errors.New("o livro de destino é o mesmo de origem")

// MoveToBook leva os lançamentos do livro atual para o livro dest (ativo), numa transação. Cada categoria é
// reaproveitada no destino (mesmo nome, sem caixa e acentos) ou criada lá. O lote fica no livro de origem e
// pode ser desfeito. skipped conta os ids que não existem no livro de origem.
func (d *DB) MoveToBook(ids []int64, dest int64) (applied, skipped int, err error) {
	if dest == d.book {
		return 0, 0, ErrSameBook
	}
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var destName string
	var archived int
	err = tx.QueryRow(`SELECT name, archived FROM books WHERE id = ?`, dest).Scan(&destName, &archived)
	if err != nil || archived != 0 {
		return 0, 0, ErrBookNotFound
	}

	// categorias do destino, por nome normalizado
	known := map[string]string{}
	rows, err := tx.Query(`SELECT name, name_key FROM categories WHERE book_id = ?`, dest)
	if err != nil {
		return 0, 0, err
	}
	for rows.Next() {
		var name, key string
		if err := rows.Scan(&name, &key); err != nil {
			rows.Close()
			return 0, 0, err
		}
		known[key] = name
	}
	rows.Close()

	current, err := loadTransactions(tx, ids, d.book)
	if err != nil {
		return 0, 0, err
	}
	var items []batchItem
	for _, id := range ids {
		before, ok := current[id]
		if !ok {
			skipped++
			continue
		}
		key := textutil.Fold(before.Category)
		name, exists := known[key]
		if !exists {
			clean, err := cleanCategoryName(before.Category)
			if err != nil {
				return 0, 0, err
			}
			// a classificação acompanha a categoria de mesmo nome do livro de origem (se houver)
			var priority string
			err = tx.QueryRow(`SELECT priority FROM categories WHERE book_id = ? AND name_key = ?`, d.book, textutil.Fold(clean)).Scan(&priority)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return 0, 0, err
			}
			if _, err := tx.Exec(`INSERT INTO categories (book_id, name, name_key, priority) VALUES (?, ?, ?, ?)`, dest, clean, textutil.Fold(clean), priority); err != nil {
				return 0, 0, err
			}
			known[textutil.Fold(clean)] = clean
			name = clean
		}
		if _, err := tx.Exec(`UPDATE transactions SET book_id = ?, category = ? WHERE id = ? AND book_id = ?`, dest, name, id, d.book); err != nil {
			return 0, 0, err
		}
		after := before
		after.BookID, after.Category = dest, name
		items = append(items, batchItem{before: before, after: &after})
	}
	if len(items) == 0 {
		return 0, skipped, nil
	}
	summary := fmt.Sprintf("Movimentação de %d %s para %s", len(items), map[bool]string{true: "lançamento", false: "lançamentos"}[len(items) == 1], destName)
	if err := d.recordBatch(tx, "edit", summary, items); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(items), skipped, nil
}
