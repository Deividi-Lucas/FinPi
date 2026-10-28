package database

import (
	"path/filepath"
	"testing"

	"finpi/internal/models"
	"finpi/internal/textutil"
)

func importRow(ref, desc string, cents int64, typ, category, date string) models.Transaction {
	return models.Transaction{
		Description: desc, AmountCents: cents, Type: typ, Category: category, Date: date,
		ExternalID: "mp:" + ref + "|" + date + "|" + "1",
	}
}

func TestMigrate_AddsExternalIDColumnAndUniqueIndex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	createLegacyDB(t, path) // sem priority e sem external_id

	for i := 0; i < 2; i++ { // a segunda vez prova a idempotência
		db, err := InitDB(path)
		if err != nil {
			t.Fatalf("InitDB (vez %d): %v", i+1, err)
		}
		cols, err := tableColumns(db.conn, "transactions")
		if err != nil || !cols["external_id"] {
			t.Fatalf("sem coluna external_id: %v %v", cols, err)
		}
		var n int
		if err := db.conn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_transactions_external_id'`).Scan(&n); err != nil || n != 1 {
			t.Errorf("índice único ausente: n=%d err=%v", n, err)
		}
		db.Close()
	}
}

func TestImportTransactions_InsertsAndSkipsRepeated(t *testing.T) {
	db := openTestDB(t)
	rows := []models.Transaction{
		importRow("1", "Rendimentos", 122, "income", "Investimentos", "2026-08-03"),
		importRow("2", "Pix enviado X", 2000, "expense", "Outros", "2026-08-05"),
	}
	inserted, skipped, err := db.ImportTransactions(rows)
	if err != nil || inserted != 2 || skipped != 0 {
		t.Fatalf("1ª importação: %d/%d %v", inserted, skipped, err)
	}

	inserted, skipped, err = db.ImportTransactions(rows)
	if err != nil || inserted != 0 || skipped != 2 {
		t.Fatalf("reimportação: %d/%d %v", inserted, skipped, err)
	}

	list, _ := db.GetTransactions("2026-08")
	if len(list) != 2 {
		t.Errorf("lançamentos = %d, quer 2", len(list))
	}
}

func TestImportTransactions_PartialOverlap(t *testing.T) {
	db := openTestDB(t)
	a := importRow("1", "A", 100, "income", "Outros", "2026-08-03")
	b := importRow("2", "B", 200, "expense", "Outros", "2026-08-03")
	if _, _, err := db.ImportTransactions([]models.Transaction{a}); err != nil {
		t.Fatal(err)
	}
	inserted, skipped, err := db.ImportTransactions([]models.Transaction{a, b})
	if err != nil || inserted != 1 || skipped != 1 {
		t.Errorf("%d/%d %v", inserted, skipped, err)
	}
}

func TestImportTransactions_IsAtomic(t *testing.T) {
	db := openTestDB(t)
	good := importRow("1", "A", 100, "income", "Outros", "2026-08-03")
	bad := importRow("2", "B", 200, "bogus", "Outros", "2026-08-03") // viola o CHECK do tipo
	if _, _, err := db.ImportTransactions([]models.Transaction{good, bad}); err == nil {
		t.Fatal("tipo inválido deveria falhar")
	}
	if list, _ := db.GetTransactions("2026-08"); len(list) != 0 {
		t.Errorf("a transação deveria ter sido desfeita: %+v", list)
	}
}

func TestImportTransactions_RequiresExternalID(t *testing.T) {
	db := openTestDB(t)
	row := importRow("1", "A", 100, "income", "Outros", "2026-08-03")
	row.ExternalID = ""
	if _, _, err := db.ImportTransactions([]models.Transaction{row}); err == nil {
		t.Error("external_id vazio deveria ser recusado")
	}
}

func TestImportedExternalIDs(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "Manual", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-08-01"})
	row := importRow("1", "A", 100, "income", "Outros", "2026-08-03")
	if _, _, err := db.ImportTransactions([]models.Transaction{row}); err != nil {
		t.Fatal(err)
	}
	ids, err := db.ImportedExternalIDs()
	if err != nil || len(ids) != 1 || !ids[row.ExternalID] {
		t.Errorf("ids = %v, err = %v", ids, err)
	}
}

func TestManualTransactionKeys_OnlyManualInRange(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "Dentro", AmountCents: 2000, Type: "expense", Category: "Outros", Date: "2026-08-05"})
	mustCreate(t, db, models.Transaction{Description: "Fora", AmountCents: 3000, Type: "expense", Category: "Outros", Date: "2026-09-05"})
	if _, _, err := db.ImportTransactions([]models.Transaction{importRow("1", "Importado", 4000, "expense", "Outros", "2026-08-05")}); err != nil {
		t.Fatal(err)
	}

	keys, err := db.ManualTransactionKeys("2026-08-01", "2026-08-31")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || !keys[models.TransactionKey("2026-08-05", 2000, "expense")] {
		t.Errorf("chaves = %v", keys)
	}
}

func TestLatestCategoryByDescription(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "Padaria Central", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-08-01"})
	mustCreate(t, db, models.Transaction{Description: "padaria  central", AmountCents: 100, Type: "expense", Category: "Alimentação", Date: "2026-08-10"})
	mustCreate(t, db, models.Transaction{Description: "Padaria Central", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-08-05"})

	got, err := db.LatestCategoryByDescription()
	if err != nil {
		t.Fatal(err)
	}
	if c := got[textutil.Fold("Padaria Central")]; c != "Alimentação" {
		t.Errorf("categoria = %q, quer Alimentação (a mais recente)", c)
	}
}

func TestEditAndDelete_KeepExternalIDSemantics(t *testing.T) {
	db := openTestDB(t)
	row := importRow("1", "A", 100, "expense", "Outros", "2026-08-03")
	if _, _, err := db.ImportTransactions([]models.Transaction{row}); err != nil {
		t.Fatal(err)
	}
	list, _ := db.GetTransactions("2026-08")
	tx := list[0]
	tx.Description = "A editado"
	if err := db.UpdateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	if ids, _ := db.ImportedExternalIDs(); !ids[row.ExternalID] {
		t.Error("editar deveria manter o external_id")
	}

	if err := db.DeleteTransaction(tx.ID); err != nil {
		t.Fatal(err)
	}
	if ids, _ := db.ImportedExternalIDs(); ids[row.ExternalID] {
		t.Error("excluir deveria liberar o external_id")
	}
	if inserted, _, err := db.ImportTransactions([]models.Transaction{row}); err != nil || inserted != 1 {
		t.Errorf("reimportar após excluir: %d %v", inserted, err)
	}
}
