package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// writeOldDB cria um banco no formato anterior aos livros (categorias com name_key UNIQUE).
func writeOldDB(t *testing.T, path string) {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	stmts := []string{
		`CREATE TABLE transactions (id INTEGER PRIMARY KEY AUTOINCREMENT, description TEXT NOT NULL, amount_cents INTEGER NOT NULL,
			type TEXT NOT NULL, category TEXT NOT NULL, date TEXT NOT NULL, priority TEXT NOT NULL DEFAULT '',
			external_id TEXT NOT NULL DEFAULT '', created_at DATETIME DEFAULT CURRENT_TIMESTAMP)`,
		`CREATE TABLE categories (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, name_key TEXT NOT NULL UNIQUE,
			hidden INTEGER NOT NULL DEFAULT 0, priority TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE batches (id INTEGER PRIMARY KEY AUTOINCREMENT, created_at TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('edit', 'delete')), summary TEXT NOT NULL, count INTEGER NOT NULL, undone_at TEXT)`,
		`CREATE TABLE batch_items (batch_id INTEGER NOT NULL, tx_id INTEGER NOT NULL, before_json TEXT NOT NULL, after_json TEXT,
			PRIMARY KEY (batch_id, tx_id))`,
		`INSERT INTO categories (name, name_key, hidden, priority) VALUES ('Mercado', 'mercado', 0, 'essential')`,
		`INSERT INTO categories (name, name_key, hidden, priority) VALUES ('Antiga', 'antiga', 1, '')`,
		`INSERT INTO transactions (description, amount_cents, type, category, date) VALUES ('Compra', 1000, 'expense', 'Mercado', '2026-10-05')`,
		`INSERT INTO batches (created_at, action, summary, count) VALUES ('2026-10-06T10:00:00Z', 'edit', 'Edição', 1)`,
		`PRAGMA user_version = 1`,
	}
	for _, s := range stmts {
		if _, err := conn.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func count(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func TestMigrate_NewDatabaseHasPessoalBook(t *testing.T) {
	db := openTestDB(t)
	if n := count(t, db, `SELECT COUNT(*) FROM books WHERE id = 1 AND name = 'Pessoal' AND archived = 0`); n != 1 {
		t.Errorf("livro Pessoal ausente: %d", n)
	}
	if db.BookID() != DefaultBookID {
		t.Errorf("BookID = %d, quer %d", db.BookID(), DefaultBookID)
	}
}

func TestMigrate_OldDatabaseKeepsEverythingInPessoal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	writeOldDB(t, path)

	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	if n := count(t, db, `SELECT COUNT(*) FROM categories WHERE book_id = 1`); n != 2 {
		t.Errorf("categorias no livro 1: %d, quer 2", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM categories WHERE name = 'Mercado' AND priority = 'essential' AND hidden = 0`); n != 1 {
		t.Error("prioridade ou visibilidade da categoria se perdeu")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM categories WHERE name = 'Antiga' AND hidden = 1`); n != 1 {
		t.Error("categoria oculta deixou de ser oculta")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE book_id = 1`); n != 1 {
		t.Errorf("lançamentos no livro 1: %d, quer 1", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM batches WHERE book_id = 1`); n != 1 {
		t.Errorf("lotes no livro 1: %d, quer 1", n)
	}
	// o nome da categoria agora é único por livro, não no banco todo
	if _, err := db.conn.Exec(`INSERT INTO categories (book_id, name, name_key) VALUES (2, 'Mercado', 'mercado')`); err != nil {
		t.Errorf("mesmo nome em outro livro deveria valer: %v", err)
	}
	if _, err := db.conn.Exec(`INSERT INTO categories (book_id, name, name_key) VALUES (1, 'Mercado', 'mercado')`); err == nil {
		t.Error("nome repetido no mesmo livro deveria falhar")
	}
}

func TestMigrate_IsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	writeOldDB(t, path)
	for i := 0; i < 3; i++ {
		db, err := InitDB(path)
		if err != nil {
			t.Fatalf("InitDB #%d: %v", i, err)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM categories`); n != 2 {
			t.Errorf("rodada %d: %d categorias, quer 2", i, n)
		}
		db.Close()
	}
}
