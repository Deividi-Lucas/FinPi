package database

import (
	"errors"
	"testing"

	"finpi/internal/models"
)

// twoBooks devolve o acesso ao Pessoal e ao PJ, para testar o isolamento.
func twoBooks(t *testing.T) (pessoal, pj *DB) {
	t.Helper()
	db := openTestDB(t)
	b, err := db.CreateBook("PJ", "sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	return db, db.ForBook(b.ID)
}

func TestTransactions_AreIsolatedByBook(t *testing.T) {
	pessoal, pj := twoBooks(t)
	mustCreate(t, pessoal, models.Transaction{Description: "Mercado 1/3", AmountCents: 1000, Type: "expense", Category: "Mercado", Date: "2026-10-05"})
	own := mustCreate(t, pj, models.Transaction{Description: "DAS 1/3", AmountCents: 8705, Type: "expense", Category: "Impostos", Date: "2026-10-06"})

	for name, db := range map[string]*DB{"pessoal": pessoal, "pj": pj} {
		list, err := db.GetTransactions("2026-10")
		if err != nil || len(list) != 1 {
			t.Fatalf("%s: GetTransactions = %+v, %v", name, list, err)
		}
		between, _ := db.GetTransactionsBetween("2026-01-01", "2026-12-31")
		if len(between) != 1 || between[0].ID != list[0].ID {
			t.Errorf("%s: GetTransactionsBetween vazou: %+v", name, between)
		}
		cands, _ := db.GetInstallmentCandidates()
		if len(cands) != 1 || cands[0].ID != list[0].ID {
			t.Errorf("%s: GetInstallmentCandidates vazou: %+v", name, cands)
		}
		sum, _ := db.GetSummaryByMonth("2026-10")
		if sum.TotalExpense != list[0].AmountCents {
			t.Errorf("%s: resumo somou outro livro: %+v", name, sum)
		}
	}

	if err := pessoal.DeleteTransaction(own.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("excluir lançamento de outro livro: %v, quer ErrNotFound", err)
	}
	own.Description = "alterado"
	if err := pessoal.UpdateTransaction(&own); !errors.Is(err, ErrNotFound) {
		t.Errorf("atualizar lançamento de outro livro: %v, quer ErrNotFound", err)
	}
	if list, _ := pj.GetTransactions("2026-10"); len(list) != 1 || list[0].Description != "DAS 1/3" {
		t.Errorf("lançamento do PJ foi mexido: %+v", list)
	}
	if err := pj.DeleteTransaction(own.ID); err != nil {
		t.Errorf("excluir no próprio livro: %v", err)
	}
}

func TestCategories_AreIsolatedByBook(t *testing.T) {
	pessoal, pj := twoBooks(t)
	// "Lazer" já vem nas categorias padrão do Pessoal
	if _, err := pessoal.GetOrCreateCategory("Lazer"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := pj.CategoryExists("Lazer"); ok {
		t.Error("categoria do Pessoal apareceu no PJ")
	}
	c, err := pj.CreateCategory("Lazer") // o mesmo nome vale em outro livro
	if err != nil {
		t.Fatalf("mesmo nome em outro livro: %v", err)
	}
	if _, err := pj.CreateCategory("lazer"); !errors.Is(err, ErrCategoryExists) {
		t.Errorf("repetir no mesmo livro: %v", err)
	}

	mustCreate(t, pessoal, models.Transaction{Description: "Cinema", AmountCents: 1000, Type: "expense", Category: "Lazer", Date: "2026-10-05"})
	cats, _ := pj.ListCategories()
	if len(cats) != 1 || cats[0].Usage != 0 {
		t.Errorf("uso da categoria do PJ contou lançamentos do Pessoal: %+v", cats)
	}
	// renomear no PJ não mexe nos lançamentos do Pessoal
	if err := pj.RenameCategory(c.ID, "Diversão"); err != nil {
		t.Fatal(err)
	}
	if list, _ := pessoal.GetTransactions("2026-10"); list[0].Category != "Lazer" {
		t.Errorf("lançamento do Pessoal foi renomeado: %+v", list[0])
	}
	// categoria de outro livro: ocultar, classificar e excluir devem falhar
	pessoalCats, _ := pessoal.ListCategories()
	var lazerID int64
	for _, pc := range pessoalCats {
		if pc.Name == "Lazer" {
			lazerID = pc.ID
		}
	}
	if err := pj.SetCategoryHidden(lazerID, true); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("ocultar categoria de outro livro: %v", err)
	}
	if err := pj.SetCategoryPriority(lazerID, models.PriorityEssential); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("classificar categoria de outro livro: %v", err)
	}
	if err := pj.DeleteCategory(lazerID); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("excluir categoria de outro livro: %v", err)
	}
	if err := pj.RenameCategory(lazerID, "X"); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("renomear categoria de outro livro: %v", err)
	}
}

func TestImport_GoesToTheBookAndKeepsGlobalDedup(t *testing.T) {
	pessoal, pj := twoBooks(t)
	row := models.Transaction{Description: "Pix", AmountCents: 5000, Type: "income", Category: "Outros", Date: "2026-10-05", ExternalID: "ofx:1"}
	if n, _, err := pj.ImportTransactions([]models.Transaction{row}); err != nil || n != 1 {
		t.Fatalf("import no PJ: %d, %v", n, err)
	}
	if list, _ := pessoal.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("importação vazou para o Pessoal: %+v", list)
	}
	if n, skipped, _ := pessoal.ImportTransactions([]models.Transaction{row}); n != 0 || skipped != 1 {
		t.Errorf("o mesmo extrato em outro livro deveria ser ignorado: n=%d skipped=%d", n, skipped)
	}
	if ids, _ := pessoal.ImportedExternalIDs(); !ids["ofx:1"] {
		t.Error("ImportedExternalIDs deveria ser global")
	}
	mustCreate(t, pj, models.Transaction{Description: "Manual", AmountCents: 700, Type: "expense", Category: "Outros", Date: "2026-10-06"})
	if keys, _ := pessoal.ManualTransactionKeys("2026-10-01", "2026-10-31"); len(keys) != 0 {
		t.Errorf("chaves manuais vazaram: %+v", keys)
	}
	if m, _ := pessoal.LatestCategoryByDescription(); len(m) != 0 {
		t.Errorf("histórico de categorias vazou: %+v", m)
	}
}

func TestBulk_IsScopedByBook(t *testing.T) {
	pessoal, pj := twoBooks(t)
	a := mustCreate(t, pessoal, models.Transaction{Description: "A", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-05"})
	b := mustCreate(t, pj, models.Transaction{Description: "B", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-05"})

	got, _ := pessoal.GetTransactionsByIDs([]int64{a.ID, b.ID})
	if len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("GetTransactionsByIDs alcançou outro livro: %+v", got)
	}
	applied, skipped, err := pessoal.ApplyEdits("Edição", []int64{a.ID, b.ID}, func(t models.Transaction) (models.Transaction, error) {
		t.Category = "Lazer"
		return t, nil
	})
	if err != nil || applied != 1 || skipped != 1 {
		t.Fatalf("ApplyEdits = %d, %d, %v (quer 1 aplicado e 1 ignorado)", applied, skipped, err)
	}
	if cur := current(t, pj, b.ID); cur.Category != "Outros" {
		t.Errorf("lançamento do PJ foi editado: %+v", cur)
	}

	if bt, _ := pj.LatestUndoableBatch(); bt != nil {
		t.Errorf("o PJ enxergou o lote do Pessoal: %+v", bt)
	}
	if _, _, err := pj.UndoLatestBatch(); !errors.Is(err, ErrNoBatch) {
		t.Errorf("desfazer no PJ: %v, quer ErrNoBatch", err)
	}
	if bt, _ := pessoal.LatestUndoableBatch(); bt == nil {
		t.Fatal("o Pessoal deveria ter um lote para desfazer")
	}
	if restored, _, err := pessoal.UndoLatestBatch(); err != nil || restored != 1 {
		t.Errorf("desfazer no Pessoal = %d, %v", restored, err)
	}

	if n, _, err := pessoal.ApplyDeletes("Exclusão", []int64{a.ID, b.ID}); err != nil || n != 1 {
		t.Errorf("ApplyDeletes alcançou outro livro: %d, %v", n, err)
	}
	if got, _ := pj.GetTransactionsByIDs([]int64{b.ID}); len(got) != 1 {
		t.Error("lançamento do PJ foi excluído")
	}
}
