package transfer

import (
	"testing"

	"finpi/internal/models"
)

func tx(id int64, typ string, cents int64, category string) models.Transaction {
	return models.Transaction{ID: id, Type: typ, AmountCents: cents, Category: category}
}

func TestBuildOverview_SubtractsLinkedAndSolo(t *testing.T) {
	pessoal := BookInput{Book: models.Book{ID: 1, Name: "Pessoal"}, Txs: []models.Transaction{
		tx(1, "income", 10000, "Salário"),
		tx(2, "expense", 2000, "Mercado"),
		tx(3, "expense", 3000, "Transferência"), // ligada à entrada 4
	}}
	pj := BookInput{Book: models.Book{ID: 2, Name: "PJ"}, Txs: []models.Transaction{
		tx(4, "income", 3000, "Receita"), // ligada à saída 3
		tx(5, "expense", 1000, "Impostos"),
		tx(6, "expense", 500, "Outros"),               // fora do FinPi
		tx(7, "income", 999, models.InternalCategory), // movimentação interna
	}}
	o := BuildOverview([]BookInput{pessoal, pj}, map[int64]bool{3: true, 4: true}, map[int64]bool{6: true})

	if len(o.Rows) != 2 {
		t.Fatalf("linhas = %d", len(o.Rows))
	}
	if s := o.Rows[0].Summary; s.TotalIncome != 10000 || s.TotalExpense != 5000 || s.Balance != 5000 {
		t.Errorf("Pessoal = %+v", s)
	}
	if s := o.Rows[1].Summary; s.TotalIncome != 3000 || s.TotalExpense != 1500 || s.Balance != 1500 {
		t.Errorf("PJ = %+v (a movimentação interna fica de fora)", s)
	}
	if o.LinkedIn != 3000 || o.LinkedOut != 3000 || o.LinkedSides != 2 {
		t.Errorf("ligadas: in %d out %d lados %d", o.LinkedIn, o.LinkedOut, o.LinkedSides)
	}
	if o.SoloOut != 500 || o.SoloIn != 0 || o.SoloSides != 1 {
		t.Errorf("fora do FinPi: in %d out %d lados %d", o.SoloIn, o.SoloOut, o.SoloSides)
	}
	if o.InternalIn != 999 || o.InternalOut != 0 {
		t.Errorf("internas: in %d out %d", o.InternalIn, o.InternalOut)
	}
	// total = soma dos livros − ligadas − fora do FinPi
	if o.Total.TotalIncome != 10000 || o.Total.TotalExpense != 3000 || o.Total.Balance != 7000 {
		t.Errorf("Total = %+v", o.Total)
	}
}

func TestBuildOverview_OneSideOutsideThePeriodOnlySubtractsTheSideInside(t *testing.T) {
	// só a saída (3) está no período; a entrada (4) é de outro mês e não aparece aqui
	pessoal := BookInput{Book: models.Book{ID: 1}, Txs: []models.Transaction{tx(3, "expense", 3000, "X")}}
	pj := BookInput{Book: models.Book{ID: 2}}
	o := BuildOverview([]BookInput{pessoal, pj}, map[int64]bool{3: true, 4: true}, nil)
	if o.LinkedOut != 3000 || o.LinkedIn != 0 || o.LinkedSides != 1 || o.Total.TotalExpense != 0 {
		t.Errorf("Overview = %+v", o)
	}
}

func TestBuildOverview_Empty(t *testing.T) {
	o := BuildOverview(nil, nil, nil)
	if len(o.Rows) != 0 || o.Total.Balance != 0 {
		t.Errorf("Overview vazia = %+v", o)
	}
}
