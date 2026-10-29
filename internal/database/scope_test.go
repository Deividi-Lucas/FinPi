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
