package installments

import (
	"testing"

	"finpi/internal/models"
)

func TestAnnotate(t *testing.T) {
	list := []models.Transaction{
		tx(1, "Geladeira 3/10", "expense", "2026-10-15", 25000),
		tx(2, "Mercado", "expense", "2026-10-05", 4590),
		tx(3, "Sofá 5/5", "expense", "2026-10-20", 50000),
	}

	Annotate(list)

	got := list[0].Installment
	if got == nil || got.Number != 3 || got.Total != 10 || got.Remaining != 7 || got.RemainingCents != 175000 {
		t.Errorf("selo da 3/10 errado: %+v", got)
	}
	if list[1].Installment != nil {
		t.Errorf("lançamento comum não ganha selo: %+v", list[1].Installment)
	}
	if last := list[2].Installment; last == nil || last.Remaining != 0 || last.RemainingCents != 0 {
		t.Errorf("a última parcela não tem restante: %+v", last)
	}
}
