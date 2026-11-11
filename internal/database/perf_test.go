package database

import (
	"testing"

	"finpi/internal/models"
)

// O filtro de mês usa intervalo de datas (indexável); os limites do mês e do ano precisam continuar certos.
func TestGetTransactions_MonthBounds(t *testing.T) {
	db := openTestDB(t)
	for _, d := range []string{"2026-11-30", "2026-12-01", "2026-12-31", "2027-01-01", "2026-11-01"} {
		mustCreate(t, db, models.Transaction{Description: d, AmountCents: 100, Type: "expense", Category: "Outros", Date: d})
	}
	cases := map[string][]string{
		"2026-11": {"2026-11-30", "2026-11-01"},
		"2026-12": {"2026-12-31", "2026-12-01"},
		"2027-01": {"2027-01-01"},
		"2026-10": nil,
		"2026-1":  nil, // mês inválido não casa com nada (como antes)
		"lixo":    nil,
	}
	for month, want := range cases {
		list, err := db.GetTransactions(month)
		if err != nil {
			t.Fatalf("%s: %v", month, err)
		}
		var got []string
		for _, tx := range list {
			got = append(got, tx.Date)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: datas = %v, quer %v", month, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: datas = %v, quer %v", month, got, want)
			}
		}
	}
}

// GroupsBetween usa o mesmo período de GetTransactionsBetween (as duas pontas inclusive) e separa por
// categoria, tipo e classificação.
func TestGroupsBetween_SameRangeAsGetTransactionsBetween(t *testing.T) {
	db := openTestDB(t)
	for _, tx := range []models.Transaction{
		{Description: "a", AmountCents: 100, Type: "expense", Category: "Lazer", Date: "2026-10-31"},
		{Description: "b", AmountCents: 200, Type: "expense", Category: "Lazer", Date: "2026-11-01"},
		{Description: "c", AmountCents: 300, Type: "expense", Category: "Lazer", Date: "2026-11-30"},
		{Description: "d", AmountCents: 400, Type: "expense", Category: "Lazer", Date: "2026-11-15", Priority: "essential"},
		{Description: "e", AmountCents: 500, Type: "income", Category: "Lazer", Date: "2026-11-15"},
		{Description: "f", AmountCents: 600, Type: "expense", Category: "Lazer", Date: "2026-12-01"},
	} {
		mustCreate(t, db, tx)
	}
	got, err := db.GroupsBetween("2026-11-01", "2026-11-30")
	if err != nil {
		t.Fatal(err)
	}
	want := []models.MonthGroup{
		{Category: "Lazer", Type: "expense", Priority: "", Count: 2, Cents: 500},
		{Category: "Lazer", Type: "expense", Priority: "essential", Count: 1, Cents: 400},
		{Category: "Lazer", Type: "income", Priority: "", Count: 1, Cents: 500},
	}
	if len(got) != len(want) {
		t.Fatalf("grupos = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("grupo %d = %+v, quer %+v", i, got[i], want[i])
		}
	}
	month, _ := db.MonthGroups("2026-11")
	if len(month) != len(got) {
		t.Errorf("MonthGroups e GroupsBetween do mesmo mês diferem: %+v x %+v", month, got)
	}
}
