package database

import "testing"

func TestMigrate_MovesNubankRDBToInternalOnce(t *testing.T) {
	db := openTestDB(t)
	ins := func(desc, ext, cat string, book int64) {
		t.Helper()
		if _, err := db.conn.Exec(`INSERT INTO transactions (description, amount_cents, type, category, date, external_id, book_id)
			VALUES (?, 100, 'income', ?, '2026-10-01', ?, ?)`, desc, cat, ext, book); err != nil {
			t.Fatal(err)
		}
	}
	ins("Resgate RDB", "nu:a", "Outros", 1)
	ins("Aplicação RDB", "nu:b", "Outros", 1)
	ins("Resgate RDB", "bb:c", "Outros", 1)     // outro banco: não mexe
	ins("Pix recebido Ana", "nu:d", "Outros", 1) // não é RDB
	ins("Resgate RDB", "", "Outros", 1)          // lançado à mão: não mexe

	if _, err := db.conn.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db.conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE category = 'Movimentação interna'`); n != 2 {
		t.Errorf("internas = %d, quer 2", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM categories WHERE book_id = 1 AND name = 'Movimentação interna'`); n != 1 {
		t.Errorf("categoria não criada no livro: %d", n)
	}

	// Recategorizar depois não é desfeito por uma nova abertura.
	if _, err := db.conn.Exec(`UPDATE transactions SET category = 'Outros' WHERE external_id = 'nu:a'`); err != nil {
		t.Fatal(err)
	}
	if err := migrate(db.conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE category = 'Movimentação interna'`); n != 1 {
		t.Errorf("a migração rodou de novo: %d", n)
	}
}
