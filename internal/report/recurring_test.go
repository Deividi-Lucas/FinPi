package report

import (
	"testing"

	"finpi/internal/models"
)

func ex(id int64, desc, category string, cents int64, date string) models.Transaction {
	return models.Transaction{ID: id, Type: "expense", Description: desc, Category: category, AmountCents: cents, Date: date}
}

func TestRecurring_CategoryGroupsIgnoreCaseAndAccents(t *testing.T) {
	txs := []models.Transaction{
		ex(1, "a", "Saúde", 100, "2026-01-01"),
		ex(2, "b", "saude", 100, "2026-01-02"),
		ex(3, "c", "SAÚDE", 100, "2026-01-03"),
		ex(4, "d", "Saúde", 100, "2026-01-04"),
		ex(5, "e", "Lazer", 100, "2026-01-05"),
	}
	r := Recurring(txs)
	if len(r.ByCategory) != 2 {
		t.Fatalf("categorias = %+v", r.ByCategory)
	}
	top := r.ByCategory[0]
	if top.Name != "Saúde" || top.Count != 4 || top.TotalCents != 400 {
		t.Errorf("topo = %+v (a grafia mais frequente é 'Saúde')", top)
	}
	if r.ByCategory[1].Name != "Lazer" || r.ByCategory[1].Count != 1 {
		t.Errorf("segundo = %+v", r.ByCategory[1])
	}
}

func TestRecurring_DescriptionDropsInstallmentMarkerAndNeedsTwo(t *testing.T) {
	txs := []models.Transaction{
		ex(1, "Geladeira 3/10", "Casa", 25000, "2026-08-10"),
		ex(2, "Geladeira 4/10", "Casa", 25000, "2026-09-10"),
		ex(3, "geladeira", "Casa", 500, "2026-09-11"),
		ex(4, "Cinema", "Lazer", 4500, "2026-09-12"), // só uma vez: não é recorrente
	}
	r := Recurring(txs)
	if len(r.ByDescription) != 1 {
		t.Fatalf("descrições = %+v", r.ByDescription)
	}
	g := r.ByDescription[0]
	if g.Count != 3 || g.TotalCents != 50500 || g.Name != "Geladeira" {
		t.Errorf("grupo = %+v", g)
	}
	if len(r.ByCategory) != 2 {
		t.Errorf("a aba de categoria mostra todas: %+v", r.ByCategory)
	}
}

func TestRecurring_OnlyExpensesCount(t *testing.T) {
	txs := []models.Transaction{
		{ID: 1, Type: "income", Description: "Salário", Category: "Salário", AmountCents: 500000, Date: "2026-01-01"},
		{ID: 2, Type: "income", Description: "Salário", Category: "Salário", AmountCents: 500000, Date: "2026-02-01"},
		ex(3, "Mercado", "Alimentação", 1000, "2026-01-02"),
	}
	r := Recurring(txs)
	if r.ExpenseTotal != 1000 || len(r.ByCategory) != 1 || len(r.ByDescription) != 0 {
		t.Errorf("resultado = %+v", r)
	}
}

func TestRecurring_OrderCountThenTotalThenName(t *testing.T) {
	txs := []models.Transaction{
		ex(1, "B", "B", 100, "2026-01-01"), ex(2, "B", "B", 100, "2026-01-02"), // 2 × 100 = 200
		ex(3, "A", "A", 300, "2026-01-03"), ex(4, "A", "A", 300, "2026-01-04"), // 2 × 300 = 600 (mais total)
		ex(5, "C", "C", 100, "2026-01-05"), ex(6, "C", "C", 100, "2026-01-06"), // empata com B: ordem por nome
		ex(7, "D", "D", 9999, "2026-01-07"), // 1 vez
	}
	r := Recurring(txs)
	var names []string
	for _, row := range r.ByCategory {
		names = append(names, row.Name)
	}
	want := []string{"A", "B", "C", "D"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("ordem = %v, quer %v", names, want)
		}
	}
}

func TestRecurring_AverageAndPercent(t *testing.T) {
	txs := []models.Transaction{
		ex(1, "X", "A", 1000, "2026-01-01"), ex(2, "X", "A", 1000, "2026-01-02"), ex(3, "X", "A", 1000, "2026-01-03"),
		ex(4, "Y", "B", 500, "2026-01-04"), ex(5, "Y", "B", 501, "2026-01-05"),
	}
	r := Recurring(txs)
	if r.ExpenseTotal != 4001 {
		t.Fatalf("total = %d", r.ExpenseTotal)
	}
	a, b := r.ByCategory[0], r.ByCategory[1]
	if a.AvgCents != 1000 || a.Percent != 75.0 {
		t.Errorf("A = %+v", a)
	}
	if b.AvgCents != 501 || b.Percent != 25.0 { // 1001/2 = 500,5 → arredonda para 501
		t.Errorf("B = %+v", b)
	}
}

func TestRecurring_SpellingTieGoesToEarliest(t *testing.T) {
	txs := []models.Transaction{
		ex(2, "Padaria Central", "Alimentação", 100, "2026-01-02"),
		ex(1, "padaria central", "Alimentação", 100, "2026-01-01"),
	}
	r := Recurring(txs)
	if got := r.ByDescription[0].Name; got != "padaria central" {
		t.Errorf("nome = %q, quer a grafia mais antiga em caso de empate", got)
	}
}

func TestRecurring_Empty(t *testing.T) {
	r := Recurring(nil)
	if r.ExpenseTotal != 0 || len(r.ByCategory) != 0 || len(r.ByDescription) != 0 {
		t.Errorf("resultado = %+v", r)
	}
}
