package database

import (
	"testing"

	"finpi/internal/models"
)

// stripAccountFromBatches simula lotes gravados antes das contas (snapshot sem account_id).
func stripAccountFromBatches(t *testing.T, db *DB) {
	t.Helper()
	if _, err := db.conn.Exec(`UPDATE batch_items SET before_json = json_remove(before_json, '$.account_id'),
		after_json = CASE WHEN after_json IS NULL THEN NULL ELSE json_remove(after_json, '$.account_id') END`); err != nil {
		t.Fatal(err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM batch_items WHERE before_json LIKE '%account_id%' OR after_json LIKE '%account_id%'`); n != 0 {
		t.Fatalf("snapshot ainda tem account_id: %d", n)
	}
}

func accountOf(t *testing.T, db *DB, id int64) int64 {
	t.Helper()
	return int64(count(t, db, `SELECT account_id FROM transactions WHERE id = ?`, id))
}

func importOne(t *testing.T, db *DB, ext string) int64 {
	t.Helper()
	rows := []models.Transaction{{Description: "Compra sintética", AmountCents: 1500, Type: "expense", Category: "Outros", Date: "2026-10-05", ExternalID: ext}}
	if _, _, err := db.ImportTransactionsToAccount(rows, ImportAccount{Bank: models.BankFromExternalID(ext)}); err != nil {
		t.Fatal(err)
	}
	return int64(count(t, db, `SELECT id FROM transactions WHERE external_id = ?`, ext))
}

func TestUndo_LegacyMoveSnapshotGetsOriginBankAccount(t *testing.T) {
	pessoal, pj := twoBooks(t)
	id := importOne(t, pessoal, "nu:legado-1")
	manual := mustCreate(t, pessoal, models.Transaction{Description: "Manual", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	origin, ok, err := pessoal.AccountForBank(models.BankNubank)
	if err != nil || !ok {
		t.Fatalf("conta Nubank de origem: %v %v", ok, err)
	}
	if _, _, err := pessoal.MoveToBook([]int64{id, manual.ID}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	stripAccountFromBatches(t, pessoal)

	restored, skipped, err := pessoal.UndoLatestBatch()
	if err != nil || restored != 2 || skipped != 0 {
		t.Fatalf("UndoLatestBatch = %d, %d, %v", restored, skipped, err)
	}
	if got := accountOf(t, pessoal, id); got != origin.ID {
		t.Errorf("conta após desfazer = %d, quer a Nubank do livro de origem (%d)", got, origin.ID)
	}
	if got := accountOf(t, pessoal, manual.ID); got != 0 {
		t.Errorf("manual deveria ficar sem conta: %d", got)
	}
	// a edição volta a funcionar: a conta é do livro
	tx := current(t, pessoal, id)
	tx.Description = "Editado"
	if err := pessoal.UpdateTransaction(&tx); err != nil {
		t.Errorf("edição após desfazer: %v", err)
	}
}

func TestUndo_LegacyMoveSnapshotCreatesMissingOriginAccount(t *testing.T) {
	pessoal, pj := twoBooks(t)
	id := importOne(t, pessoal, "nu:legado-2")
	if _, _, err := pessoal.MoveToBook([]int64{id}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	origin, _, _ := pessoal.AccountForBank(models.BankNubank)
	if err := pessoal.SetAccountArchived(origin.ID, true); err != nil {
		t.Fatal(err)
	}
	stripAccountFromBatches(t, pessoal)
	if _, _, err := pessoal.UndoLatestBatch(); err != nil {
		t.Fatal(err)
	}
	got := accountOf(t, pessoal, id)
	acc, err := pessoal.GetAccount(got)
	if err != nil || acc.Bank != models.BankNubank || acc.Archived || acc.Name != "Nubank (2)" {
		t.Errorf("conta após desfazer = %+v, %v (quer uma Nubank ativa nova no livro de origem)", acc, err)
	}
}

func TestUndo_LegacySameBookEditKeepsAccount(t *testing.T) {
	db := openTestDB(t)
	id := importOne(t, db, "nu:legado-3")
	acc := accountOf(t, db, id)
	if _, _, err := db.ApplyEdits("lote", []int64{id}, toCategory("Lazer")); err != nil {
		t.Fatal(err)
	}
	stripAccountFromBatches(t, db)
	if _, _, err := db.UndoLatestBatch(); err != nil {
		t.Fatal(err)
	}
	if got := accountOf(t, db, id); got != acc {
		t.Errorf("conta = %d, quer %d (mesmo livro: mantém)", got, acc)
	}
}

func TestUndo_LegacyDeleteSnapshotRestoresBankAccount(t *testing.T) {
	db := openTestDB(t)
	id := importOne(t, db, "sic:legado-4")
	acc := accountOf(t, db, id)
	manual := mustCreate(t, db, models.Transaction{Description: "Manual", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	if _, _, err := db.ApplyDeletes("excluir", []int64{id, manual.ID}); err != nil {
		t.Fatal(err)
	}
	stripAccountFromBatches(t, db)
	restored, _, err := db.UndoLatestBatch()
	if err != nil || restored != 2 {
		t.Fatalf("UndoLatestBatch = %d, %v", restored, err)
	}
	if got := accountOf(t, db, id); got != acc || acc == 0 {
		t.Errorf("importado restaurado com conta %d, quer %d", got, acc)
	}
	if got := accountOf(t, db, manual.ID); got != 0 {
		t.Errorf("manual restaurado com conta %d, quer 0", got)
	}
}
