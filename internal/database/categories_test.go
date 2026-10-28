package database

import (
	"sort"
	"testing"

	"finpi/internal/models"
)

func categoryNames(t *testing.T, db *DB) []string {
	t.Helper()
	list, err := db.ListCategories()
	if err != nil {
		t.Fatalf("ListCategories: %v", err)
	}
	var names []string
	for _, c := range list {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}

func TestInitDB_SeedsDefaultCategories(t *testing.T) {
	db := openTestDB(t)

	names := categoryNames(t, db)
	if len(names) != 9 {
		t.Fatalf("esperava 9 categorias padrão, veio %d: %v", len(names), names)
	}
	for _, want := range []string{"Alimentação", "Saúde", "Outros", "Salário"} {
		ok, err := db.CategoryExists(want)
		if err != nil || !ok {
			t.Errorf("categoria padrão %q ausente (err=%v)", want, err)
		}
	}
}

func TestMigrate_SeedsCategoriesAlreadyUsedByTransactions(t *testing.T) {
	db := openTestDB(t)
	// Simula um banco antigo: sem tabela de categorias e com lançamentos em categorias próprias.
	if _, err := db.conn.Exec(`DROP TABLE categories`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	mustCreate(t, db, models.Transaction{Description: "Ração", AmountCents: 100, Type: "expense", Category: "Pets", Date: "2026-10-01"})
	mustCreate(t, db, models.Transaction{Description: "Consulta", AmountCents: 100, Type: "expense", Category: "saude", Date: "2026-10-02"})

	if err := migrate(db.conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	names := categoryNames(t, db)
	if len(names) != 10 {
		t.Fatalf("9 padrão + Pets = 10, veio %d: %v", len(names), names)
	}
	if ok, _ := db.CategoryExists("Pets"); !ok {
		t.Error("a categoria já usada (Pets) deveria ter sido semeada")
	}
	if ok, _ := db.CategoryExists("saude"); !ok {
		t.Error("'saude' deve casar com 'Saúde' (mesmo nome normalizado)")
	}
}

func TestMigrate_SeedsOnlyOnce(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(`DELETE FROM categories WHERE name_key = 'outros'`); err != nil {
		t.Fatal(err)
	}

	if err := migrate(db.conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if ok, _ := db.CategoryExists("Outros"); ok {
		t.Error("categoria excluída não pode ser recriada por uma nova migração")
	}
}

func TestListCategories_CountsUsageIncludingVariants(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "a", AmountCents: 100, Type: "expense", Category: "Saúde", Date: "2026-10-01"})
	mustCreate(t, db, models.Transaction{Description: "b", AmountCents: 100, Type: "expense", Category: "saude", Date: "2026-10-02"})
	mustCreate(t, db, models.Transaction{Description: "c", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-10-03"})

	list, err := db.ListCategories()
	if err != nil {
		t.Fatal(err)
	}
	usage := map[string]int{}
	for _, c := range list {
		usage[c.Name] = c.Usage
	}
	if usage["Saúde"] != 2 || usage["Lazer"] != 1 || usage["Moradia"] != 0 {
		t.Errorf("uso por categoria errado: %v", usage)
	}
}

func TestCategoryExists_IgnoresCaseAndAccents(t *testing.T) {
	db := openTestDB(t)
	for _, name := range []string{"saude", "SAÚDE", " Saúde "} {
		if ok, err := db.CategoryExists(name); err != nil || !ok {
			t.Errorf("CategoryExists(%q) = %v, %v; quer true", name, ok, err)
		}
	}
	if ok, _ := db.CategoryExists("Inexistente"); ok {
		t.Error("categoria inexistente não pode existir")
	}
}
