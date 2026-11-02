package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"finpi/internal/models"
)

// Banco no esquema v2 real (livros e categorias por livro, sem tabela accounts e sem a coluna account_id),
// com lançamentos do Mercado Pago e do Banco do Brasil.
func TestMigrateAccounts_FromRealV2Schema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
		CREATE TABLE transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			description TEXT NOT NULL,
			amount_cents INTEGER NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('income', 'expense')),
			category TEXT NOT NULL,
			date TEXT NOT NULL,
			priority TEXT NOT NULL DEFAULT '' CHECK(priority IN ('', 'essential', 'superfluous')),
			external_id TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			book_id INTEGER NOT NULL DEFAULT 1,
			original_description TEXT NOT NULL DEFAULT ''
		);
		CREATE TABLE books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			name_key TEXT NOT NULL UNIQUE,
			color TEXT NOT NULL DEFAULT 'emerald',
			archived INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			book_id INTEGER NOT NULL DEFAULT 1,
			name TEXT NOT NULL,
			name_key TEXT NOT NULL,
			hidden INTEGER NOT NULL DEFAULT 0,
			priority TEXT NOT NULL DEFAULT '' CHECK(priority IN ('', 'essential', 'superfluous')),
			UNIQUE(book_id, name_key)
		);
		INSERT INTO books (id, name, name_key) VALUES (1, 'Pessoal', 'pessoal'), (2, 'PJ', 'pj');
		INSERT INTO transactions (description, amount_cents, type, category, date, external_id, book_id) VALUES
			('Pix sintético', 1000, 'income', 'Outros', '2026-10-01', 'mp:1', 1),
			('Boleto sintético', 2000, 'expense', 'Outros', '2026-10-02', 'bb:1', 2),
			('Tarifa sintética', 300, 'expense', 'Outros', '2026-10-03', 'bb:2', 2),
			('Manual', 400, 'expense', 'Outros', '2026-10-04', '', 2);
		PRAGMA user_version = 2;
	`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()

	for pass := 0; pass < 2; pass++ {
		db, err := InitDB(path)
		if err != nil {
			t.Fatalf("InitDB (passo %d): %v", pass, err)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM accounts`); n != 2 {
			t.Errorf("passo %d: contas = %d, quer 2", pass, n)
		}
		mp := count(t, db, `SELECT id FROM accounts WHERE book_id = 1 AND name = 'Mercado Pago' AND bank = 'mercadopago'`)
		bb := count(t, db, `SELECT id FROM accounts WHERE book_id = 2 AND name = 'Banco do Brasil' AND bank = 'bb'`)
		if mp == 0 || bb == 0 {
			t.Fatalf("contas criadas: mp=%d bb=%d", mp, bb)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE external_id = 'mp:1' AND account_id = ?`, mp); n != 1 {
			t.Errorf("mp:1 sem a conta Mercado Pago")
		}
		if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE external_id LIKE 'bb:%' AND account_id = ?`, bb); n != 2 {
			t.Errorf("bb: atribuídos = %d, quer 2", n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE external_id = '' AND account_id = 0`); n != 1 {
			t.Errorf("manual deveria ficar sem conta")
		}
		if v := count(t, db, `PRAGMA user_version`); v != 3 {
			t.Errorf("user_version = %d, quer 3", v)
		}
		db.Close()
	}
}

// Uma conta "Nubank" sem banco já existe no livro: a migração cria "Nubank (2)" ligada ao banco.
func TestMigrateAccounts_ExistingNubankWithoutBank(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nu.db")
	db, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := db.CreateAccount("Nubank", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`INSERT INTO transactions (description, amount_cents, type, category, date, external_id, book_id)
		VALUES ('x', 100, 'expense', 'Outros', '2026-10-01', 'nu:1', 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`UPDATE transactions SET account_id = 0; PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	acc := count(t, db, `SELECT id FROM accounts WHERE book_id = 1 AND name = 'Nubank (2)' AND bank = 'nubank'`)
	if acc == 0 {
		t.Fatal("deveria criar Nubank (2) ligada ao banco")
	}
	if got := count(t, db, `SELECT account_id FROM transactions WHERE external_id = 'nu:1'`); got != acc || int64(got) == manual.ID {
		t.Errorf("nu:1 na conta %d, quer %d", got, acc)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM accounts WHERE id = ? AND bank = ''`, manual.ID); n != 1 {
		t.Error("a conta Nubank sem banco não deveria mudar")
	}
}

// PRAGMA user_version dentro de uma transação é desfeito no rollback (driver modernc): a marca da migração
// só vale se a atribuição for gravada.
func TestUserVersionPragma_IsTransactional(t *testing.T) {
	db := openTestDB(t)
	before := count(t, db, `PRAGMA user_version`)
	tx, err := db.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if v := count(t, db, `PRAGMA user_version`); v != before {
		t.Errorf("user_version = %d após rollback, quer %d", v, before)
	}
}

func TestMoveToBook_TwiceReusesBankAccountAndArchivedCreatesNew(t *testing.T) {
	pessoal, pj := twoBooks(t)
	a := importOne(t, pessoal, "nu:mv-a")
	b := importOne(t, pessoal, "nu:mv-b")
	if _, _, err := pessoal.MoveToBook([]int64{a}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pessoal.MoveToBook([]int64{b}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	accs, _ := pj.ListAccounts()
	if len(accs) != 1 || accs[0].Name != "Nubank" || accountOf(t, pj, a) != accs[0].ID || accountOf(t, pj, b) != accs[0].ID {
		t.Fatalf("duas movimentações deveriam usar a mesma Nubank do destino: %+v", accs)
	}

	// com a Nubank do destino arquivada, a próxima movimentação cria "Nubank (2)"
	if err := pj.SetAccountArchived(accs[0].ID, true); err != nil {
		t.Fatal(err)
	}
	c := importOne(t, pessoal, "nu:mv-c")
	if _, _, err := pessoal.MoveToBook([]int64{c}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	acc, err := pj.GetAccount(accountOf(t, pj, c))
	if err != nil || acc.Name != "Nubank (2)" || acc.Bank != models.BankNubank || acc.Archived {
		t.Errorf("conta do terceiro = %+v, %v", acc, err)
	}
}
