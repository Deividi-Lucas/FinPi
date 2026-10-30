package handlers

import (
	"strings"
	"testing"

	"finpi/internal/dashboard"
	"finpi/internal/models"
)

func TestDashboard_InternalMovementIsOutOfTotalsButInTheList(t *testing.T) {
	h, db := newTestHandler(t)
	if _, err := db.CreateCategory(models.InternalCategory); err != nil {
		t.Fatal(err)
	}
	mustTx(t, db, "Salário", "Salário", "income", 100000, "2026-10-05")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 20000, "2026-10-06")
	mustTx(t, db, "Resgate RDB", models.InternalCategory, "income", 93365, "2026-10-07")
	mustTx(t, db, "Pix enviado Maria", models.InternalCategory, "expense", 93365, "2026-10-07")

	body := getDashboard(h, "/?month=2026-10")
	if !strings.Contains(body, "R$ 1.000,00") || !strings.Contains(body, "R$ 200,00") || !strings.Contains(body, "R$ 800,00") {
		t.Errorf("os cartões deveriam somar só os lançamentos regulares (1.000,00 / 200,00 / 800,00)")
	}
	if strings.Contains(body, "R$ 1.933,65") || strings.Contains(body, "R$ 1.133,65") {
		t.Error("o resgate vazou para os totais")
	}
	if !strings.Contains(body, "Movimentações internas") || !strings.Contains(body, "R$ 933,65") {
		t.Error("deveria mostrar a linha de movimentações internas")
	}
	if list := history(body); !strings.Contains(list, "Resgate RDB") || !strings.Contains(list, "interna") {
		t.Error("a lista deveria mostrar o lançamento interno, com o selo")
	}
}

func TestDashboard_NoInternalLineWhenThereAreNone(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Mercado", "Alimentação", "expense", 20000, "2026-10-06")
	if body := getDashboard(h, "/?month=2026-10"); strings.Contains(body, "Movimentações internas") {
		t.Error("sem movimentações internas a linha não deve aparecer")
	}
}

func TestReport_InternalMovementIsOutOfTheFlow(t *testing.T) {
	h, db := newTestHandler(t)
	mustTx(t, db, "Salário", "Salário", "income", 100000, "2026-10-05")
	mustTx(t, db, "Resgate RDB", models.InternalCategory, "income", 93365, "2026-10-07")
	_, body := getReport(h, "/relatorio?ano=2026")
	if strings.Contains(body, "1.933,65") {
		t.Error("o resgate vazou para os totais do relatório")
	}
	if !strings.Contains(body, "Movimentações internas") {
		t.Error("o relatório deveria mostrar a linha de movimentações internas")
	}
}

// Sem lançamentos internos, o resumo calculado em Go dá o mesmo resultado que o SQL de antes.
func TestSummarizeMatchesSQLSummaryWithoutInternal(t *testing.T) {
	_, db := newTestHandler(t)
	mustTx(t, db, "Salário", "Salário", "income", 100000, "2026-10-05")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 20055, "2026-10-06")
	want, err := db.GetSummaryByMonth("2026-10")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := db.GetTransactions("2026-10")
	regular, _, _ := dashboard.SplitInternal(list)
	if got := dashboard.Summarize(regular); got != want {
		t.Errorf("Summarize = %+v, SQL = %+v", got, want)
	}
}
