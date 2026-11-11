package report

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// jsOrder reproduz a ordenação da página (report.html, setupRanked): chaves em ordem decrescente e, no empate,
// a posição original (data-i).
func jsOrder(n int, keys ...func(i int) int64) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		for _, k := range keys {
			if x, y := k(idx[a]), k(idx[b]); x != y {
				return x > y
			}
		}
		return idx[a] < idx[b]
	})
	return idx
}

func TestRecurrenceLimited_KeepsTopForEverySort(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	var rows []RecurringRow
	for i := 0; i < 500; i++ {
		rows = append(rows, RecurringRow{Name: fmt.Sprint("d", i), Count: rng.Intn(6) + 2, TotalCents: int64(rng.Intn(40)) * 100})
	}
	full := Recurrence{ByCategory: rows[:30], ByDescription: rows}
	got := full.Limited(50)

	if len(got.ByCategory) != 30 || got.CategoryTotal != 30 {
		t.Fatalf("lista curta não pode ser cortada: %d linhas, total %d", len(got.ByCategory), got.CategoryTotal)
	}
	if got.DescriptionTotal != 500 || len(got.ByDescription) > 100 || len(got.ByDescription) < 50 {
		t.Fatalf("descrições: %d linhas, total %d", len(got.ByDescription), got.DescriptionTotal)
	}
	top := func(list []RecurringRow, keys ...func(r RecurringRow) int64) []RecurringRow {
		ks := make([]func(int) int64, len(keys))
		for i, k := range keys {
			k := k
			ks[i] = func(j int) int64 { return k(list[j]) }
		}
		var out []RecurringRow
		for _, i := range jsOrder(len(list), ks...)[:50] {
			out = append(out, list[i])
		}
		return out
	}
	count := func(r RecurringRow) int64 { return int64(r.Count) }
	total := func(r RecurringRow) int64 { return r.TotalCents }
	if !reflect.DeepEqual(top(rows, count, total), top(got.ByDescription, count, total)) {
		t.Error("ordenação por vezes: as 50 primeiras mudaram")
	}
	if !reflect.DeepEqual(top(rows, total, count), top(got.ByDescription, total, count)) {
		t.Error("ordenação por total: as 50 primeiras mudaram")
	}
}

func TestIncomeLimited_KeepsTopForEverySort(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	var rows []IncomeRow
	for i := 0; i < 400; i++ {
		rows = append(rows, IncomeRow{Name: fmt.Sprint("r", i), Count: rng.Intn(5) + 1, TotalCents: int64(rng.Intn(30)) * 100, Months: rng.Intn(12) + 1})
	}
	full := IncomeReport{BySource: rows, ByCategory: rows[:5]}
	got := full.Limited(40)
	if got.SourceTotal != 400 || got.CategoryTotal != 5 || len(got.ByCategory) != 5 {
		t.Fatalf("totais: %d %d %d", got.SourceTotal, got.CategoryTotal, len(got.ByCategory))
	}
	pick := func(list []IncomeRow, keys ...func(r IncomeRow) int64) []IncomeRow {
		ks := make([]func(int) int64, len(keys))
		for i, k := range keys {
			k := k
			ks[i] = func(j int) int64 { return k(list[j]) }
		}
		var out []IncomeRow
		for _, i := range jsOrder(len(list), ks...)[:40] {
			out = append(out, list[i])
		}
		return out
	}
	total := func(r IncomeRow) int64 { return r.TotalCents }
	count := func(r IncomeRow) int64 { return int64(r.Count) }
	months := func(r IncomeRow) int64 { return int64(r.Months) }
	for name, keys := range map[string][]func(IncomeRow) int64{
		"total": {total, count, months}, "vezes": {count, total, months}, "meses": {months, total, count},
	} {
		if !reflect.DeepEqual(pick(rows, keys...), pick(got.BySource, keys...)) {
			t.Errorf("ordenação por %s: as 40 primeiras mudaram", name)
		}
	}
}
