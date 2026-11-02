package database

import (
	"errors"
	"testing"

	"finpi/internal/models"
)

func firstTx(t *testing.T, db *DB, month string) models.Transaction {
	t.Helper()
	list, err := db.GetTransactions(month)
	if err != nil || len(list) != 1 {
		t.Fatalf("GetTransactions = %+v, %v", list, err)
	}
	return list[0]
}

func TestTransaction_AccountRoundTrip(t *testing.T) {
	db := openTestDB(t)
	acc, err := db.CreateAccount("Conta A", "")
	if err != nil {
		t.Fatal(err)
	}
	tx := mustCreate(t, db, models.Transaction{Description: "X", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: acc.ID})
	if got := firstTx(t, db, "2026-10"); got.AccountID != acc.ID {
		t.Fatalf("AccountID = %d, quer %d", got.AccountID, acc.ID)
	}
	tx.AccountID = 0
	if err := db.UpdateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	if got := firstTx(t, db, "2026-10"); got.AccountID != 0 {
		t.Errorf("AccountID = %d, quer 0", got.AccountID)
	}
	tx.AccountID = acc.ID
	if err := db.UpdateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	if got := firstTx(t, db, "2026-10"); got.AccountID != acc.ID {
		t.Errorf("AccountID = %d, quer %d", got.AccountID, acc.ID)
	}
}

func TestTransaction_AccountValidation(t *testing.T) {
	pessoal, pj := twoBooks(t)
	other, _ := pj.CreateAccount("Do PJ", "")
	own, _ := pessoal.CreateAccount("Minha", "")

	bad := models.Transaction{Description: "X", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: other.ID}
	if err := pessoal.CreateTransaction(&bad); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("conta de outro livro: %v", err)
	}
	bad.AccountID = 9999
	if err := pessoal.CreateTransaction(&bad); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("conta inexistente: %v", err)
	}

	tx := mustCreate(t, pessoal, models.Transaction{Description: "X", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: own.ID})
	other2, _ := pessoal.CreateAccount("Outra", "")
	if err := pessoal.SetAccountArchived(own.ID, true); err != nil {
		t.Fatal(err)
	}
	// manter a mesma conta arquivada na edição vale
	tx.Description = "Y"
	if err := pessoal.UpdateTransaction(&tx); err != nil {
		t.Errorf("manter conta arquivada: %v", err)
	}
	// nova atribuição a conta arquivada não
	if err := pessoal.SetAccountArchived(other2.ID, true); err != nil {
		t.Fatal(err)
	}
	tx.AccountID = other2.ID
	if err := pessoal.UpdateTransaction(&tx); !errors.Is(err, ErrAccountArchived) {
		t.Errorf("nova conta arquivada (update): %v", err)
	}
	n := models.Transaction{Description: "Z", AmountCents: 1, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: other2.ID}
	if err := pessoal.CreateTransaction(&n); !errors.Is(err, ErrAccountArchived) {
		t.Errorf("nova conta arquivada (create): %v", err)
	}
	// update com conta de outro livro
	tx.AccountID = other.ID
	if err := pessoal.UpdateTransaction(&tx); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("update conta de outro livro: %v", err)
	}
}

func TestImportTransactions_WritesAccount(t *testing.T) {
	pessoal, pj := twoBooks(t)
	acc, _ := pessoal.CreateAccount("Nubank", models.BankNubank)
	foreign, _ := pj.CreateAccount("PJ", "")

	row := importRow("1", "Rend", 100, "income", "Outros", "2026-08-03")
	row.AccountID = acc.ID
	if n, _, err := pessoal.ImportTransactions([]models.Transaction{row}); err != nil || n != 1 {
		t.Fatalf("import: %d %v", n, err)
	}
	if got := firstTx(t, pessoal, "2026-08"); got.AccountID != acc.ID {
		t.Errorf("AccountID = %d, quer %d", got.AccountID, acc.ID)
	}

	bad := importRow("2", "Rend", 100, "income", "Outros", "2026-08-04")
	bad.AccountID = foreign.ID
	if _, _, err := pessoal.ImportTransactions([]models.Transaction{bad}); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("conta de outro livro: %v", err)
	}
	if list, _ := pessoal.GetTransactions("2026-08"); len(list) != 1 {
		t.Errorf("importação com erro deveria desfazer tudo: %d", len(list))
	}
}

func TestMoveToBook_AssignsDestinationBankAccountAndUndoRestores(t *testing.T) {
	pessoal, pj := twoBooks(t)
	src, _ := pessoal.CreateAccount("Nubank", models.BankNubank)
	imp := importRow("abc", "Compra", 500, "expense", "Outros", "2026-10-05")
	imp.ExternalID = "nu:abc"
	imp.AccountID = src.ID
	if _, _, err := pessoal.ImportTransactions([]models.Transaction{imp}); err != nil {
		t.Fatal(err)
	}
	manual := mustCreate(t, pessoal, models.Transaction{Description: "Manual", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-06", AccountID: src.ID})
	list, _ := pessoal.GetTransactions("2026-10")
	var impID int64
	for _, x := range list {
		if x.Description == "Compra" {
			impID = x.ID
		}
	}
	if accs, _ := pj.ListAccounts(); len(accs) != 0 {
		t.Fatalf("PJ não deveria ter contas: %+v", accs)
	}
	if n, _, err := pessoal.MoveToBook([]int64{impID, manual.ID}, pj.BookID()); err != nil || n != 2 {
		t.Fatalf("MoveToBook: %d %v", n, err)
	}
	accs, _ := pj.ListAccounts()
	if len(accs) != 1 || accs[0].Name != "Nubank" || accs[0].Bank != models.BankNubank {
		t.Fatalf("conta criada no destino: %+v", accs)
	}
	got, _ := pj.GetTransactions("2026-10")
	for _, x := range got {
		switch x.Description {
		case "Compra":
			if x.AccountID != accs[0].ID {
				t.Errorf("importado: AccountID = %d, quer %d", x.AccountID, accs[0].ID)
			}
		case "Manual":
			if x.AccountID != 0 {
				t.Errorf("manual: AccountID = %d, quer 0", x.AccountID)
			}
		}
	}
	if _, _, err := pessoal.UndoLatestBatch(); err != nil {
		t.Fatal(err)
	}
	back, _ := pessoal.GetTransactions("2026-10")
	if len(back) != 2 {
		t.Fatalf("voltaram %d", len(back))
	}
	for _, x := range back {
		if x.AccountID != src.ID {
			t.Errorf("%s: AccountID = %d, quer %d", x.Description, x.AccountID, src.ID)
		}
	}
}

func TestUndoDelete_RestoresAccount(t *testing.T) {
	db := openTestDB(t)
	acc, _ := db.CreateAccount("Conta A", "")
	tx := mustCreate(t, db, models.Transaction{Description: "X", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: acc.ID})
	if n, _, err := db.ApplyDeletes("del", []int64{tx.ID}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if r, _, err := db.UndoLatestBatch(); err != nil || r != 1 {
		t.Fatal(r, err)
	}
	if got := firstTx(t, db, "2026-10"); got.AccountID != acc.ID {
		t.Errorf("AccountID = %d, quer %d", got.AccountID, acc.ID)
	}
}

func TestApplyEdits_PreservesAccountAndUndo(t *testing.T) {
	db := openTestDB(t)
	acc, _ := db.CreateAccount("Conta A", "")
	tx := mustCreate(t, db, models.Transaction{Description: "X", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: acc.ID})
	if n, _, err := db.ApplyEdits("cat", []int64{tx.ID}, toCategory("Mercado")); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if got := firstTx(t, db, "2026-10"); got.AccountID != acc.ID || got.Category != "Mercado" {
		t.Errorf("após edição: %+v", got)
	}
	if r, _, err := db.UndoLatestBatch(); err != nil || r != 1 {
		t.Fatal(r, err)
	}
	if got := firstTx(t, db, "2026-10"); got.AccountID != acc.ID || got.Category != "Outros" {
		t.Errorf("após desfazer: %+v", got)
	}
}

func TestAllTransactions_CarriesAccount(t *testing.T) {
	db := openTestDB(t)
	acc, _ := db.CreateAccount("Conta A", "")
	mustCreate(t, db, models.Transaction{Description: "X", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-10-05", AccountID: acc.ID})
	all, _, err := db.AllTransactions(10)
	if err != nil || len(all) != 1 || all[0].AccountID != acc.ID {
		t.Errorf("AllTransactions = %+v, %v", all, err)
	}
}
