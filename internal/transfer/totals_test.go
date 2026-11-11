package transfer

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"finpi/internal/models"
)

// groupTxs faz em Go o mesmo GROUP BY category, type, priority de database.GroupsBetween.
func groupTxs(txs []models.Transaction) []models.MonthGroup {
	idx := map[[3]string]int{}
	var out []models.MonthGroup
	for _, t := range txs {
		k := [3]string{t.Category, t.Type, t.Priority}
		i, ok := idx[k]
		if !ok {
			i = len(out)
			idx[k] = i
			out = append(out, models.MonthGroup{Category: t.Category, Type: t.Type, Priority: t.Priority})
		}
		out[i].Count++
		out[i].Cents += t.AmountCents
	}
	sort.Slice(out, func(a, b int) bool { return fmt.Sprint(out[a]) < fmt.Sprint(out[b]) })
	return out
}

func TestBuildOverviewFromTotals_MatchesBuildOverview(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	books := []models.Book{{ID: 1, Name: "Pessoal"}, {ID: 2, Name: "PJ"}}
	cats := []string{"Lazer", "Outros", models.InternalCategory, "investimento", "Salário"}
	const from, to = "2026-11-01", "2026-11-30"
	var all []models.Transaction
	for i := 1; i <= 400; i++ {
		typ := "expense"
		if rng.Intn(3) == 0 {
			typ = "income"
		}
		date := fmt.Sprintf("2026-11-%02d", rng.Intn(30)+1)
		if i%7 == 0 {
			date = "2026-10-31" // fora do período: só conta se o filtro de data falhar
		}
		all = append(all, models.Transaction{ID: int64(i), BookID: int64(rng.Intn(2) + 1), Type: typ, Date: date,
			Category: cats[rng.Intn(len(cats))], AmountCents: int64(rng.Intn(900) + 1)})
	}
	linked, solo := map[int64]bool{}, map[int64]bool{}
	var sides []models.Transaction
	for _, tx := range all {
		switch rng.Intn(10) {
		case 0:
			linked[tx.ID] = true
			sides = append(sides, tx)
		case 1:
			solo[tx.ID] = true
			sides = append(sides, tx, tx) // repetido (ex.: nas duas listas): não pode contar duas vezes
		case 2:
			linked[tx.ID], solo[tx.ID] = true, true
			sides = append(sides, tx)
		}
	}

	var inputs []BookInput
	var totals []BookTotals
	for _, b := range books {
		var period []models.Transaction
		for _, tx := range all {
			if tx.BookID == b.ID && tx.Date >= from && tx.Date <= to {
				period = append(period, tx)
			}
		}
		inputs = append(inputs, BookInput{Book: b, Txs: period})
		totals = append(totals, BookTotals{Book: b, Groups: groupTxs(period)})
	}

	want := BuildOverview(inputs, linked, solo)
	got := BuildOverviewFromTotals(totals, from, to, sides, linked, solo)
	if !reflect.DeepEqual(want, got) {
		t.Errorf("difere:\nquer %+v\ntem  %+v", want, got)
	}
	if want.LinkedSides == 0 || want.SoloSides == 0 || want.InternalIn+want.InternalOut == 0 {
		t.Fatalf("dados do teste não cobrem ligados/solo/internos: %+v", want)
	}
}
