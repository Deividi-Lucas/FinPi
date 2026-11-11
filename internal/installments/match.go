package installments

import (
	"fmt"
	"time"

	"finpi/internal/models"
)

// MatchWindowDays é a distância máxima, em dias, entre a data prevista e a data da linha do extrato.
const MatchWindowDays = 5

// Row é uma linha de extrato candidata a realizar uma parcela prevista.
type Row struct {
	Date        string // AAAA-MM-DD
	Type        string // "income" ou "expense"
	AmountCents int64  // módulo
	Skip        bool   // linha que não pode ser alterada (ex.: já importada)
}

// Description monta a descrição da parcela real ("Geladeira 4/10"), que faz a prevista sumir.
func Description(p models.ProjectedInstallment) string {
	return fmt.Sprintf("%s %d/%d", p.Name, p.Number, p.Total)
}

// Match liga cada linha a no máximo uma parcela prevista: mesmo tipo, mesmo valor, data a até
// MatchWindowDays dias e, quando os dois lados têm conta, a mesma conta (account 0 = desconhecida).
// Só vale par sem ambiguidade: a linha com uma única prevista possível, e essa prevista com uma
// única linha possível. Devolve, para cada linha, o índice da prevista ou -1.
func Match(projected []models.ProjectedInstallment, rows []Row, account int64) []int {
	out := make([]int, len(rows))
	rowCands := make([][]int, len(rows))
	projCount := make([]int, len(projected))

	for i, r := range rows {
		out[i] = -1
		if r.Skip {
			continue
		}
		rd, err := time.Parse("2006-01-02", r.Date)
		if err != nil {
			continue
		}
		for j, p := range projected {
			if p.Type != r.Type || p.AmountCents != r.AmountCents {
				continue
			}
			if account != 0 && p.AccountID != 0 && account != p.AccountID {
				continue
			}
			pd, err := time.Parse("2006-01-02", p.Date)
			if err != nil {
				continue
			}
			days := rd.Sub(pd).Hours() / 24
			if days < -MatchWindowDays || days > MatchWindowDays {
				continue
			}
			rowCands[i] = append(rowCands[i], j)
			projCount[j]++
		}
	}

	for i, c := range rowCands {
		if len(c) == 1 && projCount[c[0]] == 1 {
			out[i] = c[0]
		}
	}
	return out
}
