package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"finpi/internal/models"
)

// Banco como era antes do dashboard: sem a coluna priority em nenhuma tabela.
func createLegacyDB(t *testing.T, path string) {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, stmt := range []string{
		`CREATE TABLE transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			description TEXT NOT NULL,
			amount_cents INTEGER NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('income', 'expense')),
			category TEXT NOT NULL,
			date TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE categories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			name_key TEXT NOT NULL UNIQUE,
			hidden INTEGER NOT NULL DEFAULT 0
		)`,
		`INSERT INTO transactions (description, amount_cents, type, category, date) VALUES ('Mercado', 1999, 'expense', 'Alimentação', '2026-10-05')`,
		`INSERT INTO categories (name, name_key) VALUES ('Alimentação', 'alimentacao')`,
		`PRAGMA user_version = 1`,
	} {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
}

func TestMigrate_AddsPriorityColumnsToLegacyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	createLegacyDB(t, path)

	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	for _, table := range []string{"transactions", "categories"} {
		cols, err := tableColumns(db.conn, table)
		if err != nil {
			t.Fatal(err)
		}
		if !cols["priority"] {
			t.Errorf("tabela %s sem coluna priority", table)
		}
	}
	list, err := db.GetTransactions("2026-10")
	if err != nil || len(list) != 1 {
		t.Fatalf("GetTransactions = %v, %v", list, err)
	}
	if list[0].Priority != "" || list[0].AmountCents != 1999 {
		t.Errorf("lançamento antigo alterado: %+v", list[0])
	}
	if c := findCategory(t, db, "Alimentação"); c.Priority != "" {
		t.Errorf("categoria antiga deveria começar sem classificação, veio %q", c.Priority)
	}
}

func TestMigrate_PriorityColumnsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twice.db")
	createLegacyDB(t, path)
	for i := 0; i < 2; i++ {
		db, err := InitDB(path)
		if err != nil {
			t.Fatalf("InitDB (vez %d): %v", i+1, err)
		}
		db.Close()
	}
}

func TestMigrate_NewDatabaseHasPriorityColumns(t *testing.T) {
	db := openTestDB(t)
	for _, table := range []string{"transactions", "categories"} {
		cols, err := tableColumns(db.conn, table)
		if err != nil {
			t.Fatal(err)
		}
		if !cols["priority"] {
			t.Errorf("tabela %s sem coluna priority", table)
		}
	}
}

func TestTransactionPriority_RoundTrip(t *testing.T) {
	db := openTestDB(t)
	tx := mustCreate(t, db, models.Transaction{Description: "Geladeira 1/3", AmountCents: 5000, Type: "expense", Category: "Casa", Date: "2026-10-05", Priority: models.PriorityEssential})

	list, err := db.GetTransactions("2026-10")
	if err != nil || len(list) != 1 || list[0].Priority != models.PriorityEssential {
		t.Fatalf("GetTransactions = %+v, %v", list, err)
	}

	cands, err := db.GetInstallmentCandidates()
	if err != nil || len(cands) != 1 || cands[0].Priority != models.PriorityEssential {
		t.Fatalf("GetInstallmentCandidates = %+v, %v", cands, err)
	}

	tx.Priority = models.PrioritySuperfluous
	if err := db.UpdateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	list, _ = db.GetTransactions("2026-10")
	if list[0].Priority != models.PrioritySuperfluous {
		t.Errorf("após atualizar: %q", list[0].Priority)
	}

	tx.Priority = ""
	if err := db.UpdateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	list, _ = db.GetTransactions("2026-10")
	if list[0].Priority != "" {
		t.Errorf("após limpar: %q", list[0].Priority)
	}
}

func TestTransactionPriority_InvalidIsRejected(t *testing.T) {
	db := openTestDB(t)
	tx := models.Transaction{Description: "X", AmountCents: 100, Type: "expense", Category: "Casa", Date: "2026-10-05", Priority: "urgente"}
	if err := db.CreateTransaction(&tx); !errors.Is(err, ErrInvalidPriority) {
		t.Errorf("CreateTransaction err = %v, quer ErrInvalidPriority", err)
	}
	tx.Priority = ""
	tx = mustCreate(t, db, tx)
	tx.Priority = "urgente"
	if err := db.UpdateTransaction(&tx); !errors.Is(err, ErrInvalidPriority) {
		t.Errorf("UpdateTransaction err = %v, quer ErrInvalidPriority", err)
	}
}

func TestSetCategoryPriority(t *testing.T) {
	db := openTestDB(t)
	c := findCategory(t, db, "Lazer")

	if err := db.SetCategoryPriority(c.ID, models.PrioritySuperfluous); err != nil {
		t.Fatal(err)
	}
	if got := findCategory(t, db, "Lazer").Priority; got != models.PrioritySuperfluous {
		t.Errorf("priority = %q", got)
	}
	if err := db.SetCategoryPriority(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got := findCategory(t, db, "Lazer").Priority; got != "" {
		t.Errorf("após limpar = %q", got)
	}
	if err := db.SetCategoryPriority(c.ID, "urgente"); !errors.Is(err, ErrInvalidPriority) {
		t.Errorf("inválida: err = %v", err)
	}
	if err := db.SetCategoryPriority(99999, models.PriorityEssential); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("inexistente: err = %v", err)
	}
}

func TestRenameCategory_KeepsPriority(t *testing.T) {
	db := openTestDB(t)
	c := findCategory(t, db, "Lazer")
	if err := db.SetCategoryPriority(c.ID, models.PrioritySuperfluous); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameCategory(c.ID, "Diversão"); err != nil {
		t.Fatal(err)
	}
	if got := findCategory(t, db, "Diversão").Priority; got != models.PrioritySuperfluous {
		t.Errorf("prioridade perdida ao renomear: %q", got)
	}
}

func TestGetOrCreateCategory_ReturnsPriority(t *testing.T) {
	db := openTestDB(t)
	c := findCategory(t, db, "Saúde")
	if err := db.SetCategoryPriority(c.ID, models.PriorityEssential); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetOrCreateCategory("saude")
	if err != nil || got.Priority != models.PriorityEssential {
		t.Errorf("GetOrCreateCategory = %+v, %v", got, err)
	}
}
