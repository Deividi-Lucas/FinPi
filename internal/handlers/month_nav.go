package handlers

import (
	"net/url"
	"strconv"
	"time"

	"finpi/internal/filter"
)

// monthNavURLs monta os links das setas do mês (anterior e próximo) da tela principal, mantendo o livro, as previstas
// e os filtros. Mês inválido ou fora de reportMinYear..reportMaxYear (a mesma faixa do relatório) fica sem seta
// naquela direção, devolvendo "".
func monthNavURLs(month string, bookID int64, projected bool, f filter.Filter) (prev, next string) {
	t, err := time.Parse("2006-01", month)
	if err != nil || len(month) != 7 {
		return "", ""
	}
	link := func(target time.Time) string {
		if target.Year() < reportMinYear || target.Year() > reportMaxYear {
			return ""
		}
		v := f.Values()
		v.Del("todos")
		v.Set("month", target.Format("2006-01"))
		if bookID > 0 {
			v.Set("livro", strconv.FormatInt(bookID, 10))
		}
		if projected {
			v.Set("previstas", "1")
		}
		return "/?" + url.Values(v).Encode()
	}
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return link(first.AddDate(0, -1, 0)), link(first.AddDate(0, 1, 0))
}
