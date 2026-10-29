package database

import (
	"errors"
	"slices"
	"testing"

	"finpi/internal/models"
)

func TestMoveToBook_MovesAndCreatesMissingCategories(t *testing.T) {
	pessoal, pj := twoBooks(t)
	if _, err := pj.CreateCategory("Mercado"); err != nil { // já existe no destino (outra grafia)
		t.Fatal(err)
	}
	a := mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 100, Type: "expense", Category: "mercado", Date: "2026-10-05"})
	b := mustCreate(t, pessoal, models.Transaction{Description: "B", AmountCents: 200, Type: "expense", Category: "Impostos", Date: "2026-10-06"})

	applied, skipped, err := pessoal.MoveToBook([]int64{a.ID, b.ID, 9999}, pj.BookID())
	if err != nil || applied != 2 || skipped != 1 {
		t.Fatalf("MoveToBook = %d, %d, %v", applied, skipped, err)
	}
	if list, _ := pessoal.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("origem ainda tem lançamentos: %+v", list)
	}
	list, _ := pj.GetTransactions("2026-10")
	if len(list) != 2 {
		t.Fatalf("destino tem %d lançamentos, quer 2", len(list))
	}
	byDesc := map[string]string{}
	for _, tx := range list {
		byDesc[tx.Description] = tx.Category
	}
	if byDesc["A"] != "Mercado" {
		t.Errorf("categoria existente (outra grafia) deveria ser reaproveitada: %q", byDesc["A"])
	}
	if byDesc["B"] != "Impostos" || !slices.Contains(categoryNames(t, pj), "Impostos") {
		t.Errorf("categoria inexistente deveria ser criada no destino: %q %v", byDesc["B"], categoryNames(t, pj))
	}
}

func TestMoveToBook_RejectsBadDestination(t *testing.T) {
	pessoal, pj := twoBooks(t)
	tx := mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	if _, _, err := pessoal.MoveToBook([]int64{tx.ID}, pessoal.BookID()); !errors.Is(err, ErrSameBook) {
		t.Errorf("mesmo livro: %v", err)
	}
	if _, _, err := pessoal.MoveToBook([]int64{tx.ID}, 999); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("destino inexistente: %v", err)
	}
	if err := pessoal.SetBookArchived(pj.BookID(), true); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pessoal.MoveToBook([]int64{tx.ID}, pj.BookID()); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("destino arquivado: %v", err)
	}
	if list, _ := pessoal.GetTransactions("2026-10"); len(list) != 1 {
		t.Error("nada deveria ter sido movido")
	}
}

func TestMoveToBook_CanBeUndoneFromTheOriginBook(t *testing.T) {
	pessoal, pj := twoBooks(t)
	tx := mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 100, Type: "expense", Category: "Impostos", Date: "2026-10-05"})
	if _, _, err := pessoal.MoveToBook([]int64{tx.ID}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	if b, _ := pj.LatestUndoableBatch(); b != nil {
		t.Error("o desfazer pertence ao livro de origem, não ao destino")
	}
	b, _ := pessoal.LatestUndoableBatch()
	if b == nil || b.Count != 1 {
		t.Fatalf("lote da movimentação ausente: %+v", b)
	}
	restored, skipped, err := pessoal.UndoLatestBatch()
	if err != nil || restored != 1 || skipped != 0 {
		t.Fatalf("UndoLatestBatch = %d, %d, %v", restored, skipped, err)
	}
	if list, _ := pessoal.GetTransactions("2026-10"); len(list) != 1 || list[0].Category != "Impostos" {
		t.Errorf("lançamento não voltou ao Pessoal: %+v", list)
	}
	if list, _ := pj.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("destino ainda tem o lançamento: %+v", list)
	}
}

func TestMoveToBook_UndoSkipsWhatChangedAfterwards(t *testing.T) {
	pessoal, pj := twoBooks(t)
	tx := mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	if _, _, err := pessoal.MoveToBook([]int64{tx.ID}, pj.BookID()); err != nil {
		t.Fatal(err)
	}
	moved, _ := pj.GetTransactions("2026-10")
	moved[0].Description = "Editado depois"
	if err := pj.UpdateTransaction(&moved[0]); err != nil {
		t.Fatal(err)
	}
	restored, skipped, err := pessoal.UndoLatestBatch()
	if err != nil || restored != 0 || skipped != 1 {
		t.Errorf("UndoLatestBatch = %d, %d, %v (quer 0 restaurado e 1 pulado)", restored, skipped, err)
	}
}
