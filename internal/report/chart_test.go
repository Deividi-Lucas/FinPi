package report

import (
	"strconv"
	"strings"
	"testing"
)

func brl(c int64) string { return "R$" + strconv.FormatInt(c, 10) }

func TestCompactMoney(t *testing.T) {
	cases := map[int64]string{
		0:         "R$ 0",
		85000:     "R$ 850",
		100000:    "R$ 1 mil",
		123456:    "R$ 1,2 mil",
		-123456:   "-R$ 1,2 mil",
		100000000: "R$ 1 mi",
		250000000: "R$ 2,5 mi",
		5000000:   "R$ 50 mil",
	}
	for in, want := range cases {
		if got := CompactMoney(in); got != want {
			t.Errorf("CompactMoney(%d) = %q, quer %q", in, got, want)
		}
	}
}

func buckets3() []Bucket {
	return []Bucket{
		{Label: "jan/26", Income: 100000, Expense: 40000, Balance: 60000},
		{Label: "fev/26", Income: 100000, Expense: 150000, ProjExpense: 20000, Balance: 10000 - 20000},
		{Label: "mar/26", Income: 0, Expense: 0, Balance: -10000},
	}
}

func TestBuildChart_EmptyBuckets(t *testing.T) {
	if c := BuildChart(nil, brl); !c.Empty {
		t.Error("sem fatias o gráfico deveria ser vazio")
	}
}

func TestBuildChart_BarsAndProjectedSegments(t *testing.T) {
	c := BuildChart(buckets3(), brl)
	var income, expense, projected int
	for _, b := range c.Bars {
		switch {
		case b.Projected:
			projected++
		case b.Kind == "income":
			income++
		case b.Kind == "expense":
			expense++
		}
		if b.W < 1.5 || b.H <= 0 {
			t.Errorf("barra inválida: %+v", b)
		}
	}
	// jan: 1 entrada + 1 saída; fev: 1 entrada + 1 saída + 1 trecho previsto; mar: nada.
	if income != 2 || expense != 2 || projected != 1 {
		t.Errorf("barras: entradas=%d saídas=%d previstas=%d", income, expense, projected)
	}
	if len(c.Dots) != 3 {
		t.Errorf("pontos do saldo = %d, quer 3", len(c.Dots))
	}
	if strings.Count(c.Line, ",") != 3 {
		t.Errorf("linha = %q", c.Line)
	}
}

func TestBuildChart_TitlesUseFormatter(t *testing.T) {
	c := BuildChart(buckets3(), brl)
	found := false
	for _, b := range c.Bars {
		// Saídas de fevereiro: 150000 realizadas + 20000 previstas = 170000 no total.
		if strings.Contains(b.Title, "fev/26") && strings.Contains(b.Title, "R$170000") && strings.Contains(b.Title, "previstas: R$20000") {
			found = true
		}
	}
	if !found {
		t.Error("a dica da barra de saídas de fevereiro deveria trazer o total e as previstas formatados")
	}
	if !strings.Contains(c.Dots[2].Title, "R$-10000") {
		t.Errorf("dica do saldo = %q", c.Dots[2].Title)
	}
}

func TestBuildChart_ZeroLineAndNegativeBalance(t *testing.T) {
	positive := BuildChart([]Bucket{{Label: "a", Income: 100, Balance: 100}, {Label: "b", Expense: 50, Balance: 50}}, brl)
	if positive.ZeroY != positive.Bottom {
		t.Errorf("sem saldo negativo o zero fica na base: ZeroY=%v Bottom=%v", positive.ZeroY, positive.Bottom)
	}

	c := BuildChart(buckets3(), brl)
	if c.ZeroY >= c.Bottom {
		t.Errorf("com saldo negativo o zero sobe: ZeroY=%v Bottom=%v", c.ZeroY, c.Bottom)
	}
	last := c.Dots[2]
	if last.Y <= c.ZeroY {
		t.Errorf("saldo negativo deveria ficar abaixo da linha do zero: ponto=%v zero=%v", last.Y, c.ZeroY)
	}
	for _, b := range c.Bars {
		if b.Y < c.Top-0.1 || b.Y+b.H > c.ZeroY+0.1 {
			t.Errorf("barra fora da área (acima do zero): %+v zero=%v", b, c.ZeroY)
		}
	}
}

func TestBuildChart_TicksCoverRangeWithZero(t *testing.T) {
	c := BuildChart(buckets3(), brl)
	if len(c.YTicks) < 3 || len(c.YTicks) > 9 {
		t.Fatalf("marcas = %+v", c.YTicks)
	}
	hasZero := false
	for _, tk := range c.YTicks {
		if tk.Label == "R$ 0" {
			hasZero = true
		}
		if tk.Y < c.Top-0.1 || tk.Y > c.Bottom+0.1 {
			t.Errorf("marca fora da área: %+v", tk)
		}
	}
	if !hasZero {
		t.Error("deveria haver uma marca no zero")
	}
}

func TestBuildChart_ThinsXLabelsWhenCrowded(t *testing.T) {
	var many []Bucket
	for i := 0; i < 93; i++ {
		many = append(many, Bucket{Label: "03/08", Income: 100, Balance: int64(i)})
	}
	c := BuildChart(many, brl)
	if len(c.XLabels) >= 93 || len(c.XLabels) < 5 {
		t.Errorf("rótulos = %d, quer entre 5 e 92", len(c.XLabels))
	}
	for i := 1; i < len(c.XLabels); i++ {
		if c.XLabels[i].X-c.XLabels[i-1].X < 30 {
			t.Errorf("rótulos sobrepostos: %v e %v", c.XLabels[i-1].X, c.XLabels[i].X)
		}
	}

	few := BuildChart(buckets3(), brl)
	if len(few.XLabels) != 3 {
		t.Errorf("poucas fatias mostram todos os rótulos: %d", len(few.XLabels))
	}
}

func TestBuildChart_SingleBucketBarsStayNarrow(t *testing.T) {
	c := BuildChart([]Bucket{{Label: "jan/26", Income: 100, Expense: 50, Balance: 50}}, brl)
	for _, b := range c.Bars {
		if b.W > 30 {
			t.Errorf("barra larga demais: %+v", b)
		}
	}
}
