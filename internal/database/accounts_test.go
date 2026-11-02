package database

import (
	"errors"
	"path/filepath"
	"testing"

	"finpi/internal/models"
)

func TestCreateAccount_Validation(t *testing.T) {
	db := openTestDB(t)
	other, err := db.CreateBook("PJ", "sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAccount("   ", ""); !errors.Is(err, ErrAccountName) {
		t.Errorf("vazio: %v", err)
	}
	long := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" // 41
	if _, err := db.CreateAccount(long, ""); !errors.Is(err, ErrAccountName) {
		t.Errorf("longo: %v", err)
	}
	a, err := db.CreateAccount("Poupança", models.BankNubank)
	if err != nil || a.ID == 0 || a.BookID != DefaultBookID || a.Name != "Poupança" || a.Bank != models.BankNubank {
		t.Fatalf("criar: %+v %v", a, err)
	}
	if _, err := db.CreateAccount("  POUPANCA ", ""); !errors.Is(err, ErrAccountDuplicate) {
		t.Errorf("duplicado: %v", err)
	}
	if _, err := db.ForBook(other.ID).CreateAccount("Poupança", ""); err != nil {
		t.Errorf("mesmo nome em outro livro: %v", err)
	}
}

func TestGetAccount_OtherBook(t *testing.T) {
	db := openTestDB(t)
	other, _ := db.CreateBook("PJ", "sky", 0)
	a, _ := db.ForBook(other.ID).CreateAccount("Caixa", "")
	if _, err := db.GetAccount(a.ID); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("outro livro: %v", err)
	}
	got, err := db.ForBook(other.ID).GetAccount(a.ID)
	if err != nil || got.Name != "Caixa" {
		t.Errorf("get: %+v %v", got, err)
	}
}

func TestRenameAndArchiveAccount(t *testing.T) {
	db := openTestDB(t)
	a, _ := db.CreateAccount("Um", "")
	b, _ := db.CreateAccount("Dois", "")
	if err := db.RenameAccount(b.ID, "um"); !errors.Is(err, ErrAccountDuplicate) {
		t.Errorf("renomear duplicado: %v", err)
	}
	if err := db.RenameAccount(b.ID, ""); !errors.Is(err, ErrAccountName) {
		t.Errorf("renomear vazio: %v", err)
	}
	if err := db.RenameAccount(b.ID, "Três"); err != nil {
		t.Fatal(err)
	}
	if err := db.RenameAccount(999, "X"); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("renomear inexistente: %v", err)
	}
	if err := db.SetAccountArchived(a.ID, true); err != nil {
		t.Fatal(err)
	}
	list, err := db.ListAccounts()
	if err != nil || len(list) != 2 || list[0].Name != "Três" || list[1].ID != a.ID || !list[1].Archived {
		t.Errorf("ordem: %+v %v", list, err)
	}
	if err := db.SetAccountArchived(a.ID, false); err != nil {
		t.Fatal(err)
	}
	list, _ = db.ListAccounts()
	if list[0].ID != a.ID || list[0].Archived {
		t.Errorf("reativar: %+v", list)
	}
	if err := db.SetAccountArchived(999, true); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("arquivar inexistente: %v", err)
	}
}

func TestAccountForBankAndGetOrCreate(t *testing.T) {
	db := openTestDB(t)
	if _, ok, err := db.AccountForBank(models.BankNubank); ok || err != nil {
		t.Fatalf("vazio: %v %v", ok, err)
	}
	a, err := db.GetOrCreateBankAccount(models.BankNubank)
	if err != nil || a.Name != "Nubank" || a.Bank != models.BankNubank {
		t.Fatalf("criar: %+v %v", a, err)
	}
	again, _ := db.GetOrCreateBankAccount(models.BankNubank)
	if again.ID != a.ID {
		t.Errorf("não reaproveitou: %d vs %d", again.ID, a.ID)
	}
	if err := db.SetAccountArchived(a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := db.AccountForBank(models.BankNubank); ok {
		t.Error("AccountForBank devolveu arquivada")
	}
	fresh, err := db.GetOrCreateBankAccount(models.BankNubank)
	if err != nil || fresh.ID == a.ID || fresh.Name != "Nubank (2)" {
		t.Errorf("com arquivada: %+v %v", fresh, err)
	}
}

func TestGetOrCreateBankAccount_NameTaken(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.CreateAccount("Nubank", ""); err != nil {
		t.Fatal(err)
	}
	a, err := db.GetOrCreateBankAccount(models.BankNubank)
	if err != nil || a.Name != "Nubank (2)" || a.Bank != models.BankNubank {
		t.Fatalf("%+v %v", a, err)
	}
	if _, err := db.CreateAccount("Nubank (3)", ""); err != nil {
		t.Fatal(err)
	}
	b, err := db.GetOrCreateBankAccount(models.BankSicredi)
	if err != nil || b.Name != "Sicredi" {
		t.Fatalf("%+v %v", b, err)
	}
}

func TestListAccountsAll(t *testing.T) {
	db := openTestDB(t)
	other, _ := db.CreateBook("PJ", "sky", 0)
	db.CreateAccount("A", "")
	db.ForBook(other.ID).CreateAccount("B", "")
	list, err := db.ListAccountsAll()
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestUsedAccountIDs(t *testing.T) {
	db := openTestDB(t)
	other, _ := db.CreateBook("PJ", "sky", 0)
	a, _ := db.CreateAccount("A", "")
	db.CreateAccount("Sem uso", "")
	b, _ := db.ForBook(other.ID).CreateAccount("B", "")
	for _, tx := range []models.Transaction{
		{Description: "x", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-01", AccountID: a.ID},
		{Description: "y", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-01"},
	} {
		if err := db.CreateTransaction(&tx); err != nil {
			t.Fatal(err)
		}
	}
	tx := models.Transaction{Description: "z", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-01", AccountID: b.ID}
	if err := db.ForBook(other.ID).CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	used, err := db.UsedAccountIDs()
	if err != nil || len(used) != 2 || !used[a.ID] || !used[b.ID] {
		t.Fatalf("em uso = %v %v; quer as contas A e B (de todos os livros, sem o 0)", used, err)
	}
}

func TestBankHelpers(t *testing.T) {
	cases := map[string]string{
		"mp:1": models.BankMercadoPago, "nu:1": models.BankNubank, "sic:1": models.BankSicredi,
		"bb:1": models.BankBB, "": "", "x:1": "", "manual": "",
	}
	for id, want := range cases {
		if got := models.BankFromExternalID(id); got != want {
			t.Errorf("BankFromExternalID(%q) = %q, quer %q", id, got, want)
		}
	}
	names := map[string]string{
		models.BankMercadoPago: "Mercado Pago", models.BankNubank: "Nubank",
		models.BankSicredi: "Sicredi", models.BankBB: "Banco do Brasil", "zzz": "",
	}
	for bank, want := range names {
		if got := models.BankName(bank); got != want {
			t.Errorf("BankName(%q) = %q, quer %q", bank, got, want)
		}
	}
}

func TestMigrateAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mig.db")
	db, err := InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	pj, err := db.CreateBook("PJ", "sky", 0)
	if err != nil {
		t.Fatal(err)
	}
	ins := func(ext string, book int64) {
		t.Helper()
		if _, err := db.conn.Exec(`INSERT INTO transactions (description, amount_cents, type, category, date, external_id, book_id)
			VALUES ('x', 100, 'income', 'Outros', '2026-10-01', ?, ?)`, ext, book); err != nil {
			t.Fatal(err)
		}
	}
	ins("nu:a", 1)
	ins("nu:b", 1)
	ins("sic:c", 1)
	ins("", 1)
	ins("sic:d", pj.ID)
	ins("", pj.ID)
	// Simula o banco anterior às contas.
	if _, err := db.conn.Exec(`DELETE FROM accounts; UPDATE transactions SET account_id = 0; PRAGMA user_version = 2`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	for pass := 0; pass < 2; pass++ {
		db, err = InitDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM accounts`); n != 3 {
			t.Errorf("passo %d: contas = %d, quer 3", pass, n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM accounts WHERE book_id = 1 AND name = 'Nubank' AND bank = 'nubank'`); n != 1 {
			t.Errorf("Nubank no livro 1: %d", n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM accounts WHERE book_id = 1 AND name = 'Sicredi' AND bank = 'sicredi'`); n != 1 {
			t.Errorf("Sicredi no livro 1: %d", n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM accounts WHERE book_id = ? AND name = 'Sicredi'`, pj.ID); n != 1 {
			t.Errorf("Sicredi no PJ: %d", n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM accounts WHERE book_id = ? AND name = 'Nubank'`, pj.ID); n != 0 {
			t.Errorf("Nubank no PJ não deveria existir: %d", n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM transactions t JOIN accounts a ON a.id = t.account_id
			WHERE a.book_id = t.book_id AND ((t.external_id LIKE 'nu:%' AND a.bank = 'nubank') OR (t.external_id LIKE 'sic:%' AND a.bank = 'sicredi'))`); n != 4 {
			t.Errorf("importados atribuídos = %d, quer 4", n)
		}
		if n := count(t, db, `SELECT COUNT(*) FROM transactions WHERE external_id = '' AND account_id = 0`); n != 2 {
			t.Errorf("manuais sem conta = %d, quer 2", n)
		}
		if v := count(t, db, `PRAGMA user_version`); v != 3 {
			t.Errorf("user_version = %d, quer 3", v)
		}
		db.Close()
	}
}
