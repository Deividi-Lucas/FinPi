package database

import (
	"testing"

	"finpi/internal/models"
)

func TestGetTransactionsBetween_InclusiveBoundsAndOrder(t *testing.T) {
	db := openTestDB(t)
	for _, d := range []string{"2026-01-31", "2026-02-01", "2026-02-15", "2026-02-28", "2026-03-01"} {
		mustCreate(t, db, models.Transaction{Description: d, AmountCents: 100, Type: "expense", Category: "Outros", Date: d})
	}
	mustCreate(t, db, models.Transaction{Description: "segundo do dia", AmountCents: 100, Type: "income", Category: "Outros", Date: "2026-02-15"})

	got, err := db.GetTransactionsBetween("2026-02-01", "2026-02-28")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2026-02-01", "2026-02-15", "segundo do dia", "2026-02-28"}
	if len(got) != len(want) {
		t.Fatalf("lançamentos = %d, quer %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Description != w {
			t.Errorf("posição %d = %q, quer %q", i, got[i].Description, w)
		}
	}
}

func TestGetTransactionsBetween_EmptyPeriod(t *testing.T) {
	db := openTestDB(t)
	got, err := db.GetTransactionsBetween("2030-01-01", "2030-12-31")
	if err != nil || len(got) != 0 {
		t.Errorf("got=%v err=%v", got, err)
	}
}

func TestGetTransactionsBetween_ReturnsPriorityAndCategory(t *testing.T) {
	db := openTestDB(t)
	mustCreate(t, db, models.Transaction{Description: "X", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-02-01", Priority: models.PrioritySuperfluous})
	got, _ := db.GetTransactionsBetween("2026-02-01", "2026-02-01")
	if len(got) != 1 || got[0].Category != "Lazer" || got[0].Priority != models.PrioritySuperfluous {
		t.Errorf("got = %+v", got)
	}
}
