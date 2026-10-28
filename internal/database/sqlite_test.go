package database

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"finpi/internal/models"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := InitDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustCreate(t *testing.T, db *DB, tx models.Transaction) models.Transaction {
	t.Helper()
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatalf("CreateTransaction: %v", err)
	}
	return tx
}

func TestCreateAndGetTransactions_StoresCents(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "Mercado", AmountCents: 1999, Type: "expense", Category: "Alimentação", Date: "2026-10-05"})

	list, err := db.GetTransactions("2026-10")
	if err != nil {
		t.Fatalf("GetTransactions: %v", err)
	}
	if len(list) != 1 || list[0].AmountCents != 1999 {
		t.Fatalf("esperava 1 lançamento de 1999 centavos, veio %+v", list)
	}
}

func TestGetSummaryByMonth_IsExactInCents(t *testing.T) {
	db := openTestDB(t)
	// 10 receitas de R$ 0,10 somariam 0.9999999999999999 em float64.
	for i := 0; i < 10; i++ {
		mustCreate(t, db, models.Transaction{Description: "Rendimento", AmountCents: 10, Type: "income", Category: "Renda", Date: "2026-10-01"})
	}
	mustCreate(t, db, models.Transaction{Description: "Conta", AmountCents: 30, Type: "expense", Category: "Casa", Date: "2026-10-02"})
	mustCreate(t, db, models.Transaction{Description: "Outro mês", AmountCents: 5000, Type: "expense", Category: "Casa", Date: "2026-09-02"})

	s, err := db.GetSummaryByMonth("2026-10")
	if err != nil {
		t.Fatalf("GetSummaryByMonth: %v", err)
	}
	if s.TotalIncome != 100 || s.TotalExpense != 30 || s.Balance != 70 {
		t.Errorf("resumo = %+v, quer receita 100, despesa 30, saldo 70", s)
	}
}

func TestUpdateTransaction(t *testing.T) {
	db := openTestDB(t)
	tx := mustCreate(t, db, models.Transaction{Description: "Errado", AmountCents: 100, Type: "expense", Category: "X", Date: "2026-10-05"})

	tx.AmountCents = 250
	tx.Description = "Certo"
	if err := db.UpdateTransaction(&tx); err != nil {
		t.Fatalf("UpdateTransaction: %v", err)
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].AmountCents != 250 || list[0].Description != "Certo" {
		t.Errorf("atualização não refletida: %+v", list)
	}
}

func TestUpdateTransaction_NotFound(t *testing.T) {
	db := openTestDB(t)
	err := db.UpdateTransaction(&models.Transaction{ID: 999, Description: "x", AmountCents: 1, Type: "income", Category: "x", Date: "2026-10-05"})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("quer ErrNotFound, veio %v", err)
	}
}

func TestDeleteTransaction(t *testing.T) {
	db := openTestDB(t)
	tx := mustCreate(t, db, models.Transaction{Description: "Apagar", AmountCents: 100, Type: "expense", Category: "X", Date: "2026-10-05"})

	if err := db.DeleteTransaction(tx.ID); err != nil {
		t.Fatalf("DeleteTransaction: %v", err)
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("lançamento não foi removido: %+v", list)
	}
}

func TestDeleteTransaction_NotFound(t *testing.T) {
	db := openTestDB(t)
	if err := db.DeleteTransaction(999); !errors.Is(err, ErrNotFound) {
		t.Errorf("quer ErrNotFound, veio %v", err)
	}
}

// Banco criado pela versão antiga (coluna amount REAL) deve ser convertido para centavos
// sem perder dados, e reabrir o banco não pode converter de novo.
func TestInitDB_MigratesLegacyRealAmounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
		CREATE TABLE transactions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			description TEXT NOT NULL,
			amount REAL NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('income', 'expense')),
			category TEXT NOT NULL,
			date TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		INSERT INTO transactions (description, amount, type, category, date) VALUES
			('Salário', 5000.00, 'income', 'Renda', '2026-10-01'),
			('Soma float', 0.30000000000000004, 'expense', 'X', '2026-10-02'),
			('Café', 12.34, 'expense', 'Alimentação', '2026-10-03');
	`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	for round := 1; round <= 2; round++ {
		db, err := InitDB(path)
		if err != nil {
			t.Fatalf("InitDB (rodada %d): %v", round, err)
		}
		list, err := db.GetTransactions("2026-10")
		if err != nil {
			t.Fatalf("GetTransactions (rodada %d): %v", round, err)
		}
		got := map[string]int64{}
		for _, tx := range list {
			got[tx.Description] = tx.AmountCents
		}
		want := map[string]int64{"Salário": 500000, "Soma float": 30, "Café": 1234}
		for desc, cents := range want {
			if got[desc] != cents {
				t.Errorf("rodada %d: %s = %d centavos, quer %d", round, desc, got[desc], cents)
			}
		}
		db.Close()
	}
}

func TestGetInstallmentCandidates_FiltersByMarker(t *testing.T) {
	db := openTestDB(t)
	for _, desc := range []string{"Geladeira 3/10", "Sofá Parcela 2 de 5", "Mercado"} {
		mustCreate(t, db, models.Transaction{Description: desc, AmountCents: 100, Type: "expense", Category: "Casa", Date: "2026-10-05"})
	}

	list, err := db.GetInstallmentCandidates()
	if err != nil {
		t.Fatalf("GetInstallmentCandidates: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("esperava 2 candidatos, veio %d: %+v", len(list), list)
	}
	for _, tx := range list {
		if tx.Description == "Mercado" {
			t.Errorf("lançamento comum não deveria ser candidato: %+v", tx)
		}
	}
}
