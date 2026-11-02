// Package transfer reúne a regra pura das transferências entre livros: validar um par, sugerir pares e
// montar a visão geral. Não toca o banco nem o disco.
package transfer

import (
	"errors"
	"sort"
	"time"
)

const (
	MaxDaysApart    = 3   // diferença máxima de dias entre a saída e a entrada
	MaxSuggestions  = 500 // máximo de pares sugeridos
	maxAlternatives = 3
	maxEdges        = 200000
)

var (
	ErrNeedOutIn       = errors.New("Escolha uma saída e uma entrada.")
	ErrSameBook        = errors.New("Os dois lançamentos precisam ser de livros diferentes.")
	ErrAmount          = errors.New("Os valores precisam ser iguais.")
	ErrTaken           = errors.New("Um dos lançamentos já está ligado ou marcado.")
	ErrNotFound        = errors.New("Lançamento não encontrado.")
	ErrNothingSelected = errors.New("Selecione ao menos um lançamento.")
)

// Tx é o que a regra precisa de um lançamento.
type Tx struct {
	ID          int64
	BookID      int64
	Type        string // "income" ou "expense"
	AmountCents int64
	Date        string // AAAA-MM-DD
	Description string
}

// Pair é um par sugerido: a saída, a entrada e as alternativas de cada lado (outros candidatos).
type Pair struct {
	Out, In       Tx
	DaysApart     int
	AltOut, AltIn []Tx
}

// Valid confere se a saída e a entrada formam um par: despesa + receita, livros diferentes, valor igual.
func Valid(out, in Tx) error {
	if out.Type != "expense" || in.Type != "income" {
		return ErrNeedOutIn
	}
	if out.BookID == in.BookID {
		return ErrSameBook
	}
	if out.AmountCents != in.AmountCents {
		return ErrAmount
	}
	return nil
}

const layout = "2006-01-02"

func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse(layout, s)
	return t, err == nil
}

func daysApart(a, b time.Time) int {
	d := int(a.Sub(b).Hours() / 24)
	if d < 0 {
		d = -d
	}
	return d
}

type edge struct {
	o, i Tx
	days int
}

// Suggest sugere pares entre lançamentos livres (já sem os ligados e os marcados): uma despesa e uma receita de
// livros diferentes, valor igual e datas a no máximo MaxDaysApart dias. A atribuição é gulosa e um-para-um, dos
// pares de menor diferença de data para os maiores (empate: menor ID). Cada par traz até 3 alternativas por lado.
func Suggest(txs []Tx) []Pair {
	type dated struct {
		tx Tx
		at time.Time
	}
	incomes := map[int64][]dated{} // valor → entradas, ordenadas por data
	for _, t := range txs {
		if t.Type != "income" {
			continue
		}
		if at, ok := parseDay(t.Date); ok {
			incomes[t.AmountCents] = append(incomes[t.AmountCents], dated{t, at})
		}
	}
	for _, list := range incomes {
		sort.Slice(list, func(a, b int) bool { return list[a].at.Before(list[b].at) })
	}

	var edges []edge
	for _, o := range txs {
		if o.Type != "expense" || len(edges) >= maxEdges {
			continue
		}
		at, ok := parseDay(o.Date)
		if !ok {
			continue
		}
		list := incomes[o.AmountCents]
		low := at.AddDate(0, 0, -MaxDaysApart)
		start := sort.Search(len(list), func(k int) bool { return !list[k].at.Before(low) })
		for k := start; k < len(list); k++ {
			d := daysApart(at, list[k].at)
			if list[k].at.After(at.AddDate(0, 0, MaxDaysApart)) {
				break
			}
			if list[k].tx.BookID == o.BookID {
				continue
			}
			edges = append(edges, edge{o: o, i: list[k].tx, days: d})
		}
	}
	sort.Slice(edges, func(a, b int) bool {
		ea, eb := edges[a], edges[b]
		if ea.days != eb.days {
			return ea.days < eb.days
		}
		if ea.o.ID != eb.o.ID {
			return ea.o.ID < eb.o.ID
		}
		return ea.i.ID < eb.i.ID
	})

	usedOut, usedIn := map[int64]bool{}, map[int64]bool{}
	altIn, altOut := map[int64][]Tx{}, map[int64][]Tx{}
	var pairs []Pair
	for _, e := range edges {
		altIn[e.o.ID] = append(altIn[e.o.ID], e.i)
		altOut[e.i.ID] = append(altOut[e.i.ID], e.o)
		if usedOut[e.o.ID] || usedIn[e.i.ID] {
			continue
		}
		usedOut[e.o.ID], usedIn[e.i.ID] = true, true
		pairs = append(pairs, Pair{Out: e.o, In: e.i, DaysApart: e.days})
	}
	for k := range pairs {
		p := &pairs[k]
		p.AltIn = others(altIn[p.Out.ID], p.In.ID)
		p.AltOut = others(altOut[p.In.ID], p.Out.ID)
	}
	if len(pairs) > MaxSuggestions {
		pairs = pairs[:MaxSuggestions]
	}
	return pairs
}

// others devolve até maxAlternatives candidatos, sem o escolhido e sem repetir.
func others(list []Tx, chosen int64) []Tx {
	var out []Tx
	seen := map[int64]bool{chosen: true}
	for _, t := range list {
		if seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		out = append(out, t)
		if len(out) == maxAlternatives {
			break
		}
	}
	return out
}
