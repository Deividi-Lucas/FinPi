package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"finpi/internal/models"
)

func newTx(t *testing.T, db *DB, desc, cat, date string) models.Transaction {
	t.Helper()
	return mustCreate(t, db, models.Transaction{Description: desc, AmountCents: 1000, Type: "expense", Category: cat, Date: date})
}

// toCategory devolve uma função de edição que só troca a categoria.
func toCategory(cat string) func(models.Transaction) (models.Transaction, error) {
	return func(t models.Transaction) (models.Transaction, error) { t.Category = cat; return t, nil }
}

func current(t *testing.T, db *DB, id int64) models.Transaction {
	t.Helper()
	got, err := db.GetTransactionsByIDs([]int64{id})
	if err != nil || len(got) != 1 {
		t.Fatalf("lançamento %d: %v %v", id, got, err)
	}
	return got[0]
}

func TestMigrate_BatchTablesAreCreatedAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.db")
	for i := 0; i < 2; i++ {
		db, err := InitDB(path)
		if err != nil {
			t.Fatalf("InitDB (vez %d): %v", i+1, err)
		}
		for _, table := range []string{"batches", "batch_items"} {
			cols, err := tableColumns(db.conn, table)
			if err != nil || len(cols) == 0 {
				t.Errorf("tabela %s ausente", table)
			}
		}
		db.Close()
	}
}

func TestGetTransactionsByIDs_OrderAndMissing(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	b := newTx(t, db, "B", "Outros", "2026-10-09")
	got, err := db.GetTransactionsByIDs([]int64{a.ID, 9999, b.ID})
	if err != nil || len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("got = %+v err %v (esperado B, depois A; inexistente ignorado)", got, err)
	}
	if got, err := db.GetTransactionsByIDs(nil); err != nil || len(got) != 0 {
		t.Errorf("sem ids: %v %v", got, err)
	}
}

func TestApplyEdits_UpdatesAndRecordsBatch(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	b := newTx(t, db, "B", "Outros", "2026-10-02")

	applied, skipped, err := db.ApplyEdits("categoria → Lazer", []int64{a.ID, b.ID, 9999}, toCategory("Lazer"))
	if err != nil || applied != 2 || skipped != 1 {
		t.Fatalf("applied=%d skipped=%d err=%v", applied, skipped, err)
	}
	if current(t, db, a.ID).Category != "Lazer" || current(t, db, b.ID).Category != "Lazer" {
		t.Error("as categorias deveriam ter mudado")
	}
	batch, err := db.LatestUndoableBatch()
	if err != nil || batch == nil || batch.Action != "edit" || batch.Count != 2 || batch.Summary != "categoria → Lazer" {
		t.Errorf("lote = %+v err %v", batch, err)
	}
}

func TestApplyEdits_UnchangedRowsAreSkippedAndNoBatchWhenNothingChanged(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Lazer", "2026-10-01")
	applied, skipped, err := db.ApplyEdits("x", []int64{a.ID}, toCategory("Lazer"))
	if err != nil || applied != 0 || skipped != 1 {
		t.Fatalf("applied=%d skipped=%d err=%v", applied, skipped, err)
	}
	if batch, _ := db.LatestUndoableBatch(); batch != nil {
		t.Error("sem mudança não deveria criar lote")
	}
}

func TestApplyEdits_IsAtomic(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	b := newTx(t, db, "B", "Outros", "2026-10-02")
	boom := errors.New("falhou no segundo")
	calls := 0
	_, _, err := db.ApplyEdits("x", []int64{a.ID, b.ID}, func(t models.Transaction) (models.Transaction, error) {
		calls++
		if calls == 2 {
			return t, boom
		}
		t.Category = "Lazer"
		return t, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if current(t, db, a.ID).Category != "Outros" {
		t.Error("o primeiro lançamento deveria ter sido desfeito (transação)")
	}
	if batch, _ := db.LatestUndoableBatch(); batch != nil {
		t.Error("nenhum lote deveria ter sido criado")
	}

	_, _, err = db.ApplyEdits("x", []int64{a.ID}, func(t models.Transaction) (models.Transaction, error) {
		t.Priority = "urgente"
		return t, nil
	})
	if !errors.Is(err, ErrInvalidPriority) {
		t.Errorf("prioridade inválida: %v", err)
	}
}

func TestApplyDeletes_RemovesAndRecordsBatch(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	b := newTx(t, db, "B", "Outros", "2026-10-02")
	applied, skipped, err := db.ApplyDeletes("Exclusão de 2 lançamentos", []int64{a.ID, b.ID, 9999})
	if err != nil || applied != 2 || skipped != 1 {
		t.Fatalf("applied=%d skipped=%d err=%v", applied, skipped, err)
	}
	if got, _ := db.GetTransactionsByIDs([]int64{a.ID, b.ID}); len(got) != 0 {
		t.Error("os lançamentos deveriam ter sido excluídos")
	}
	if batch, _ := db.LatestUndoableBatch(); batch == nil || batch.Action != "delete" || batch.Count != 2 {
		t.Errorf("lote = %+v", batch)
	}
}

func TestUndo_RestoresEditedTransactions(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	if _, _, err := db.ApplyEdits("x", []int64{a.ID}, toCategory("Lazer")); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := db.UndoLatestBatch()
	if err != nil || restored != 1 || skipped != 0 {
		t.Fatalf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
	if current(t, db, a.ID).Category != "Outros" {
		t.Error("a categoria deveria ter voltado")
	}
	if batch, _ := db.LatestUndoableBatch(); batch != nil {
		t.Error("o lote desfeito não pode ser desfeito de novo")
	}
}

func TestUndo_SkipsTransactionsChangedAfterTheBatch(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	b := newTx(t, db, "B", "Outros", "2026-10-02")
	if _, _, err := db.ApplyEdits("x", []int64{a.ID, b.ID}, toCategory("Lazer")); err != nil {
		t.Fatal(err)
	}
	edited := current(t, db, a.ID)
	edited.Description = "A editado depois"
	if err := db.UpdateTransaction(&edited); err != nil {
		t.Fatal(err)
	}

	restored, skipped, err := db.UndoLatestBatch()
	if err != nil || restored != 1 || skipped != 1 {
		t.Fatalf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
	if got := current(t, db, a.ID); got.Category != "Lazer" || got.Description != "A editado depois" {
		t.Errorf("o lançamento alterado depois deveria ficar como está: %+v", got)
	}
	if current(t, db, b.ID).Category != "Outros" {
		t.Error("o outro deveria ter sido restaurado")
	}
}

func TestUndo_SkipsWhenDeletedAfterTheEdit(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	if _, _, err := db.ApplyEdits("x", []int64{a.ID}, toCategory("Lazer")); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteTransaction(a.ID); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := db.UndoLatestBatch()
	if err != nil || restored != 0 || skipped != 1 {
		t.Errorf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
}

func TestUndo_RecreatesDeletedWithSameIDAndExternalID(t *testing.T) {
	db := openTestDB(t)
	row := models.Transaction{Description: "Importado", AmountCents: 777, Type: "income", Category: "Outros", Date: "2026-10-03", ExternalID: "mp:1|2026-10-03|777"}
	if _, _, err := db.ImportTransactions([]models.Transaction{row}); err != nil {
		t.Fatal(err)
	}
	list, _ := db.GetTransactions("2026-10")
	orig := list[0]

	if _, _, err := db.ApplyDeletes("x", []int64{orig.ID}); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := db.UndoLatestBatch()
	if err != nil || restored != 1 || skipped != 0 {
		t.Fatalf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
	back := current(t, db, orig.ID)
	if back.Description != "Importado" || back.AmountCents != 777 || back.Type != "income" || !back.CreatedAt.Equal(orig.CreatedAt) {
		t.Errorf("recriado = %+v, original = %+v", back, orig)
	}
	if ids, _ := db.ImportedExternalIDs(); !ids[row.ExternalID] {
		t.Error("o identificador de importação deveria ter voltado")
	}
}

func TestUndo_SkipsDeletedWhoseExternalIDWasReimported(t *testing.T) {
	db := openTestDB(t)
	row := models.Transaction{Description: "Importado", AmountCents: 777, Type: "income", Category: "Outros", Date: "2026-10-03", ExternalID: "mp:1|2026-10-03|777"}
	db.ImportTransactions([]models.Transaction{row})
	list, _ := db.GetTransactions("2026-10")
	if _, _, err := db.ApplyDeletes("x", []int64{list[0].ID}); err != nil {
		t.Fatal(err)
	}
	if n, _, err := db.ImportTransactions([]models.Transaction{row}); err != nil || n != 1 { // reimportou o extrato
		t.Fatalf("reimportação: %d %v", n, err)
	}
	restored, skipped, err := db.UndoLatestBatch()
	if err != nil || restored != 0 || skipped != 1 {
		t.Errorf("restored=%d skipped=%d err=%v", restored, skipped, err)
	}
}

func TestUndo_IsAStackAndErrorsWhenEmpty(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	db.ApplyEdits("primeiro", []int64{a.ID}, toCategory("Lazer"))
	db.ApplyEdits("segundo", []int64{a.ID}, toCategory("Moradia"))

	if batch, _ := db.LatestUndoableBatch(); batch.Summary != "segundo" {
		t.Errorf("o mais recente é o segundo: %+v", batch)
	}
	db.UndoLatestBatch()
	if got := current(t, db, a.ID).Category; got != "Lazer" {
		t.Errorf("depois de desfazer o segundo: %q", got)
	}
	if batch, _ := db.LatestUndoableBatch(); batch == nil || batch.Summary != "primeiro" {
		t.Errorf("agora o primeiro é desfazível: %+v", batch)
	}
	db.UndoLatestBatch()
	if got := current(t, db, a.ID).Category; got != "Outros" {
		t.Errorf("depois de desfazer o primeiro: %q", got)
	}
	if _, _, err := db.UndoLatestBatch(); !errors.Is(err, ErrNoBatch) {
		t.Errorf("sem lote: err = %v, quer ErrNoBatch", err)
	}
}

func TestBatches_KeepOnlyTheLast10(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	for i := 0; i < 12; i++ {
		cat := fmt.Sprintf("Cat%02d", i)
		if _, _, err := db.ApplyEdits(cat, []int64{a.ID}, toCategory(cat)); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	for {
		if _, _, err := db.UndoLatestBatch(); err != nil {
			if !errors.Is(err, ErrNoBatch) {
				t.Fatal(err)
			}
			break
		}
		n++
	}
	if n != 10 {
		t.Errorf("lotes desfazíveis = %d, quer 10", n)
	}
	var items int
	db.conn.QueryRow(`SELECT COUNT(*) FROM batch_items`).Scan(&items)
	if items != 10 {
		t.Errorf("itens guardados = %d, quer 10 (os lotes antigos são apagados com os itens)", items)
	}
}

func TestBatches_AreDroppedAfter30Days(t *testing.T) {
	db := openTestDB(t)
	a := newTx(t, db, "A", "Outros", "2026-10-01")
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	db.now = func() time.Time { return base }
	db.ApplyEdits("antigo", []int64{a.ID}, toCategory("Lazer"))
	db.now = func() time.Time { return base.Add(31 * 24 * time.Hour) }
	db.ApplyEdits("novo", []int64{a.ID}, toCategory("Moradia"))

	if _, _, err := db.UndoLatestBatch(); err != nil { // o novo
		t.Fatal(err)
	}
	if _, _, err := db.UndoLatestBatch(); !errors.Is(err, ErrNoBatch) {
		t.Errorf("o lote de 31 dias deveria ter sido descartado: %v", err)
	}
}
