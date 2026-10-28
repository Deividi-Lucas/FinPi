package installments

import (
	"sort"
	"time"

	"finpi/internal/models"
)

type lastParcel struct {
	info Info
	tx   models.Transaction
}

// Project devolve as parcelas previstas para o mês (AAAA-MM), a partir dos lançamentos candidatos.
// Cada série parte da parcela de maior número já lançada e segue mês a mês até N/N.
func Project(candidates []models.Transaction, month string) []models.ProjectedInstallment {
	latest := make(map[string]lastParcel)
	for _, t := range candidates {
		info, ok := Parse(t.Description)
		if !ok {
			continue
		}
		key := Key(info, t.Type)
		cur, seen := latest[key]
		if !seen || isLater(info, t, cur) {
			latest[key] = lastParcel{info: info, tx: t}
		}
	}

	var out []models.ProjectedInstallment
	for _, l := range latest {
		for k := l.info.Number + 1; k <= l.info.Total; k++ {
			date, ok := addMonths(l.tx.Date, k-l.info.Number)
			if !ok || date[:7] > month {
				break
			}
			if date[:7] != month {
				continue
			}
			out = append(out, models.ProjectedInstallment{
				Name:        l.info.Name,
				Type:        l.tx.Type,
				Number:      k,
				Total:       l.info.Total,
				Date:        date,
				AmountCents: l.tx.AmountCents,
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Number < out[j].Number
	})
	return out
}

// isLater diz se a parcela (info, t) deve substituir a atual da série: maior número; em empate, a mais recente.
func isLater(info Info, t models.Transaction, cur lastParcel) bool {
	if info.Number != cur.info.Number {
		return info.Number > cur.info.Number
	}
	if t.Date != cur.tx.Date {
		return t.Date > cur.tx.Date
	}
	return t.ID > cur.tx.ID
}

// addMonths soma n meses a uma data AAAA-MM-DD, ajustando o dia ao último do mês quando necessário.
func addMonths(date string, n int) (string, bool) {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return "", false
	}
	first := time.Date(t.Year(), t.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	lastDay := first.AddDate(0, 1, -1).Day()
	day := t.Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(first.Year(), first.Month(), day, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), true
}
