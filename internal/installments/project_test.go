package installments

import (
	"testing"

	"finpi/internal/models"
)

func tx(id int64, desc, typ, date string, cents int64) models.Transaction {
	return models.Transaction{ID: id, Description: desc, Type: typ, Date: date, AmountCents: cents}
}

func only(t *testing.T, got []models.ProjectedInstallment) models.ProjectedInstallment {
	t.Helper()
	if len(got) != 1 {
		t.Fatalf("esperava 1 parcela prevista, veio %d: %+v", len(got), got)
	}
	return got[0]
}

func TestProject_NextMonthsOfASeries(t *testing.T) {
	list := []models.Transaction{tx(1, "Geladeira 3/10", "expense", "2026-10-15", 25000)}

	p := only(t, Project(list, "2026-11"))
	if p.Name != "Geladeira" || p.Number != 4 || p.Total != 10 || p.Date != "2026-11-15" || p.AmountCents != 25000 || p.Type != "expense" {
		t.Errorf("parcela prevista errada: %+v", p)
	}
	if p := only(t, Project(list, "2026-12")); p.Number != 5 || p.Date != "2026-12-15" {
		t.Errorf("dezembro deveria ter a 5/10: %+v", p)
	}
	if got := Project(list, "2026-10"); len(got) != 0 {
		t.Errorf("o mês da própria parcela não projeta nada: %+v", got)
	}
}

func TestProject_StopsAtLastInstallment(t *testing.T) {
	done := []models.Transaction{tx(1, "Geladeira 10/10", "expense", "2026-10-15", 25000)}
	if got := Project(done, "2026-11"); len(got) != 0 {
		t.Errorf("série concluída não projeta: %+v", got)
	}

	nearEnd := []models.Transaction{tx(1, "Geladeira 9/10", "expense", "2026-10-15", 25000)}
	if p := only(t, Project(nearEnd, "2026-11")); p.Number != 10 {
		t.Errorf("deveria prever a 10/10: %+v", p)
	}
	if got := Project(nearEnd, "2026-12"); len(got) != 0 {
		t.Errorf("depois da 10/10 não há mais nada: %+v", got)
	}
}

func TestProject_ClampsDayInShortMonths(t *testing.T) {
	list := []models.Transaction{tx(1, "Curso 1/3", "expense", "2026-01-31", 10000)}

	if p := only(t, Project(list, "2026-02")); p.Date != "2026-02-28" {
		t.Errorf("fevereiro = %s, quer 2026-02-28", p.Date)
	}
	if p := only(t, Project(list, "2026-03")); p.Date != "2026-03-31" {
		t.Errorf("março = %s, quer 2026-03-31 (volta ao dia original)", p.Date)
	}
}

func TestProject_HighestNumberWins(t *testing.T) {
	list := []models.Transaction{
		tx(1, "Sofá 3/5", "expense", "2026-11-10", 50000),
		tx(2, "Sofá 2/5", "expense", "2026-10-10", 50000),
	}
	if p := only(t, Project(list, "2026-12")); p.Number != 4 || p.Date != "2026-12-10" {
		t.Errorf("deveria partir da 3/5: %+v", p)
	}
	if got := Project(list, "2026-11"); len(got) != 0 {
		t.Errorf("a 3/5 já foi lançada em novembro: %+v", got)
	}
}

func TestProject_PostedInstallmentReplacesForecast(t *testing.T) {
	before := []models.Transaction{tx(1, "Geladeira 3/10", "expense", "2026-10-15", 25000)}
	if got := Project(before, "2026-11"); len(got) != 1 {
		t.Fatalf("antes de lançar a 4/10 ela é prevista: %+v", got)
	}

	after := append(before, tx(2, "Geladeira 4/10", "expense", "2026-11-15", 25000))
	if got := Project(after, "2026-11"); len(got) != 0 {
		t.Errorf("a 4/10 lançada não pode aparecer como prevista: %+v", got)
	}
	if p := only(t, Project(after, "2026-12")); p.Number != 5 {
		t.Errorf("dezembro passa a ter a 5/10: %+v", p)
	}
}

func TestProject_GapDoesNotCreateRetroactiveForecast(t *testing.T) {
	list := []models.Transaction{
		tx(1, "Geladeira 3/10", "expense", "2026-10-15", 25000),
		tx(2, "Geladeira 5/10", "expense", "2026-12-15", 25000),
	}
	if got := Project(list, "2026-11"); len(got) != 0 {
		t.Errorf("novembro não ganha uma 4/10 retroativa: %+v", got)
	}
	if p := only(t, Project(list, "2027-01")); p.Number != 6 {
		t.Errorf("janeiro deveria ter a 6/10: %+v", p)
	}
}

func TestProject_MissedInstallmentShowsInItsMonth(t *testing.T) {
	list := []models.Transaction{tx(1, "Geladeira 3/10", "expense", "2026-08-15", 25000)}

	if p := only(t, Project(list, "2026-09")); p.Number != 4 {
		t.Errorf("setembro: %+v", p)
	}
	if p := only(t, Project(list, "2026-10")); p.Number != 5 {
		t.Errorf("outubro: %+v", p)
	}
}

func TestProject_SeparatesSeriesByTotalAndIgnoresPlainTransactions(t *testing.T) {
	list := []models.Transaction{
		tx(1, "Geladeira 3/10", "expense", "2026-10-15", 25000),
		tx(2, "Geladeira 1/12", "expense", "2026-10-20", 9000),
		tx(3, "Mercado", "expense", "2026-10-05", 4590),
		tx(4, "Conta 5/3", "expense", "2026-10-06", 100),
	}
	got := Project(list, "2026-11")
	if len(got) != 2 {
		t.Fatalf("esperava 2 séries projetadas, veio %d: %+v", len(got), got)
	}
	if got[0].Total != 10 || got[1].Total != 12 {
		t.Errorf("ordenação por data/nome inesperada: %+v", got)
	}
}

func TestProject_CarriesTheCategoryOfTheLatestInstallment(t *testing.T) {
	first := tx(1, "Geladeira 2/10", "expense", "2026-09-15", 25000)
	first.Category = "Moradia"
	latest := tx(2, "Geladeira 3/10", "expense", "2026-10-15", 25000)
	latest.Category = "Casa"

	if p := only(t, Project([]models.Transaction{first, latest}, "2026-11")); p.Category != "Casa" {
		t.Errorf("categoria = %q, quer a da parcela mais recente (Casa)", p.Category)
	}
}
