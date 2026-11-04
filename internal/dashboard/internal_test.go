package dashboard

import (
	"testing"

	"finpi/internal/models"
)

func TestSplitOutsideAndSummarize(t *testing.T) {
	txs := []models.Transaction{
		{Type: "income", Category: "Salário", AmountCents: 10000},
		{Type: "expense", Category: "Lazer", AmountCents: 2500},
		{Type: "income", Category: "movimentacao  INTERNA", AmountCents: 93365},
		{Type: "expense", Category: models.InternalCategory, AmountCents: 93365},
		{Type: "expense", Category: "investimento", AmountCents: 50000},
		{Type: "expense", Category: models.InvestmentCategory, AmountCents: 1000},
		{Type: "income", Category: models.InvestmentCategory, AmountCents: 20000},
	}
	regular, o := SplitOutside(txs)
	want := Outside{InternalIn: 93365, InternalOut: 93365, Invested: 51000, Redeemed: 20000}
	if len(regular) != 2 || o != want {
		t.Fatalf("SplitOutside = %d regulares, %+v; esperado %+v", len(regular), o, want)
	}
	s := Summarize(regular)
	if s.TotalIncome != 10000 || s.TotalExpense != 2500 || s.Balance != 7500 {
		t.Errorf("Summarize = %+v", s)
	}
	if s := Summarize(nil); s.TotalIncome != 0 || s.Balance != 0 {
		t.Errorf("Summarize(nil) = %+v", s)
	}
}
