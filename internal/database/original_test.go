package database

import (
	"path/filepath"
	"testing"

	"finpi/internal/models"
)

func TestOriginalDescription_StoredReadAndUndoneWithDelete(t *testing.T) {
	db := openTestDB(t)
	row := models.Transaction{Description: "Pix recebido MARIA", OriginalDescription: "Transferência recebida pelo Pix - MARIA - doc", AmountCents: 1000,
		Type: "income", Category: "Outros", Date: "2026-10-05", ExternalID: "nu:u1"}
	if n, _, err := db.ImportTransactions([]models.Transaction{row}); err != nil || n != 1 {
		t.Fatalf("import = %d, %v", n, err)
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].OriginalDescription != row.OriginalDescription {
		t.Fatalf("GetTransactions = %+v", list)
	}
	if between, _ := db.GetTransactionsBetween("2026-01-01", "2026-12-31"); between[0].OriginalDescription != row.OriginalDescription {
		t.Error("GetTransactionsBetween não devolve o original")
	}

	// editar não mexe no original
	list[0].Description = "Outro texto"
	if err := db.UpdateTransaction(&list[0]); err != nil {
		t.Fatal(err)
	}
	if after, _ := db.GetTransactions("2026-10"); after[0].OriginalDescription != row.OriginalDescription {
		t.Errorf("UpdateTransaction apagou o original: %+v", after[0])
	}

	// excluir em lote e desfazer restaura o original
	if _, _, err := db.ApplyDeletes("Exclusão", []int64{list[0].ID}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.UndoLatestBatch(); err != nil {
		t.Fatal(err)
	}
	restored, _ := db.GetTransactionsByIDs([]int64{list[0].ID})
	if len(restored) != 1 || restored[0].OriginalDescription != row.OriginalDescription {
		t.Errorf("desfazer não restaurou o original: %+v", restored)
	}
}

func TestOriginalDescription_ManualTransactionsHaveNone(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "Mercado", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	if list, _ := db.GetTransactions("2026-10"); list[0].OriginalDescription != "" {
		t.Errorf("lançamento manual não tem original: %+v", list[0])
	}
}

func TestMigrate_AddsOriginalDescriptionToOldDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	writeOldDB(t, path) // banco anterior aos livros e ao original
	db, err := InitDB(path)
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()
	if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE original_description = ''`); n != 1 {
		t.Errorf("lançamento antigo deveria ter original vazio: %d", n)
	}
}
