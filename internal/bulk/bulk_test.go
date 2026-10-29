package bulk

import (
	"strings"
	"testing"

	"finpi/internal/models"
)

func tx(typ, desc, cat, date, prio string) models.Transaction {
	return models.Transaction{ID: 1, Type: typ, Description: desc, Category: cat, Date: date, Priority: prio, AmountCents: 1000}
}

func TestSpec_EmptyAndValidate(t *testing.T) {
	if !(Spec{}).Empty() || (Spec{Category: "X"}).Empty() {
		t.Error("Empty deveria ser verdadeiro só sem nenhuma alteração")
	}
	good := []Spec{
		{Category: "Lazer"},
		{Priority: PriorityFollow}, {Priority: models.PriorityEssential}, {Priority: models.PrioritySuperfluous},
		{DateMode: DateSet, DateSet: "2026-10-05"},
		{DateMode: DateShift, ShiftN: -3, ShiftUnit: UnitDays},
		{DateMode: DateShift, ShiftN: 120, ShiftUnit: UnitMonths},
		{Find: "pix", Replace: ""},
	}
	for i, s := range good {
		if err := s.Validate(); err != nil {
			t.Errorf("spec %d deveria ser válida: %v", i, err)
		}
	}
	bad := map[string]Spec{
		"prioridade":               {Priority: "urgente"},
		"data sem valor":           {DateMode: DateSet},
		"data inválida":            {DateMode: DateSet, DateSet: "2026-13-40"},
		"modo de data":             {DateMode: "x"},
		"deslocar zero":            {DateMode: DateShift, ShiftN: 0, ShiftUnit: UnitDays},
		"unidade":                  {DateMode: DateShift, ShiftN: 1, ShiftUnit: "anos"},
		"dias demais":              {DateMode: DateShift, ShiftN: 3651, ShiftUnit: UnitDays},
		"meses demais":             {DateMode: DateShift, ShiftN: -121, ShiftUnit: UnitMonths},
		"substituir sem localizar": {Replace: "x"},
		"localizar longo":          {Find: strings.Repeat("a", 101)},
		"substituir longo":         {Find: "a", Replace: strings.Repeat("b", 201)},
	}
	for name, s := range bad {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: deveria ser inválida", name)
		}
	}
}

func TestApply_CategoryAndPriority(t *testing.T) {
	got, err := Apply(tx("expense", "Mercado", "Outros", "2026-10-05", ""), Spec{Category: "Alimentação", Priority: models.PriorityEssential})
	if err != nil || got.Category != "Alimentação" || got.Priority != models.PriorityEssential {
		t.Errorf("got %+v err %v", got, err)
	}
	cleared, _ := Apply(tx("expense", "Mercado", "Outros", "2026-10-05", models.PrioritySuperfluous), Spec{Priority: PriorityFollow})
	if cleared.Priority != "" {
		t.Errorf("seguir a categoria limpa a prioridade: %q", cleared.Priority)
	}
	income, _ := Apply(tx("income", "Salário", "Outros", "2026-10-05", ""), Spec{Priority: models.PriorityEssential})
	if income.Priority != "" {
		t.Errorf("receita nunca tem prioridade: %q", income.Priority)
	}
	same, _ := Apply(tx("expense", "Mercado", "Outros", "2026-10-05", models.PrioritySuperfluous), Spec{Category: "Lazer"})
	if same.Priority != models.PrioritySuperfluous {
		t.Error("sem alterar prioridade, ela fica como estava")
	}
}

func TestApply_DateSet(t *testing.T) {
	got, err := Apply(tx("expense", "X", "Outros", "2026-01-31", ""), Spec{DateMode: DateSet, DateSet: "2026-10-05"})
	if err != nil || got.Date != "2026-10-05" {
		t.Errorf("got %q err %v", got.Date, err)
	}
}

func TestApply_DateShift(t *testing.T) {
	cases := []struct {
		from string
		n    int
		unit string
		want string
	}{
		{"2026-01-31", 1, UnitDays, "2026-02-01"},
		{"2026-03-01", -1, UnitDays, "2026-02-28"},
		{"2028-03-01", -1, UnitDays, "2028-02-29"},
		{"2026-01-31", 1, UnitMonths, "2026-02-28"},  // fim de mês
		{"2028-01-31", 1, UnitMonths, "2028-02-29"},  // ano bissexto
		{"2026-12-15", 1, UnitMonths, "2027-01-15"},  // vira o ano
		{"2026-03-15", -3, UnitMonths, "2025-12-15"}, // volta o ano
		{"2026-03-31", -1, UnitMonths, "2026-02-28"},
		{"2026-05-31", 1, UnitMonths, "2026-06-30"},
		{"2026-10-05", 12, UnitMonths, "2027-10-05"},
	}
	for _, c := range cases {
		got, err := Apply(tx("expense", "X", "Outros", c.from, ""), Spec{DateMode: DateShift, ShiftN: c.n, ShiftUnit: c.unit})
		if err != nil || got.Date != c.want {
			t.Errorf("%s %+d %s = %q (err %v), quer %q", c.from, c.n, c.unit, got.Date, err, c.want)
		}
	}
}

func TestApply_FindReplace(t *testing.T) {
	cases := []struct{ desc, find, repl, want string }{
		{"Pix RECEBIDO Fulano", "pix recebido", "Pix", "Pix Fulano"},
		{"Plano de Saúde", "SAÚDE", "Plano", "Plano de Plano"},
		{"Compra (1) loja", "(1)", "", "Compra loja"},        // caracteres de regex são literais
		{"axb a.b", "a.b", "X", "axb X"},                      // o ponto não é curinga
		{"Preço", "Preço", "$1 e $&", "$1 e $&"},              // $ é literal
		{"A  X  B", "X", "", "A B"},                           // espaços colapsados
		{"mercado mercado", "mercado", "loja", "loja loja"},   // todas as ocorrências
		{"Netflix", "inexistente", "x", "Netflix"},
	}
	for _, c := range cases {
		got, err := Apply(tx("expense", c.desc, "Outros", "2026-10-05", ""), Spec{Find: c.find, Replace: c.repl})
		if err != nil || got.Description != c.want {
			t.Errorf("%q: %q → %q = %q (err %v), quer %q", c.desc, c.find, c.repl, got.Description, err, c.want)
		}
	}
}

func TestApply_DescriptionLimits(t *testing.T) {
	if _, err := Apply(tx("expense", "Pix", "Outros", "2026-10-05", ""), Spec{Find: "pix", Replace: ""}); err == nil {
		t.Error("descrição vazia deveria ser recusada")
	}
	if _, err := Apply(tx("expense", "a", "Outros", "2026-10-05", ""), Spec{Find: "a", Replace: strings.Repeat("b", 201)}); err == nil {
		t.Error("descrição com mais de 200 caracteres deveria ser recusada")
	}
	if got, err := Apply(tx("expense", "a", "Outros", "2026-10-05", ""), Spec{Find: "a", Replace: strings.Repeat("b", 200)}); err != nil || len([]rune(got.Description)) != 200 {
		t.Errorf("200 caracteres é aceito: %v", err)
	}
}

func TestPreview_FlagsChangedUnchangedAndErrors(t *testing.T) {
	list := []models.Transaction{
		tx("expense", "Pix recebido A", "Outros", "2026-10-05", ""),
		tx("expense", "Mercado", "Outros", "2026-10-06", ""), // não contém o trecho: sem mudança
		tx("expense", "Pix", "Outros", "2026-10-07", ""),     // com Find "pix" ficaria vazia: erro
	}
	rows := Preview(list, Spec{Find: "pix recebido", Replace: ""})
	if !rows[0].Changed || rows[0].After.Description != "A" {
		t.Errorf("linha 0 = %+v", rows[0])
	}
	if rows[1].Changed || rows[1].Err != "" {
		t.Errorf("linha 1 = %+v", rows[1])
	}
	rows = Preview(list, Spec{Find: "pix", Replace: ""})
	if rows[2].Err == "" || rows[2].Changed {
		t.Errorf("linha 2 deveria ter erro: %+v", rows[2])
	}
}

func TestDiffers(t *testing.T) {
	a := tx("expense", "X", "Outros", "2026-10-05", "")
	b := a
	if Differs(a, b) {
		t.Error("iguais")
	}
	for name, mod := range map[string]func(*models.Transaction){
		"descrição":  func(x *models.Transaction) { x.Description = "Y" },
		"categoria":  func(x *models.Transaction) { x.Category = "Lazer" },
		"data":       func(x *models.Transaction) { x.Date = "2026-10-06" },
		"prioridade": func(x *models.Transaction) { x.Priority = models.PriorityEssential },
	} {
		c := a
		mod(&c)
		if !Differs(a, c) {
			t.Errorf("%s deveria diferir", name)
		}
	}
}

func TestSummary(t *testing.T) {
	cases := []struct {
		spec Spec
		want string
	}{
		{Spec{Category: "Salário", Find: "Pix recebido", Replace: "Pix"}, "categoria → Salário; descrição: 'Pix recebido' → 'Pix'"},
		{Spec{Priority: models.PriorityEssential, DateMode: DateShift, ShiftN: 1, ShiftUnit: UnitMonths}, "prioridade → essencial; data deslocada +1 mês"},
		{Spec{Priority: models.PrioritySuperfluous, DateMode: DateShift, ShiftN: -3, ShiftUnit: UnitDays}, "prioridade → supérfluo; data deslocada -3 dias"},
		{Spec{Priority: PriorityFollow, DateMode: DateSet, DateSet: "2026-10-05"}, "prioridade → seguir a categoria; data → 05/10/2026"},
	}
	for _, c := range cases {
		if got := Summary(c.spec); got != c.want {
			t.Errorf("Summary = %q, quer %q", got, c.want)
		}
	}
	long := Summary(Spec{Find: strings.Repeat("a", 100), Replace: strings.Repeat("b", 200)})
	if n := len([]rune(long)); n > 300 {
		t.Errorf("o resumo deveria ter no máximo 300 caracteres, tem %d", n)
	}
}
