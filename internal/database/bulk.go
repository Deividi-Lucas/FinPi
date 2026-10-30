package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

const createBatchesSQL = `
	CREATE TABLE IF NOT EXISTS batches (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at TEXT NOT NULL,
		action TEXT NOT NULL CHECK(action IN ('edit', 'delete')),
		summary TEXT NOT NULL,
		count INTEGER NOT NULL,
		undone_at TEXT
	)`

const createBatchItemsSQL = `
	CREATE TABLE IF NOT EXISTS batch_items (
		batch_id INTEGER NOT NULL,
		tx_id INTEGER NOT NULL,
		before_json TEXT NOT NULL,
		after_json TEXT,
		PRIMARY KEY (batch_id, tx_id)
	)`

const (
	maxBatches  = 10                  // lotes guardados para desfazer
	batchMaxAge = 30 * 24 * time.Hour // idade máxima de um lote
)

// ErrNoBatch indica que não há edição em lote para desfazer.
var ErrNoBatch = errors.New("não há edição em lote para desfazer")

// snapshot é o estado de um lançamento guardado com o lote.
type snapshot struct {
	ID          int64  `json:"id"`
	Description string `json:"description"`
	AmountCents int64  `json:"amount_cents"`
	Type        string `json:"type"`
	Category    string `json:"category"`
	Date        string `json:"date"`
	Priority    string `json:"priority"`
	ExternalID  string `json:"external_id"`
	CreatedAt   string `json:"created_at"`
	BookID      int64  `json:"book_id"`

	OriginalDescription string `json:"original_description"`
}

func snapshotOf(t models.Transaction) snapshot {
	return snapshot{
		ID: t.ID, Description: t.Description, AmountCents: t.AmountCents, Type: t.Type, Category: t.Category,
		Date: t.Date, Priority: t.Priority, ExternalID: t.ExternalID, CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339Nano), BookID: t.BookID,
		OriginalDescription: t.OriginalDescription,
	}
}

// sameState compara todos os campos, menos a data de criação.
func (s snapshot) sameState(o snapshot) bool {
	return s.ID == o.ID && s.Description == o.Description && s.AmountCents == o.AmountCents && s.Type == o.Type &&
		s.Category == o.Category && s.Date == o.Date && s.Priority == o.Priority && s.ExternalID == o.ExternalID && s.BookID == o.BookID
}

const loadColumns = `id, description, amount_cents, type, category, date, priority, external_id, created_at, book_id, original_description`

// loadTransactions lê os lançamentos que existem entre os ids (com o identificador de origem).
// Com book > 0 só alcança os daquele livro; com book == 0, os de qualquer livro.
func loadTransactions(q queryer, ids []int64, book int64) (map[int64]models.Transaction, error) {
	out := map[int64]models.Transaction{}
	if len(ids) == 0 {
		return out, nil
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	query := `SELECT ` + loadColumns + ` FROM transactions WHERE id IN (` + marks + `)`
	if book > 0 {
		query += ` AND book_id = ?`
		args = append(args, book)
	}
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t models.Transaction
		if err := rows.Scan(&t.ID, &t.Description, &t.AmountCents, &t.Type, &t.Category, &t.Date, &t.Priority, &t.ExternalID, &t.CreatedAt, &t.BookID, &t.OriginalDescription); err != nil {
			return nil, err
		}
		out[t.ID] = t
	}
	return out, rows.Err()
}

// GetTransactionsByIDs devolve os lançamentos que existem, do mais recente para o mais antigo.
func (d *DB) GetTransactionsByIDs(ids []int64) ([]models.Transaction, error) {
	m, err := loadTransactions(d.conn, ids, d.book)
	if err != nil {
		return nil, err
	}
	list := make([]models.Transaction, 0, len(m))
	for _, t := range m {
		list = append(list, t)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Date != list[j].Date {
			return list[i].Date > list[j].Date
		}
		return list[i].ID > list[j].ID
	})
	return list, nil
}

type batchItem struct {
	before models.Transaction
	after  *models.Transaction // nil na exclusão
}

// recordBatch grava o lote e seus itens e descarta os lotes velhos (por quantidade e por idade).
func (d *DB) recordBatch(tx *sql.Tx, action, summary string, items []batchItem) error {
	now := d.now().UTC()
	res, err := tx.Exec(`INSERT INTO batches (created_at, action, summary, count, book_id) VALUES (?, ?, ?, ?, ?)`,
		now.Format(time.RFC3339), action, summary, len(items), d.book)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	for _, it := range items {
		before, err := json.Marshal(snapshotOf(it.before))
		if err != nil {
			return err
		}
		var after any
		if it.after != nil {
			b, err := json.Marshal(snapshotOf(*it.after))
			if err != nil {
				return err
			}
			after = string(b)
		}
		if _, err := tx.Exec(`INSERT INTO batch_items (batch_id, tx_id, before_json, after_json) VALUES (?, ?, ?, ?)`,
			id, it.before.ID, string(before), after); err != nil {
			return err
		}
	}
	return d.purgeBatches(tx, now)
}

func (d *DB) purgeBatches(tx *sql.Tx, now time.Time) error {
	cutoff := now.Add(-batchMaxAge).Format(time.RFC3339)
	rows, err := tx.Query(`SELECT id FROM batches WHERE created_at < ? OR (book_id = ? AND id NOT IN (SELECT id FROM batches WHERE book_id = ? ORDER BY id DESC LIMIT ?))`, cutoff, d.book, d.book, maxBatches)
	if err != nil {
		return err
	}
	var old []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		old = append(old, id)
	}
	rows.Close()
	for _, id := range old {
		if _, err := tx.Exec(`DELETE FROM batch_items WHERE batch_id = ?`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM batches WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// ApplyEdits relê os lançamentos dentro de uma transação, aplica edit a cada um e grava só os que mudaram
// (descrição, categoria, data e prioridade), junto com o lote que permite desfazer. Se edit falhar para algum
// lançamento, nada é gravado. skipped conta os que não existem mais e os que não mudaram.
func (d *DB) ApplyEdits(summary string, ids []int64, edit func(models.Transaction) (models.Transaction, error)) (applied, skipped int, err error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

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
		after, err := edit(before)
		if err != nil {
			return 0, 0, err
		}
		if !models.ValidPriority(after.Priority) {
			return 0, 0, ErrInvalidPriority
		}
		if before.Description == after.Description && before.Category == after.Category && before.Date == after.Date && before.Priority == after.Priority {
			skipped++
			continue
		}
		if _, err := tx.Exec(`UPDATE transactions SET description = ?, category = ?, date = ?, priority = ? WHERE id = ?`,
			after.Description, after.Category, after.Date, after.Priority, id); err != nil {
			return 0, 0, err
		}
		a := after
		a.BookID = before.BookID
		items = append(items, batchItem{before: before, after: &a})
	}
	if len(items) == 0 {
		return 0, skipped, nil
	}
	if err := d.recordBatch(tx, "edit", summary, items); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(items), skipped, nil
}

// ApplyDeletes exclui os lançamentos que existem, numa transação, e grava o lote que permite desfazer.
func (d *DB) ApplyDeletes(summary string, ids []int64) (applied, skipped int, err error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

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
		if _, err := tx.Exec(`DELETE FROM transactions WHERE id = ?`, id); err != nil {
			return 0, 0, err
		}
		items = append(items, batchItem{before: before})
	}
	if len(items) == 0 {
		return 0, skipped, nil
	}
	if err := d.recordBatch(tx, "delete", summary, items); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return len(items), skipped, nil
}

// UndoableBatch é o lote mais recente que ainda pode ser desfeito.
type UndoableBatch struct {
	ID        int64
	Action    string // "edit" ou "delete"
	Summary   string
	Count     int
	CreatedAt time.Time
}

// LatestUndoableBatch devolve o lote mais recente ainda não desfeito, ou nil se não houver.
func (d *DB) LatestUndoableBatch() (*UndoableBatch, error) {
	var (
		b       UndoableBatch
		created string
	)
	err := d.conn.QueryRow(`SELECT id, action, summary, count, created_at FROM batches WHERE undone_at IS NULL AND book_id = ? ORDER BY id DESC LIMIT 1`, d.book).
		Scan(&b.ID, &b.Action, &b.Summary, &b.Count, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if b.CreatedAt, err = time.Parse(time.RFC3339, created); err != nil {
		return nil, err
	}
	return &b, nil
}

// ensureCategoryInBook recria a categoria no livro caso ela tenha sido excluída depois do lote, para que o
// lançamento restaurado nunca fique sem categoria. Nome legado inválido é ignorado em silêncio.
func ensureCategoryInBook(tx *sql.Tx, bookID int64, category string) error {
	clean, err := cleanCategoryName(category)
	if err != nil {
		return nil
	}
	_, err = tx.Exec(`INSERT OR IGNORE INTO categories (book_id, name, name_key) VALUES (?, ?, ?)`, bookID, clean, textutil.Fold(clean))
	return err
}

// UndoLatestBatch desfaz o lote mais recente ainda não desfeito, numa transação. Só restaura o que continua
// exatamente como o lote deixou (edição) ou o que ainda não existe (exclusão); o resto é pulado.
func (d *DB) UndoLatestBatch() (restored, skipped int, err error) {
	tx, err := d.conn.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var (
		id     int64
		action string
	)
	err = tx.QueryRow(`SELECT id, action FROM batches WHERE undone_at IS NULL AND book_id = ? ORDER BY id DESC LIMIT 1`, d.book).Scan(&id, &action)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, ErrNoBatch
	}
	if err != nil {
		return 0, 0, err
	}

	rows, err := tx.Query(`SELECT before_json, after_json FROM batch_items WHERE batch_id = ? ORDER BY tx_id`, id)
	if err != nil {
		return 0, 0, err
	}
	type stored struct {
		before snapshot
		after  *snapshot
	}
	var items []stored
	for rows.Next() {
		var beforeJSON string
		var afterJSON sql.NullString
		if err := rows.Scan(&beforeJSON, &afterJSON); err != nil {
			rows.Close()
			return 0, 0, err
		}
		var it stored
		if err := json.Unmarshal([]byte(beforeJSON), &it.before); err != nil {
			rows.Close()
			return 0, 0, err
		}
		if it.before.BookID == 0 { // lote antigo, sem o campo
			it.before.BookID = d.book
		}
		if afterJSON.Valid {
			var a snapshot
			if err := json.Unmarshal([]byte(afterJSON.String), &a); err != nil {
				rows.Close()
				return 0, 0, err
			}
			if a.BookID == 0 {
				a.BookID = d.book
			}
			it.after = &a
		}
		items = append(items, it)
	}
	rows.Close()

	for _, it := range items {
		cur, err := loadTransactions(tx, []int64{it.before.ID}, 0)
		if err != nil {
			return 0, 0, err
		}
		now, exists := cur[it.before.ID]

		if action == "edit" {
			if !exists || it.after == nil || !snapshotOf(now).sameState(*it.after) {
				skipped++
				continue
			}
			if err := ensureCategoryInBook(tx, it.before.BookID, it.before.Category); err != nil {
				return 0, 0, err
			}
			if _, err := tx.Exec(`UPDATE transactions SET description = ?, category = ?, date = ?, priority = ?, book_id = ? WHERE id = ?`,
				it.before.Description, it.before.Category, it.before.Date, it.before.Priority, it.before.BookID, it.before.ID); err != nil {
				return 0, 0, err
			}
			restored++
			continue
		}

		// exclusão: recria o lançamento, desde que o ID e o identificador de origem estejam livres
		if exists {
			skipped++
			continue
		}
		if it.before.ExternalID != "" {
			var n int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM transactions WHERE external_id = ?`, it.before.ExternalID).Scan(&n); err != nil {
				return 0, 0, err
			}
			if n > 0 {
				skipped++
				continue
			}
		}
		created, err := time.Parse(time.RFC3339Nano, it.before.CreatedAt)
		if err != nil {
			return 0, 0, fmt.Errorf("data de criação inválida no lote: %w", err)
		}
		if err := ensureCategoryInBook(tx, it.before.BookID, it.before.Category); err != nil {
			return 0, 0, err
		}
		if _, err := tx.Exec(`INSERT INTO transactions (`+loadColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			it.before.ID, it.before.Description, it.before.AmountCents, it.before.Type, it.before.Category,
			it.before.Date, it.before.Priority, it.before.ExternalID, created, it.before.BookID, it.before.OriginalDescription); err != nil {
			return 0, 0, err
		}
		restored++
	}

	if _, err := tx.Exec(`UPDATE batches SET undone_at = ? WHERE id = ?`, d.now().UTC().Format(time.RFC3339), id); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return restored, skipped, nil
}
