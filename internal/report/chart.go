package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const (
	chartW = 960.0
	chartH = 360.0
	padL   = 64.0
	padR   = 16.0
	padT   = 16.0
	padB   = 44.0
)

type Bar struct {
	X, Y, W, H float64
	Kind       string // "income" ou "expense"
	Projected  bool
	Title      string
}

type Dot struct {
	X, Y  float64
	Title string
}

type Tick struct {
	Y     float64
	Label string
}

type XLabel struct {
	X    float64
	Text string
}

// Chart é a geometria pronta do gráfico (coordenadas de um viewBox de Width x Height).
type Chart struct {
	Width, Height            int
	Left, Right, Top, Bottom float64
	ZeroY                    float64
	Bars                     []Bar
	Line                     string // pontos da linha do saldo acumulado ("x,y x,y ...")
	Dots                     []Dot
	YTicks                   []Tick
	XLabels                  []XLabel
	Empty                    bool
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }

// CompactMoney formata centavos de forma curta para o eixo: R$ 850, R$ 1,2 mil, R$ 2,5 mi.
func CompactMoney(cents int64) string {
	sign := ""
	if cents < 0 {
		sign, cents = "-", -cents
	}
	v := float64(cents) / 100
	one := func(x float64) string {
		s := strings.Replace(strconv.FormatFloat(x, 'f', 1, 64), ".", ",", 1)
		return strings.TrimSuffix(s, ",0")
	}
	switch {
	case v >= 999_950:
		return sign + "R$ " + one(v/1_000_000) + " mi"
	case v >= 999.5:
		return sign + "R$ " + one(v/1_000) + " mil"
	}
	return sign + "R$ " + strconv.FormatInt(int64(math.Round(v)), 10)
}

func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(raw)))
	switch f := raw / exp; {
	case f <= 1:
		return exp
	case f <= 2:
		return 2 * exp
	case f <= 5:
		return 5 * exp
	}
	return 10 * exp
}

// BuildChart calcula barras (entradas e saídas por fatia, com o previsto empilhado), a linha do saldo
// acumulado, as marcas do eixo vertical e os rótulos do eixo horizontal. format formata centavos nas dicas.
func BuildChart(buckets []Bucket, format func(int64) string) Chart {
	c := Chart{Width: int(chartW), Height: int(chartH), Left: padL, Right: chartW - padR, Top: padT, Bottom: chartH - padB}
	if len(buckets) == 0 {
		c.Empty = true
		return c
	}

	lo, hi := 0.0, 0.0
	for _, b := range buckets {
		hi = max(hi, float64(b.TotalIncome()), float64(b.TotalExpense()), float64(b.Balance))
		lo = min(lo, float64(b.Balance))
	}
	step := niceStep((hi - lo) / 4)
	axisLo := math.Floor(lo/step) * step
	axisHi := math.Ceil(hi/step) * step
	if axisHi == axisLo {
		axisHi = axisLo + step
	}
	yOf := func(v float64) float64 {
		return round1(c.Top + (axisHi-v)/(axisHi-axisLo)*(c.Bottom-c.Top))
	}
	c.ZeroY = yOf(0)
	for v := axisLo; v <= axisHi+step/2; v += step {
		c.YTicks = append(c.YTicks, Tick{Y: yOf(v), Label: CompactMoney(int64(math.Round(v)))})
	}

	n := float64(len(buckets))
	slot := (c.Right - c.Left) / n
	bw := min(28, max(1.5, slot*0.35))
	gap := slot * 0.04
	offset := max(0, (slot-(2*bw+gap))/2)

	addBar := func(kind string, x float64, from, to int64, projected bool, title string) {
		if to <= from {
			return
		}
		top, bottom := yOf(float64(to)), yOf(float64(from))
		c.Bars = append(c.Bars, Bar{X: round1(x), Y: top, W: round1(bw), H: round1(bottom - top), Kind: kind, Projected: projected, Title: title})
	}

	var points []string
	maxLabel := 0
	for i, b := range buckets {
		x0 := c.Left + float64(i)*slot + offset
		incomeTitle := fmt.Sprintf("%s — entradas: %s", b.Label, format(b.TotalIncome()))
		expenseTitle := fmt.Sprintf("%s — saídas: %s", b.Label, format(b.TotalExpense()))
		if b.ProjIncome != 0 {
			incomeTitle += fmt.Sprintf(" (previstas: %s)", format(b.ProjIncome))
		}
		if b.ProjExpense != 0 {
			expenseTitle += fmt.Sprintf(" (previstas: %s)", format(b.ProjExpense))
		}
		addBar("income", x0, 0, b.Income, false, incomeTitle)
		addBar("income", x0, b.Income, b.Income+b.ProjIncome, true, incomeTitle)
		addBar("expense", x0+bw+gap, 0, b.Expense, false, expenseTitle)
		addBar("expense", x0+bw+gap, b.Expense, b.Expense+b.ProjExpense, true, expenseTitle)

		cx, cy := round1(c.Left+(float64(i)+0.5)*slot), yOf(float64(b.Balance))
		points = append(points, fmt.Sprintf("%.1f,%.1f", cx, cy))
		c.Dots = append(c.Dots, Dot{X: cx, Y: cy, Title: fmt.Sprintf("%s — saldo acumulado: %s", b.Label, format(b.Balance))})
		maxLabel = max(maxLabel, len([]rune(b.Label)))
	}
	c.Line = strings.Join(points, " ")

	every := max(1, int(math.Ceil((float64(maxLabel)*6.5+10)/slot)))
	for i, b := range buckets {
		if i%every == 0 {
			c.XLabels = append(c.XLabels, XLabel{X: round1(c.Left + (float64(i)+0.5)*slot), Text: b.Label})
		}
	}
	return c
}
