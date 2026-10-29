package database

import (
	"errors"
	"testing"

	"finpi/internal/models"
)

func TestCreateBook_AndList(t *testing.T) {
	db := openTestDB(t)
	b, err := db.CreateBook("  PJ  ", "sky", 0)
	if err != nil || b.Name != "PJ" || b.Color != "sky" || b.Archived {
		t.Fatalf("CreateBook = %+v, %v", b, err)
	}
	list, err := db.ListBooks()
	if err != nil || len(list) != 2 || list[0].Name != "Pessoal" || list[1].Name != "PJ" {
		t.Fatalf("ListBooks = %+v, %v", list, err)
	}
	got, err := db.GetBook(b.ID)
	if err != nil || got.Name != "PJ" {
		t.Fatalf("GetBook = %+v, %v", got, err)
	}
	if _, err := db.GetBook(999); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("GetBook(999) = %v, quer ErrBookNotFound", err)
	}
}

func TestCreateBook_Validation(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.CreateBook("", "sky", 0); !errors.Is(err, ErrInvalidBookName) {
		t.Errorf("nome vazio: %v", err)
	}
	if _, err := db.CreateBook("PJ", "magenta", 0); !errors.Is(err, ErrInvalidBookColor) {
		t.Errorf("cor inválida: %v", err)
	}
	if _, err := db.CreateBook("PESSOAL", "sky", 0); !errors.Is(err, ErrBookExists) {
		t.Errorf("nome repetido (sem caixa): %v", err)
	}
	if _, err := db.CreateBook("PJ", "sky", 999); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("copiar de livro inexistente: %v", err)
	}
}

func TestCreateBook_CopiesCategoriesButNotTransactions(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.CreateCategory("Mercado"); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetOrCreateCategory("Mercado")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetCategoryPriority(c.ID, models.PriorityEssential); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, models.Transaction{Description: "Compra", AmountCents: 1000, Type: "expense", Category: "Mercado", Date: "2026-10-05"})

	pj, err := db.CreateBook("PJ", "sky", db.BookID())
	if err != nil {
		t.Fatal(err)
	}
	cats, err := db.ForBook(pj.ID).ListCategories()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, cat := range cats {
		if cat.Name == "Mercado" && cat.Priority == models.PriorityEssential {
			found = true
		}
	}
	if !found {
		t.Errorf("categoria Mercado (com prioridade) não foi copiada: %+v", cats)
	}
	if got, _ := db.ForBook(pj.ID).GetTransactions("2026-10"); len(got) != 0 {
		t.Errorf("lançamentos não deveriam ser copiados: %+v", got)
	}

	empty, err := db.CreateBook("Vazio", "rose", 0)
	if err != nil {
		t.Fatal(err)
	}
	if cats, _ := db.ForBook(empty.ID).ListCategories(); len(cats) != 0 {
		t.Errorf("livro vazio deveria começar sem categorias: %+v", cats)
	}
}

func TestUpdateBook(t *testing.T) {
	db := openTestDB(t)
	pj, _ := db.CreateBook("PJ", "sky", 0)
	if err := db.UpdateBook(pj.ID, "Empresa", "amber"); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetBook(pj.ID)
	if got.Name != "Empresa" || got.Color != "amber" {
		t.Errorf("UpdateBook não gravou: %+v", got)
	}
	if err := db.UpdateBook(pj.ID, "pessoal", "amber"); !errors.Is(err, ErrBookExists) {
		t.Errorf("renomear para nome existente: %v", err)
	}
	if err := db.UpdateBook(pj.ID, "Empresa", "x"); !errors.Is(err, ErrInvalidBookColor) {
		t.Errorf("cor inválida: %v", err)
	}
	if err := db.UpdateBook(999, "X", "sky"); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("livro inexistente: %v", err)
	}
}

func TestSetBookArchived_KeepsOneActiveBook(t *testing.T) {
	db := openTestDB(t)
	pj, _ := db.CreateBook("PJ", "sky", 0)
	if err := db.SetBookArchived(pj.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetBook(pj.ID); !got.Archived {
		t.Error("livro deveria estar arquivado")
	}
	if err := db.SetBookArchived(DefaultBookID, true); !errors.Is(err, ErrLastBook) {
		t.Errorf("arquivar o último ativo: %v, quer ErrLastBook", err)
	}
	if err := db.SetBookArchived(pj.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBookArchived(999, true); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("livro inexistente: %v", err)
	}
}
