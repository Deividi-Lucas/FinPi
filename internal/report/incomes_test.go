package report

import (
	"testing"

	"finpi/internal/models"
)

func in(id int64, desc, category string, cents int64, date string) models.Transaction {
	return models.Transaction{ID: id, Type: "income", Description: desc, Category: category, AmountCents: cents, Date: date}
}

func TestIncomes_SourcesByDescriptionIncludeSingleOnes(t *testing.T) {
	txs := []models.Transaction{
		in(1, "Salário", "Salário", 500000, "2026-01-05"),
		in(2, "salario", "Salário", 500000, "2026-02-05"),
		in(3, "Bônus", "Outros", 200000, "2026-02-20"),
		in(4, "Pix recebido Fulano", "Outros", 10000, "2026-03-01"),
	}
	r := Incomes(txs, 12)
	if len(r.BySource) != 3 || r.Sources != 3 {
		t.Fatalf("fontes = %+v", r.BySource)
	}
	top := r.BySource[0]
	if top.Name != "Salário" || top.Count != 2 || top.TotalCents != 1000000 {
		t.Errorf("topo = %+v (a grafia mais frequente é 'Salário', empate vai para a mais antiga)", top)
	}
	if r.BySource[1].Name != "Bônus" || r.BySource[1].Count != 1 {
		t.Errorf("segundo = %+v (uma só entrada também é fonte)", r.BySource[1])
	}
	if r.Total != 1210000 {
		t.Errorf("total = %d", r.Total)
	}
}

func TestIncomes_OnlyIncomeCounts(t *testing.T) {
	txs := []models.Transaction{
		in(1, "Salário", "Salário", 1000, "2026-01-05"),
		{ID: 2, Type: "expense", Description: "Mercado", Category: "Alimentação", AmountCents: 999999, Date: "2026-01-06"},
	}
	r := Incomes(txs, 12)
	if r.Total != 1000 || len(r.BySource) != 1 || len(r.ByCategory) != 1 {
		t.Errorf("resultado = %+v", r)
	}
}

func TestIncomes_PercentAverageMonthsAndLast(t *testing.T) {
	txs := []models.Transaction{
		in(1, "Salário", "Salário", 3000, "2026-01-05"),
		in(2, "Salário", "Salário", 3000, "2026-01-25"), // mesmo mês: conta 1 mês
		in(3, "Salário", "Salário", 3001, "2026-03-05"),
		in(4, "Freela", "Outros", 1000, "2026-02-10"),
	}
	r := Incomes(txs, 12)
	sal, freela := r.BySource[0], r.BySource[1]
	if sal.Name != "Salário" || sal.TotalCents != 9001 || sal.Count != 3 || sal.Months != 2 || sal.LastDate != "2026-03-05" {
		t.Errorf("salário = %+v", sal)
	}
	if sal.AvgCents != 3000 { // 9001 / 3 = 3000,33
		t.Errorf("média = %d", sal.AvgCents)
	}
	if sal.Percent != 90.0 { // 9001 / 10001 = 90,00%
		t.Errorf("percentual do salário = %v", sal.Percent)
	}
	if freela.Months != 1 || freela.LastDate != "2026-02-10" || freela.Percent != 10.0 {
		t.Errorf("freela = %+v", freela)
	}
}

func TestIncomes_CategoryGroupsIgnoreCaseAndAccents(t *testing.T) {
	txs := []models.Transaction{
		in(1, "A", "Salário", 100, "2026-01-01"),
		in(2, "B", "salario", 100, "2026-02-01"),
		in(3, "C", "Outros", 500, "2026-02-02"),
	}
	r := Incomes(txs, 12)
	if len(r.ByCategory) != 2 {
		t.Fatalf("categorias = %+v", r.ByCategory)
	}
	if r.ByCategory[0].Name != "Outros" || r.ByCategory[1].Name != "Salário" || r.ByCategory[1].Count != 2 || r.ByCategory[1].Months != 2 {
		t.Errorf("categorias = %+v", r.ByCategory)
	}
}

func TestIncomes_InstallmentMarkerIsDropped(t *testing.T) {
	txs := []models.Transaction{
		in(1, "Venda do carro 1/2", "Outros", 5000, "2026-01-10"),
		in(2, "Venda do carro 2/2", "Outros", 5000, "2026-02-10"),
	}
	r := Incomes(txs, 12)
	if len(r.BySource) != 1 || r.BySource[0].Name != "Venda do carro" || r.BySource[0].Count != 2 {
		t.Errorf("fontes = %+v", r.BySource)
	}
}

func TestIncomes_OrderTotalThenCountThenName(t *testing.T) {
	txs := []models.Transaction{
		in(1, "B", "X", 100, "2026-01-01"), in(2, "B", "X", 100, "2026-01-02"), // 200, 2 vezes
		in(3, "C", "X", 200, "2026-01-03"), // 200, 1 vez
		in(4, "A", "X", 200, "2026-01-04"), // 200, 1 vez, nome antes de C
		in(5, "D", "X", 900, "2026-01-05"),
	}
	r := Incomes(txs, 12)
	var names []string
	for _, row := range r.BySource {
		names = append(names, row.Name)
	}
	want := []string{"D", "B", "A", "C"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("ordem = %v, quer %v", names, want)
		}
	}
}

func TestIncomes_LargestSourceAndSingleSourceIs100Percent(t *testing.T) {
	r := Incomes([]models.Transaction{in(1, "Salário", "Salário", 700, "2026-01-05")}, 1)
	if r.Largest == nil || r.Largest.Name != "Salário" || r.Largest.Percent != 100.0 {
		t.Errorf("maior fonte = %+v", r.Largest)
	}
}

func TestIncomes_MonthsWithIncomeAndMonthlyAverage(t *testing.T) {
	txs := []models.Transaction{
		in(1, "A", "X", 1000, "2026-01-05"),
		in(2, "B", "X", 1000, "2026-01-25"),
		in(3, "A", "X", 1001, "2026-04-05"),
	}
	r := Incomes(txs, 12)
	if r.MonthsWithIncome != 2 || r.MonthsTouched != 12 {
		t.Errorf("meses = %d de %d", r.MonthsWithIncome, r.MonthsTouched)
	}
	if !r.HasMonthly || r.AvgMonthly != 250 { // 3001 / 12 = 250,08
		t.Errorf("média mensal = %d (tem=%v)", r.AvgMonthly, r.HasMonthly)
	}

	one := Incomes(txs[:1], 1)
	if one.HasMonthly {
		t.Error("período de um mês não mostra média mensal")
	}
}

func TestIncomes_Empty(t *testing.T) {
	r := Incomes(nil, 12)
	if r.Total != 0 || r.Largest != nil || len(r.BySource) != 0 || len(r.ByCategory) != 0 || r.MonthsWithIncome != 0 || r.HasMonthly {
		t.Errorf("resultado = %+v", r)
	}
}
