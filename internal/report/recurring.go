package report

import (
	"math"
	"sort"
	"strings"

	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/textutil"
)

// RecurringRow é um grupo de despesas que se repetiram no período.
type RecurringRow struct {
	Name       string
	Count      int
	TotalCents int64
	AvgCents   int64
	Percent    float64 // do total de despesas lançadas no período, com uma casa decimal
}

type Recurrence struct {
	ByCategory    []RecurringRow
	ByDescription []RecurringRow // só grupos com 2 ou mais lançamentos
	ExpenseTotal  int64

	// Linhas antes de Limited (0 se não foi chamada): o "Ver todas" informa quantas existem.
	CategoryTotal, DescriptionTotal int
}

// group acumula um grupo. Grafias e meses de um grupo são poucos, então ficam em fatias (busca linear) em vez
// de mapas: com milhares de descrições distintas, dois mapas por grupo pesavam mais que o resto do relatório.
type group struct {
	count  int
	total  int64
	order  []string // grafias na ordem em que apareceram (por data, depois ID)
	counts []int    // vezes de cada grafia (mesma posição de order)
	months []string // meses (AAAA-MM) com ao menos um lançamento
	last   string   // data (AAAA-MM-DD) do último lançamento
}

func (g *group) add(spelling string, cents int64, date string) {
	i := indexOf(g.order, spelling)
	if i < 0 {
		g.order = append(g.order, spelling)
		g.counts = append(g.counts, 0)
		i = len(g.order) - 1
	}
	g.counts[i]++
	g.count++
	g.total += cents
	if len(date) >= 7 && indexOf(g.months, date[:7]) < 0 {
		g.months = append(g.months, date[:7])
	}
	if date > g.last {
		g.last = date
	}
}

func indexOf(list []string, s string) int {
	for i := range list {
		if list[i] == s {
			return i
		}
	}
	return -1
}

// name devolve a grafia mais frequente; em empate, a que apareceu primeiro.
func (g *group) name() string {
	best, bestN := "", 0
	for i, s := range g.order {
		if g.counts[i] > bestN {
			best, bestN = s, g.counts[i]
		}
	}
	return best
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Recurring agrupa as despesas por categoria e por descrição (sem o número da parcela).
func Recurring(txs []models.Transaction) Recurrence {
	expenses, total := chronological(txs, "expense")
	cats := map[string]*group{}
	descs := map[string]*group{}
	for _, i := range expenses {
		t := &txs[i]
		cat := collapse(t.Category)
		addTo(cats, textutil.FoldCached(cat), cat, t.AmountCents, t.Date)

		name := collapse(t.Description)
		if info, ok := installments.Parse(t.Description); ok {
			name = collapse(info.Name)
		}
		addTo(descs, textutil.Fold(name), name, t.AmountCents, t.Date)
	}

	r := Recurrence{ExpenseTotal: total}
	r.ByCategory = rows(cats, total, 1)
	r.ByDescription = rows(descs, total, 2)
	return r
}

// chronological devolve os índices dos lançamentos do tipo, por data e depois ID, e a soma deles. Índices em vez
// de cópias: cada lançamento tem ~250 bytes e o período pode ter dezenas de milhares. Lista já ordenada (o caso
// comum, vinda do banco) não é reordenada.
func chronological(txs []models.Transaction, typ string) ([]int, int64) {
	var idx []int
	var total int64
	for i := range txs {
		if txs[i].Type == typ {
			idx = append(idx, i)
			total += txs[i].AmountCents
		}
	}
	less := func(i, j int) bool {
		a, b := &txs[idx[i]], &txs[idx[j]]
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.ID < b.ID
	}
	if !sort.SliceIsSorted(idx, less) {
		sort.SliceStable(idx, less)
	}
	return idx, total
}

func addTo(m map[string]*group, key, spelling string, cents int64, date string) {
	g := m[key]
	if g == nil {
		g = &group{}
		m[key] = g
	}
	g.add(spelling, cents, date)
}

func rows(m map[string]*group, total int64, minCount int) []RecurringRow {
	var out []RecurringRow
	for _, g := range m {
		if g.count < minCount {
			continue
		}
		row := RecurringRow{
			Name: g.name(), Count: g.count, TotalCents: g.total,
			AvgCents: avgCents(g.total, g.count),
		}
		row.Percent = percentOf(g.total, total)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].TotalCents != out[j].TotalCents {
			return out[i].TotalCents > out[j].TotalCents
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// avgCents devolve total ÷ count arredondado (meio para cima), em centavos.
func avgCents(total int64, count int) int64 {
	return (total*2 + int64(count)) / (2 * int64(count))
}

// percentOf devolve a fatia de part em total, em %, com uma casa decimal (0 quando total é 0).
func percentOf(part, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return math.Round(float64(part)*1000/float64(total)) / 10
}
