package handlers

import (
	"strings"
	"testing"
)

func TestMonthNav_ArrowsGoToPreviousAndNextMonth(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/?month=2026-10")
	for _, want := range []string{`href="/?livro=1&amp;month=2026-09"`, `href="/?livro=1&amp;month=2026-11"`, `aria-label="Mês anterior"`, `aria-label="Próximo mês"`} {
		if !strings.Contains(body, want) {
			t.Errorf("deveria conter %q", want)
		}
	}
}

func TestMonthNav_YearBoundaries(t *testing.T) {
	h, _ := newTestHandler(t)
	jan := getDashboard(h, "/?month=2026-01")
	if !strings.Contains(jan, `month=2025-12"`) {
		t.Error("em janeiro, ‹ deveria ir para dezembro do ano anterior")
	}
	dec := getDashboard(h, "/?month=2026-12")
	if !strings.Contains(dec, `month=2027-01"`) {
		t.Error("em dezembro, › deveria ir para janeiro do ano seguinte")
	}
}

func TestMonthNav_LimitsDisableArrow(t *testing.T) {
	h, _ := newTestHandler(t)
	first := getDashboard(h, "/?month=2000-01")
	if strings.Contains(first, `aria-label="Mês anterior"`) || !strings.Contains(first, `aria-label="Próximo mês"`) {
		t.Error("em 2000-01 só a seta para frente funciona")
	}
	last := getDashboard(h, "/?month=2100-12")
	if strings.Contains(last, `aria-label="Próximo mês"`) || !strings.Contains(last, `aria-label="Mês anterior"`) {
		t.Error("em 2100-12 só a seta para trás funciona")
	}
}

func TestMonthNav_KeepsBookProjectedAndFilters(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	acc, err := pj.CreateAccount("Nubank", "nubank")
	if err != nil {
		t.Fatal(err)
	}
	body := getDashboard(h, "/?month=2026-10&livro="+pjID+"&previstas=1&cat=Outros&q=pix&conta="+itoa(acc.ID))
	i := strings.Index(body, `aria-label="Próximo mês"`)
	if i < 0 {
		t.Fatal("sem seta")
	}
	start := strings.LastIndex(body[:i], `href="`)
	link := body[start:i]
	for _, want := range []string{"month=2026-11", "livro=" + pjID, "previstas=1", "cat=Outros", "q=pix", "conta=" + itoa(acc.ID)} {
		if !strings.Contains(link, want) {
			t.Errorf("o link da seta deveria conter %q: %s", want, link)
		}
	}
}

func TestMonthNav_HiddenWhenSearchingAllMonths(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/?month=2026-10&todos=1")
	if strings.Contains(body, `aria-label="Mês anterior"`) || strings.Contains(body, `aria-label="Próximo mês"`) {
		t.Error("com Todos os meses não há mês para navegar")
	}
}

func TestMonthNav_InvalidMonthHasNoArrows(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/?month=abc")
	if strings.Contains(body, `aria-label="Mês anterior"`) || strings.Contains(body, `aria-label="Próximo mês"`) {
		t.Error("mês inválido não deveria ter setas")
	}
}
