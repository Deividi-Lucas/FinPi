package report

import (
	"testing"
	"time"

	"finpi/internal/models"
)

func tx(typ string, cents int64, date string) models.Transaction {
	return models.Transaction{Type: typ, AmountCents: cents, Date: date}
}

// span devolve o último dia de um período que começa em from e tem n dias.
func span(t *testing.T, from string, n int) string {
	t.Helper()
	d, err := time.Parse("2006-01-02", from)
	if err != nil {
		t.Fatal(err)
	}
	return d.AddDate(0, 0, n-1).Format("2006-01-02")
}

func TestAutoGranularity_Thresholds(t *testing.T) {
	cases := []struct {
		days int
		want Granularity
	}{{1, Day}, {62, Day}, {63, Week}, {120, Week}, {121, Month}, {365, Month}}
	for _, c := range cases {
		if got := AutoGranularity("2026-01-01", span(t, "2026-01-01", c.days)); got != c.want {
			t.Errorf("%d dias: %s, quer %s", c.days, got, c.want)
		}
	}
}

func TestAllowed_Limits(t *testing.T) {
	cases := []struct {
		g    Granularity
		days int
		want bool
	}{
		{Day, 93, true}, {Day, 94, false},
		{Week, 731, true}, {Week, 732, false},
		{Month, 1, true}, {Month, 1800, true},
	}
	for _, c := range cases {
		if got := Allowed(c.g, "2026-01-01", span(t, "2026-01-01", c.days)); got != c.want {
			t.Errorf("%s com %d dias: %v, quer %v", c.g, c.days, got, c.want)
		}
	}
}

func TestParseGranularity(t *testing.T) {
	for in, want := range map[string]Granularity{"dia": Day, "semana": Week, "mes": Month} {
		if g, ok := ParseGranularity(in); !ok || g != want {
			t.Errorf("%q = %q %v", in, g, ok)
		}
	}
	for _, bad := range []string{"", "ano", "DIA", "mês"} {
		if _, ok := ParseGranularity(bad); ok {
			t.Errorf("%q não deveria ser aceito", bad)
		}
	}
}

func TestBuildFlow_DaysAndRunningBalance(t *testing.T) {
	txs := []models.Transaction{
		tx("income", 10000, "2026-08-03"),
		tx("expense", 4000, "2026-08-05"),
		tx("expense", 999, "2026-08-08"), // fora do período
	}
	got := BuildFlow(txs, nil, "2026-08-03", "2026-08-07", Day)
	if len(got) != 5 {
		t.Fatalf("fatias = %d, quer 5", len(got))
	}
	wantBalance := []int64{10000, 10000, 6000, 6000, 6000}
	for i, b := range got {
		if b.Balance != wantBalance[i] {
			t.Errorf("fatia %d (%s): saldo %d, quer %d", i, b.Label, b.Balance, wantBalance[i])
		}
	}
	if got[0].Label != "03/08" || got[0].Start != "2026-08-03" || got[0].End != "2026-08-03" {
		t.Errorf("fatia 0 = %+v", got[0])
	}
	if got[2].Expense != 4000 || got[2].Net != -4000 {
		t.Errorf("fatia 2 = %+v", got[2])
	}
}

func TestBuildFlow_WeeksStartOnMondayAndAreClipped(t *testing.T) {
	// 05/08/2026 é quarta; 20/08 é quinta.
	got := BuildFlow(nil, nil, "2026-08-05", "2026-08-20", Week)
	want := []struct{ label, start, end string }{
		{"05/08–09/08", "2026-08-05", "2026-08-09"},
		{"10/08–16/08", "2026-08-10", "2026-08-16"},
		{"17/08–20/08", "2026-08-17", "2026-08-20"},
	}
	if len(got) != len(want) {
		t.Fatalf("fatias = %d, quer %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Label != w.label || got[i].Start != w.start || got[i].End != w.end {
			t.Errorf("fatia %d = %+v, quer %+v", i, got[i], w)
		}
	}
}

func TestBuildFlow_MonthsAcrossYearAndLeapDay(t *testing.T) {
	got := BuildFlow(nil, nil, "2026-11-15", "2027-02-10", Month)
	labels := []string{"nov/26", "dez/26", "jan/27", "fev/27"}
	if len(got) != 4 {
		t.Fatalf("fatias = %d: %+v", len(got), got)
	}
	for i, l := range labels {
		if got[i].Label != l {
			t.Errorf("fatia %d = %q, quer %q", i, got[i].Label, l)
		}
	}
	if got[0].Start != "2026-11-15" || got[0].End != "2026-11-30" || got[3].End != "2027-02-10" {
		t.Errorf("bordas = %+v ... %+v", got[0], got[3])
	}

	leap := BuildFlow(nil, nil, "2028-02-01", "2028-03-31", Month)
	if leap[0].End != "2028-02-29" {
		t.Errorf("fim de fevereiro/2028 = %s, quer 2028-02-29", leap[0].End)
	}
}

func TestBuildFlow_BucketsAreContiguousAndCoverThePeriod(t *testing.T) {
	for _, g := range []Granularity{Day, Week, Month} {
		got := BuildFlow(nil, nil, "2026-01-01", "2026-12-31", g)
		if got[0].Start != "2026-01-01" || got[len(got)-1].End != "2026-12-31" {
			t.Errorf("%s: cobertura %s a %s", g, got[0].Start, got[len(got)-1].End)
		}
		for i := 1; i < len(got); i++ {
			prevEnd, _ := time.Parse("2006-01-02", got[i-1].End)
			if got[i].Start != prevEnd.AddDate(0, 0, 1).Format("2006-01-02") {
				t.Errorf("%s: buraco entre %s e %s", g, got[i-1].End, got[i].Start)
			}
		}
	}
}

func TestBuildFlow_EmptyBucketsKeepRunningBalance(t *testing.T) {
	got := BuildFlow([]models.Transaction{tx("income", 500, "2026-01-10")}, nil, "2026-01-01", "2026-03-31", Month)
	if len(got) != 3 || got[1].Income != 0 || got[1].Balance != 500 || got[2].Balance != 500 {
		t.Errorf("fatias = %+v", got)
	}
}

func TestBuildFlow_SeparatesProjectedFromRealized(t *testing.T) {
	txs := []models.Transaction{tx("expense", 2500, "2026-10-15")}
	proj := []models.ProjectedInstallment{
		{Type: "expense", Date: "2026-11-15", AmountCents: 2500},
		{Type: "income", Date: "2026-11-20", AmountCents: 100},
		{Type: "expense", Date: "2027-01-15", AmountCents: 9999}, // fora do período
	}
	got := BuildFlow(txs, proj, "2026-10-01", "2026-12-31", Month)
	if got[0].HasProjection() || got[0].Expense != 2500 {
		t.Errorf("outubro = %+v", got[0])
	}
	nov := got[1]
	if !nov.HasProjection() || nov.ProjExpense != 2500 || nov.ProjIncome != 100 || nov.Expense != 0 {
		t.Errorf("novembro = %+v", nov)
	}
	if nov.TotalExpense() != 2500 || nov.TotalIncome() != 100 || nov.Net != -2400 {
		t.Errorf("totais de novembro = %d/%d/%d", nov.TotalIncome(), nov.TotalExpense(), nov.Net)
	}
	if got[2].Balance != -2500-2400 {
		t.Errorf("saldo final = %d, quer %d", got[2].Balance, -2500-2400)
	}
}
