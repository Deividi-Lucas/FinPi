package installments

import (
	"sort"

	"finpi/internal/models"
)

// PartialSeries devolve os nomes das séries de parcelas que ficariam divididas por uma movimentação: parte das
// parcelas lançadas (all) está em moved e parte fica para trás.
func PartialSeries(moved, all []models.Transaction) []string {
	total := map[string]int{}
	names := map[string]string{}
	for _, t := range all {
		if info, ok := Parse(t.Description); ok {
			k := Key(info, t.Type)
			total[k]++
			names[k] = info.Name
		}
	}
	movedCount := map[string]int{}
	for _, t := range moved {
		if info, ok := Parse(t.Description); ok {
			movedCount[Key(info, t.Type)]++
		}
	}
	var out []string
	for k, n := range movedCount {
		if n > 0 && n < total[k] {
			out = append(out, names[k])
		}
	}
	sort.Strings(out)
	return out
}
