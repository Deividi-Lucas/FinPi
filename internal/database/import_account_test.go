package database

import (
	"errors"
	"testing"

	"finpi/internal/models"
)

func TestImportTransactionsToAccount_Modes(t *testing.T) {
	db := openTestDB(t)
	row := func(ref string) []models.Transaction {
		return []models.Transaction{{Description: "x", AmountCents: 100, Type: "expense", Category: "Outros", Date: "2026-01-02", ExternalID: "nu:" + ref}}
	}
	if _, _, err := db.ImportTransactionsToAccount(row("a"), ImportAccount{Bank: models.BankNubank}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.ImportTransactionsToAccount(row("b"), ImportAccount{Bank: models.BankNubank}); err != nil {
		t.Fatal(err)
	}
	accs, _ := db.ListAccounts()
	if len(accs) != 1 || accs[0].Name != "Nubank" {
		t.Fatalf("a conta do banco deveria ser reaproveitada: %+v", accs)
	}
	if _, _, err := db.ImportTransactionsToAccount(row("c"), ImportAccount{NewName: "nubank"}); !errors.Is(err, ErrAccountDuplicate) {
		t.Errorf("err = %v", err)
	}
	if _, _, err := db.ImportTransactionsToAccount(row("c"), ImportAccount{}); !errors.Is(err, ErrAccountNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestImportTransactionsToAccount_FailedSaveLeavesNoAccount(t *testing.T) {
	db := openTestDB(t)
	bad := []models.Transaction{{Description: "x", AmountCents: 100, Type: "invalido", Category: "Outros", Date: "2026-01-02", ExternalID: "nu:z"}}
	if _, _, err := db.ImportTransactionsToAccount(bad, ImportAccount{NewName: "Temporária"}); err == nil {
		t.Fatal("esperava erro de gravação")
	}
	if _, _, err := db.ImportTransactionsToAccount(bad, ImportAccount{Bank: models.BankNubank}); err == nil {
		t.Fatal("esperava erro de gravação")
	}
	if accs, _ := db.ListAccounts(); len(accs) != 0 {
		t.Errorf("sobrou conta: %+v", accs)
	}
}
