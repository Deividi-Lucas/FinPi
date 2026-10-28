package dashboard

import (
	"testing"

	"finpi/internal/models"
)

func exp(id int64, cents int64, category, date, priority string) models.Transaction {
	return models.Transaction{ID: id, Description: "d", AmountCents: cents, Type: "expense", Category: category, Date: date, Priority: priority}
}

func inc(id int64, cents int64) models.Transaction {
	return models.Transaction{ID: id, Description: "r", AmountCents: cents, Type: "income", Category: "Salário", Date: "2026-10-05"}
}

func cats() []models.Category {
	return []models.Category{
		{ID: 1, Name: "Saúde", Priority: models.PriorityEssential},
		{ID: 2, Name: "Lazer", Priority: models.PrioritySuperfluous},
		{ID: 3, Name: "Outros"},
	}
}

func TestEffectivePriority_TransactionBeatsCategory(t *testing.T) {
	by := CategoryPriorities(cats())
	got := EffectivePriority(exp(1, 100, "Lazer", "2026-10-01", models.PriorityEssential), by)
	if got != models.PriorityEssential {
		t.Errorf("got %q, quer essential", got)
	}
}

func TestEffectivePriority_FollowsCategoryIgnoringCaseAndAccents(t *testing.T) {
	by := CategoryPriorities(cats())
	for _, name := range []string{"Saúde", "saude", "SAÚDE"} {
		if got := EffectivePriority(exp(1, 100, name, "2026-10-01", ""), by); got != models.PriorityEssential {
			t.Errorf("categoria %q: got %q, quer essential", name, got)
		}
	}
}

func TestEffectivePriority_UnclassifiedWhenNothingSet(t *testing.T) {
	by := CategoryPriorities(cats())
	if got := EffectivePriority(exp(1, 100, "Outros", "2026-10-01", ""), by); got != "" {
		t.Errorf("got %q, quer vazio", got)
	}
	if got := EffectivePriority(exp(1, 100, "Desconhecida", "2026-10-01", ""), by); got != "" {
		t.Errorf("categoria desconhecida: got %q, quer vazio", got)
	}
}

func TestAnnotate_SetsEffectivePriorityOnlyForExpenses(t *testing.T) {
	txs := []models.Transaction{exp(1, 100, "Saúde", "2026-10-01", ""), inc(2, 5000)}
	Annotate(txs, cats())
	if txs[0].EffectivePriority != models.PriorityEssential {
		t.Errorf("despesa: got %q", txs[0].EffectivePriority)
	}
	if txs[1].EffectivePriority != "" {
		t.Errorf("receita: got %q, quer vazio", txs[1].EffectivePriority)
	}
}

func TestCompute_CountsAndExtremes(t *testing.T) {
	txs := []models.Transaction{
		inc(1, 500000),
		exp(2, 3000, "Saúde", "2026-10-03", ""),
		exp(3, 12000, "Lazer", "2026-10-04", ""),
		exp(4, 800, "Outros", "2026-10-05", ""),
	}
	s := Compute(txs, cats())
	if s.TotalCount != 4 || s.IncomeCount != 1 || s.ExpenseCount != 3 {
		t.Errorf("contagens = %d/%d/%d", s.TotalCount, s.IncomeCount, s.ExpenseCount)
	}
	if s.Largest == nil || s.Largest.ID != 3 {
		t.Errorf("maior = %+v, quer ID 3", s.Largest)
	}
	if s.Smallest == nil || s.Smallest.ID != 4 {
		t.Errorf("menor = %+v, quer ID 4", s.Smallest)
	}
}

func TestCompute_IncomeNeverCountsAsLargestOrSmallest(t *testing.T) {
	s := Compute([]models.Transaction{inc(1, 999999999), exp(2, 100, "Outros", "2026-10-01", "")}, cats())
	if s.Largest.ID != 2 || s.Smallest.ID != 2 {
		t.Errorf("maior/menor = %d/%d, quer 2/2", s.Largest.ID, s.Smallest.ID)
	}
}

func TestCompute_TieGoesToMostRecentThenHighestID(t *testing.T) {
	txs := []models.Transaction{
		exp(1, 5000, "Outros", "2026-10-02", ""),
		exp(2, 5000, "Outros", "2026-10-09", ""),
		exp(3, 5000, "Outros", "2026-10-09", ""),
	}
	s := Compute(txs, cats())
	if s.Largest.ID != 3 || s.Smallest.ID != 3 {
		t.Errorf("empate: maior/menor = %d/%d, quer 3/3 (data mais recente, depois maior ID)", s.Largest.ID, s.Smallest.ID)
	}
}

func TestCompute_NoExpenses(t *testing.T) {
	s := Compute([]models.Transaction{inc(1, 5000)}, cats())
	if s.Largest != nil || s.Smallest != nil {
		t.Errorf("sem despesas: maior/menor deveriam ser nulos")
	}
	if s.ExpenseTotal != 0 || s.Unclassified().Percent != 0 {
		t.Errorf("total/percentual deveriam ser zero")
	}
}

func TestCompute_GroupsSumsCountsAndPercents(t *testing.T) {
	txs := []models.Transaction{
		exp(1, 4000, "Saúde", "2026-10-01", ""),
		exp(2, 2000, "Saúde", "2026-10-02", ""),
		exp(3, 3000, "Lazer", "2026-10-03", ""),
		exp(4, 1000, "Outros", "2026-10-04", ""),
		exp(5, 500, "Lazer", "2026-10-05", models.PriorityEssential), // exceção: essencial
		inc(6, 9999999), // fora da divisão
	}
	s := Compute(txs, cats())
	ess, sup, unc := s.Essential(), s.Superfluous(), s.Unclassified()
	if ess.Cents != 6500 || ess.Count != 3 {
		t.Errorf("essencial = %+v", ess)
	}
	if sup.Cents != 3000 || sup.Count != 1 {
		t.Errorf("supérfluo = %+v", sup)
	}
	if unc.Cents != 1000 || unc.Count != 1 {
		t.Errorf("sem classificação = %+v", unc)
	}
	if s.ExpenseTotal != 10500 {
		t.Errorf("total = %d, quer 10500", s.ExpenseTotal)
	}
	if ess.Percent != 61.9 || sup.Percent != 28.6 || unc.Percent != 9.5 {
		t.Errorf("percentuais = %v/%v/%v, quer 61.9/28.6/9.5", ess.Percent, sup.Percent, unc.Percent)
	}
}
