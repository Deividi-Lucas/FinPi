package database

import (
	"errors"
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

func findCategory(t *testing.T, db *DB, name string) models.Category {
	t.Helper()
	list, err := db.ListCategories()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("categoria %q não encontrada", name)
	return models.Category{}
}

func TestCreateCategory(t *testing.T) {
	db := openTestDB(t)

	c, err := db.CreateCategory("  Pets  ")
	if err != nil {
		t.Fatalf("CreateCategory: %v", err)
	}
	if c.Name != "Pets" || c.ID == 0 || c.Hidden {
		t.Errorf("categoria criada errada: %+v", c)
	}
	if ok, _ := db.CategoryExists("pets"); !ok {
		t.Error("a categoria criada deveria existir")
	}
}

func TestCreateCategory_RejectsDuplicatesIgnoringCaseAndAccents(t *testing.T) {
	db := openTestDB(t)
	for _, name := range []string{"Lazer", "lazer", "SAUDE", "saúde"} {
		if _, err := db.CreateCategory(name); !errors.Is(err, ErrCategoryExists) {
			t.Errorf("CreateCategory(%q) err = %v; quer ErrCategoryExists", name, err)
		}
	}
}

func TestCreateCategory_RejectsInvalidNames(t *testing.T) {
	db := openTestDB(t)
	tooLong := "12345678901234567890123456789012345678901" // 41 caracteres
	for _, name := range []string{"", "   ", tooLong} {
		if _, err := db.CreateCategory(name); !errors.Is(err, ErrInvalidCategoryName) {
			t.Errorf("CreateCategory(%q) err = %v; quer ErrInvalidCategoryName", name, err)
		}
	}
}

func TestGetOrCreateCategory_ReusesExistingEvenIfHidden(t *testing.T) {
	db := openTestDB(t)
	lazer := findCategory(t, db, "Lazer")
	if err := db.SetCategoryHidden(lazer.ID, true); err != nil {
		t.Fatal(err)
	}

	got, err := db.GetOrCreateCategory("lazer")
	if err != nil {
		t.Fatalf("GetOrCreateCategory: %v", err)
	}
	if got.ID != lazer.ID || got.Name != "Lazer" {
		t.Errorf("deveria reutilizar a existente: %+v", got)
	}
	if len(categoryNames(t, db)) != 9 {
		t.Error("não pode criar duplicata")
	}

	created, err := db.GetOrCreateCategory("Viagens")
	if err != nil || created.Name != "Viagens" {
		t.Errorf("deveria criar a nova: %+v, %v", created, err)
	}
}

func TestRenameCategory_PropagatesToAllTransactionsIncludingVariants(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "a", AmountCents: 100, Type: "expense", Category: "Saúde", Date: "2026-10-01"})
	mustCreate(t, db, models.Transaction{Description: "b", AmountCents: 100, Type: "expense", Category: "saude", Date: "2026-10-02"})
	mustCreate(t, db, models.Transaction{Description: "c", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-10-03"})
	saude := findCategory(t, db, "Saúde")

	if err := db.RenameCategory(saude.ID, "Bem-estar"); err != nil {
		t.Fatalf("RenameCategory: %v", err)
	}

	list, _ := db.GetTransactions("2026-10")
	byDesc := map[string]string{}
	for _, tx := range list {
		byDesc[tx.Description] = tx.Category
	}
	if byDesc["a"] != "Bem-estar" || byDesc["b"] != "Bem-estar" {
		t.Errorf("os lançamentos da categoria deveriam ser renomeados: %v", byDesc)
	}
	if byDesc["c"] != "Lazer" {
		t.Errorf("outras categorias não podem mudar: %v", byDesc)
	}
	if ok, _ := db.CategoryExists("Saúde"); ok {
		t.Error("o nome antigo não deveria mais existir")
	}
	if got := findCategory(t, db, "Bem-estar"); got.ID != saude.ID || got.Usage != 2 {
		t.Errorf("categoria renomeada errada: %+v", got)
	}
}

func TestRenameCategory_RejectsExistingNameAndLeavesDataUntouched(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "a", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-10-01"})
	lazer := findCategory(t, db, "Lazer")

	if err := db.RenameCategory(lazer.ID, "moradia"); !errors.Is(err, ErrCategoryExists) {
		t.Fatalf("err = %v; quer ErrCategoryExists", err)
	}
	list, _ := db.GetTransactions("2026-10")
	if list[0].Category != "Lazer" {
		t.Errorf("lançamento não pode mudar quando a renomeação falha: %+v", list[0])
	}
}

func TestRenameCategory_AllowsChangingOnlyCaseOrAccents(t *testing.T) {
	db := openTestDB(t)
	lazer := findCategory(t, db, "Lazer")

	if err := db.RenameCategory(lazer.ID, "LAZER"); err != nil {
		t.Fatalf("mudar só a caixa deveria ser permitido: %v", err)
	}
	if ok, _ := db.CategoryExists("lazer"); !ok {
		t.Error("a categoria deveria continuar existindo")
	}
}

func TestRenameCategory_InvalidAndNotFound(t *testing.T) {
	db := openTestDB(t)
	lazer := findCategory(t, db, "Lazer")

	if err := db.RenameCategory(lazer.ID, "  "); !errors.Is(err, ErrInvalidCategoryName) {
		t.Errorf("err = %v; quer ErrInvalidCategoryName", err)
	}
	if err := db.RenameCategory(9999, "Qualquer"); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("err = %v; quer ErrCategoryNotFound", err)
	}
}

func TestSetCategoryHidden(t *testing.T) {
	db := openTestDB(t)
	lazer := findCategory(t, db, "Lazer")

	if err := db.SetCategoryHidden(lazer.ID, true); err != nil {
		t.Fatal(err)
	}
	if !findCategory(t, db, "Lazer").Hidden {
		t.Error("deveria estar oculta")
	}
	if err := db.SetCategoryHidden(lazer.ID, false); err != nil {
		t.Fatal(err)
	}
	if findCategory(t, db, "Lazer").Hidden {
		t.Error("deveria estar visível de novo")
	}
	if err := db.SetCategoryHidden(9999, true); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("err = %v; quer ErrCategoryNotFound", err)
	}
}

func TestDeleteCategory_BlockedWhenInUse(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "a", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-10-01"})
	mustCreate(t, db, models.Transaction{Description: "b", AmountCents: 100, Type: "expense", Category: "lazer", Date: "2026-10-02"})
	lazer := findCategory(t, db, "Lazer")

	err := db.DeleteCategory(lazer.ID)

	var inUse *CategoryInUseError
	if !errors.As(err, &inUse) || inUse.Count != 2 || inUse.Name != "Lazer" {
		t.Fatalf("err = %v; quer *CategoryInUseError{Lazer, 2}", err)
	}
	if ok, _ := db.CategoryExists("Lazer"); !ok {
		t.Error("categoria em uso não pode ser excluída")
	}
}

func TestDeleteCategory_UnusedAndNotFound(t *testing.T) {
	db := openTestDB(t)
	outros := findCategory(t, db, "Outros")

	if err := db.DeleteCategory(outros.ID); err != nil {
		t.Fatalf("DeleteCategory: %v", err)
	}
	if ok, _ := db.CategoryExists("Outros"); ok {
		t.Error("categoria sem uso deveria ser excluída")
	}
	if err := db.DeleteCategory(outros.ID); !errors.Is(err, ErrCategoryNotFound) {
		t.Errorf("err = %v; quer ErrCategoryNotFound", err)
	}
}

func TestCategoryInUseError_Message(t *testing.T) {
	one := (&CategoryInUseError{Name: "Lazer", Count: 1}).Error()
	many := (&CategoryInUseError{Name: "Lazer", Count: 12}).Error()
	if one != `"Lazer" está em uso por 1 lançamento; renomeie ou oculte` {
		t.Errorf("mensagem singular: %q", one)
	}
	if many != `"Lazer" está em uso por 12 lançamentos; renomeie ou oculte` {
		t.Errorf("mensagem plural: %q", many)
	}
}
