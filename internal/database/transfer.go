package database

import (
	"database/sql"
	"errors"
	"time"

	"finpi/internal/models"
	"finpi/internal/transfer"
)

// ToTransferTx converte um lançamento para o que a regra de transferências precisa.
func ToTransferTx(t models.Transaction) transfer.Tx {
	return transfer.Tx{ID: t.ID, BookID: t.BookID, Type: t.Type, AmountCents: t.AmountCents, Date: t.Date, Description: t.Description}
}

const crossColumns = `t.id, t.description, t.amount_cents, t.type, t.category, t.date, t.book_id`

func scanCross(sc interface{ Scan(...any) error }) (models.Transaction, error) {
	var t models.Transaction
	err := sc.Scan(&t.ID, &t.Description, &t.AmountCents, &t.Type, &t.Category, &t.Date, &t.BookID)
	return t, err
}

// AllTransactions devolve os lançamentos de TODOS os livros ativos, do mais recente para o mais antigo, até limit.
// truncated indica que havia mais do que o limite.
func (d *DB) AllTransactions(limit int) ([]models.Transaction, bool, error) {
	rows, err := d.conn.Query(`SELECT `+crossColumns+` FROM transactions t JOIN books b ON b.id = t.book_id
		WHERE b.archived = 0 ORDER BY t.date DESC, t.id DESC LIMIT ?`, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var list []models.Transaction
	for rows.Next() {
		t, err := scanCross(rows)
		if err != nil {
			return nil, false, err
		}
		list = append(list, t)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	truncated := len(list) > limit
	if truncated {
		list = list[:limit]
	}
	return list, truncated, nil
}

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// crossTx lê um lançamento de um livro ativo, em qualquer livro.
func crossTx(q rowQuerier, id int64) (models.Transaction, error) {
	t, err := scanCross(q.QueryRow(`SELECT `+crossColumns+` FROM transactions t JOIN books b ON b.id = t.book_id
		WHERE t.id = ? AND b.archived = 0`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, transfer.ErrNotFound
	}
	return t, err
}

// TransferLink é uma ligação gravada. Valid diz se ela ainda vale (os dois lançamentos existem e formam um par).
type TransferLink struct {
	ID        int64
	Out, In   models.Transaction
	Valid     bool
	CreatedAt string
}

const linkQuery = `
	SELECT l.id, l.created_at,
		o.id, o.description, o.amount_cents, o.type, o.category, o.date, o.book_id,
		i.id, i.description, i.amount_cents, i.type, i.category, i.date, i.book_id
	FROM transfer_links l
	LEFT JOIN transactions o ON o.id = l.out_tx_id
	LEFT JOIN transactions i ON i.id = l.in_tx_id
	ORDER BY l.id DESC`

type nullTx struct {
	id          sql.NullInt64
	description sql.NullString
	cents       sql.NullInt64
	typ         sql.NullString
	category    sql.NullString
	date        sql.NullString
	book        sql.NullInt64
}

func (n *nullTx) dest() []any {
	return []any{&n.id, &n.description, &n.cents, &n.typ, &n.category, &n.date, &n.book}
}

func (n nullTx) tx() (models.Transaction, bool) {
	if !n.id.Valid {
		return models.Transaction{}, false
	}
	return models.Transaction{ID: n.id.Int64, Description: n.description.String, AmountCents: n.cents.Int64,
		Type: n.typ.String, Category: n.category.String, Date: n.date.String, BookID: n.book.Int64}, true
}

// ListTransferLinks devolve as ligações (mais recentes primeiro), com os dois lados e se ainda valem.
func (d *DB) ListTransferLinks() ([]TransferLink, error) {
	return listTransferLinks(d.conn)
}

type queryer2 interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

func listTransferLinks(q queryer2) ([]TransferLink, error) {
	rows, err := q.Query(linkQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TransferLink
	for rows.Next() {
		var l TransferLink
		var o, i nullTx
		dest := append([]any{&l.ID, &l.CreatedAt}, o.dest()...)
		dest = append(dest, i.dest()...)
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		ot, okO := o.tx()
		it, okI := i.tx()
		l.Out, l.In = ot, it
		l.Valid = okO && okI && transfer.Valid(ToTransferTx(ot), ToTransferTx(it)) == nil
		out = append(out, l)
	}
	return out, rows.Err()
}

// ListSolo devolve os lançamentos marcados "outro lado fora do FinPi" (livros ativos).
func (d *DB) ListSolo() ([]models.Transaction, error) {
	rows, err := d.conn.Query(`SELECT ` + crossColumns + ` FROM transfer_solo s
		JOIN transactions t ON t.id = s.tx_id JOIN books b ON b.id = t.book_id
		WHERE b.archived = 0 ORDER BY s.rowid DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []models.Transaction
	for rows.Next() {
		t, err := scanCross(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// purgeInvalidTransfers apaga as ligações que deixaram de valer e as marcas de lançamentos que não existem mais,
// para liberar os lançamentos. Roda dentro da transação de quem vai gravar.
func purgeInvalidTransfers(tx *sql.Tx) error {
	links, err := listTransferLinks(tx)
	if err != nil {
		return err
	}
	for _, l := range links {
		if l.Valid {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM transfer_links WHERE id = ?`, l.ID); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`DELETE FROM transfer_solo WHERE tx_id NOT IN (SELECT id FROM transactions)`)
	return err
}

// taken diz se algum dos lançamentos já está numa ligação ou marcado.
func taken(tx *sql.Tx, ids ...int64) (bool, error) {
	for _, id := range ids {
		var n int
		if err := tx.QueryRow(`SELECT
			(SELECT COUNT(*) FROM transfer_links WHERE out_tx_id = ? OR in_tx_id = ?) +
			(SELECT COUNT(*) FROM transfer_solo WHERE tx_id = ?)`, id, id, id).Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

// LinkTransfer liga uma saída a uma entrada de livros diferentes, com o mesmo valor.
func (d *DB) LinkTransfer(outID, inID int64) error {
	tx, err := d.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := purgeInvalidTransfers(tx); err != nil {
		return err
	}
	out, err := crossTx(tx, outID)
	if err != nil {
		return err
	}
	in, err := crossTx(tx, inID)
	if err != nil {
		return err
	}
	if err := transfer.Valid(ToTransferTx(out), ToTransferTx(in)); err != nil {
		return err
	}
	busy, err := taken(tx, outID, inID)
	if err != nil {
		return err
	}
	if busy {
		return transfer.ErrTaken
	}
	if _, err := tx.Exec(`INSERT INTO transfer_links (out_tx_id, in_tx_id, created_at) VALUES (?, ?, ?)`,
		outID, inID, d.now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	return tx.Commit()
}

// UnlinkTransfer desfaz uma ligação pelo id dela.
func (d *DB) UnlinkTransfer(id int64) error {
	res, err := d.conn.Exec(`DELETE FROM transfer_links WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return transfer.ErrNotFound
	}
	return nil
}

// MarkSolo marca lançamentos como "outro lado fora do FinPi". Ignora (skipped) os que não existem, os repetidos e os
// que já estão ligados ou marcados.
func (d *DB) MarkSolo(ids []int64) (marked, skipped int, err error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	if err := purgeInvalidTransfers(tx); err != nil {
		return 0, 0, err
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		if _, err := crossTx(tx, id); err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				skipped++
				continue
			}
			return 0, 0, err
		}
		busy, err := taken(tx, id)
		if err != nil {
			return 0, 0, err
		}
		if busy {
			skipped++
			continue
		}
		if _, err := tx.Exec(`INSERT INTO transfer_solo (tx_id, created_at) VALUES (?, ?)`, id, d.now().UTC().Format(time.RFC3339)); err != nil {
			return 0, 0, err
		}
		marked++
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return marked, skipped, nil
}

// UnmarkSolo tira a marca "fora do FinPi" e devolve quantas marcas foram removidas.
func (d *DB) UnmarkSolo(ids []int64) (int, error) {
	n := 0
	for _, id := range ids {
		res, err := d.conn.Exec(`DELETE FROM transfer_solo WHERE tx_id = ?`, id)
		if err != nil {
			return n, err
		}
		if c, err := res.RowsAffected(); err == nil {
			n += int(c)
		}
	}
	return n, nil
}

// execer é satisfeito por *sql.Tx e *sql.DB.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// deleteTransferRefs apaga a ligação e a marca de um lançamento que foi excluído.
func deleteTransferRefs(e execer, id int64) error {
	if _, err := e.Exec(`DELETE FROM transfer_links WHERE out_tx_id = ? OR in_tx_id = ?`, id, id); err != nil {
		return err
	}
	_, err := e.Exec(`DELETE FROM transfer_solo WHERE tx_id = ?`, id)
	return err
}
