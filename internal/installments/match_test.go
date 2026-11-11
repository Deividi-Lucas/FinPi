package installments

import (
	"testing"

	"finpi/internal/models"
)

func proj(name string, n, total int, typ, date string, cents, account int64) models.ProjectedInstallment {
	return models.ProjectedInstallment{Name: name, Number: n, Total: total, Type: typ, Date: date, AmountCents: cents, AccountID: account, Category: "Casa"}
}

func TestMatch_SameValueTypeAndNearbyDate(t *testing.T) {
	p := []models.ProjectedInstallment{proj("Geladeira", 4, 10, "expense", "2026-11-15", 25000, 0)}
	rows := []Row{
		{Date: "2026-11-18", Type: "expense", AmountCents: 25000},
		{Date: "2026-11-15", Type: "expense", AmountCents: 9900},
	}

	got := Match(p, rows, 0)
	if got[0] != 0 {
		t.Errorf("linha 0 deveria casar com a prevista 0: %v", got)
	}
	if got[1] != -1 {
		t.Errorf("valor diferente não casa: %v", got)
	}
}

func TestMatch_RejectsFarDateOtherTypeAndOtherAccount(t *testing.T) {
	p := []models.ProjectedInstallment{proj("Geladeira", 4, 10, "expense", "2026-11-15", 25000, 7)}
	cases := map[string]struct {
		row     Row
		account int64
	}{
		"6 dias depois": {Row{Date: "2026-11-21", Type: "expense", AmountCents: 25000}, 7},
		"6 dias antes":  {Row{Date: "2026-11-09", Type: "expense", AmountCents: 25000}, 7},
		"receita":       {Row{Date: "2026-11-15", Type: "income", AmountCents: 25000}, 7},
		"outra conta":   {Row{Date: "2026-11-15", Type: "expense", AmountCents: 25000}, 8},
		"data inválida": {Row{Date: "xx", Type: "expense", AmountCents: 25000}, 7},
		"linha travada": {Row{Date: "2026-11-15", Type: "expense", AmountCents: 25000, Skip: true}, 7},
	}
	for name, c := range cases {
		if got := Match(p, []Row{c.row}, c.account); got[0] != -1 {
			t.Errorf("%s: não deveria casar: %v", name, got)
		}
	}
	// Conta desconhecida de um dos lados não impede.
	if got := Match(p, []Row{{Date: "2026-11-20", Type: "expense", AmountCents: 25000}}, 0); got[0] != 0 {
		t.Errorf("sem conta na importação deveria casar: %v", got)
	}
	if got := Match(p, []Row{{Date: "2026-11-10", Type: "expense", AmountCents: 25000}}, 7); got[0] != 0 {
		t.Errorf("5 dias antes, mesma conta, deveria casar: %v", got)
	}
}

func TestMatch_CrossesMonthBoundary(t *testing.T) {
	p := []models.ProjectedInstallment{proj("Sofá", 2, 6, "expense", "2026-11-30", 50000, 0)}
	if got := Match(p, []Row{{Date: "2026-12-02", Type: "expense", AmountCents: 50000}}, 0); got[0] != 0 {
		t.Errorf("deveria casar atravessando o mês: %v", got)
	}
}

func TestMatch_AmbiguityLeavesRowsAlone(t *testing.T) {
	// Duas previstas iguais: a linha não sabe qual é.
	two := []models.ProjectedInstallment{
		proj("Geladeira", 4, 10, "expense", "2026-11-15", 25000, 0),
		proj("Fogão", 2, 5, "expense", "2026-11-16", 25000, 0),
	}
	if got := Match(two, []Row{{Date: "2026-11-15", Type: "expense", AmountCents: 25000}}, 0); got[0] != -1 {
		t.Errorf("duas previstas possíveis: não deveria casar: %v", got)
	}

	// Duas linhas iguais para uma prevista: nenhuma recebe.
	one := two[:1]
	rows := []Row{
		{Date: "2026-11-14", Type: "expense", AmountCents: 25000},
		{Date: "2026-11-16", Type: "expense", AmountCents: 25000},
	}
	if got := Match(one, rows, 0); got[0] != -1 || got[1] != -1 {
		t.Errorf("duas linhas possíveis: nenhuma deveria casar: %v", got)
	}
}

func TestMatch_EachProjectionOnce(t *testing.T) {
	p := []models.ProjectedInstallment{
		proj("Geladeira", 4, 10, "expense", "2026-11-15", 25000, 0),
		proj("Geladeira", 5, 10, "expense", "2026-12-15", 25000, 0),
	}
	rows := []Row{
		{Date: "2026-11-15", Type: "expense", AmountCents: 25000},
		{Date: "2026-12-14", Type: "expense", AmountCents: 25000},
	}
	got := Match(p, rows, 0)
	if got[0] != 0 || got[1] != 1 {
		t.Errorf("cada mês com a sua parcela: %v", got)
	}
}

func TestDescription(t *testing.T) {
	if got := Description(proj("Geladeira", 4, 10, "expense", "2026-11-15", 1, 0)); got != "Geladeira 4/10" {
		t.Errorf("Description = %q", got)
	}
}
