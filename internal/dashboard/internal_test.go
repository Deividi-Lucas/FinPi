package dashboard

import (
	"testing"

	"finpi/internal/models"
)

func TestSplitInternalAndSummarize(t *testing.T) {
	txs := []models.Transaction{
		{Type: "income", Category: "Salário", AmountCents: 10000},
		{Type: "expense", Category: "Lazer", AmountCents: 2500},
		{Type: "income", Category: "movimentacao  INTERNA", AmountCents: 93365},
		{Type: "expense", Category: models.InternalCategory, AmountCents: 93365},
	}
	regular, in, out := SplitInternal(txs)
	if len(regular) != 2 || in != 93365 || out != 93365 {
		t.Fatalf("SplitInternal = %d regulares, in %d, out %d", len(regular), in, out)
	}
	s := Summarize(regular)
	if s.TotalIncome != 10000 || s.TotalExpense != 2500 || s.Balance != 7500 {
		t.Errorf("Summarize = %+v", s)
	}
	if s := Summarize(nil); s.TotalIncome != 0 || s.Balance != 0 {
		t.Errorf("Summarize(nil) = %+v", s)
	}
}
