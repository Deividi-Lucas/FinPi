package handlers

import (
	"sort"

	"finpi/internal/database"
	"finpi/internal/filter"
	"finpi/internal/installments"
	"finpi/internal/models"
	"finpi/internal/textutil"
)

// allBooksParam é o valor de livro= que pede o relatório de todos os livros somados.
const allBooksParam = "todos"

// allBooksPseudoBook é o "livro" mostrado no cabeçalho e no seletor quando o relatório soma todos (cor neutra).
var allBooksPseudoBook = models.Book{Name: "Todos os livros", Color: "slate"}

// excludedSums são as entradas e saídas que ficaram fora do fluxo de "Todos os livros" por já terem o outro lado em
// outro livro (ligadas) ou por estarem marcadas "fora do FinPi".
type excludedSums struct {
	LinkedIn, LinkedOut int64
	SoloIn, SoloOut     int64
}

func (e *excludedSums) add(t models.Transaction, linked bool) {
	switch {
	case linked && t.Type == "income":
		e.LinkedIn += t.AmountCents
	case linked:
		e.LinkedOut += t.AmountCents
	case t.Type == "income":
		e.SoloIn += t.AmountCents
	default:
		e.SoloOut += t.AmountCents
	}
}

// transferSets lê as ligações e as marcas e devolve os IDs que não contam nos totais somados: só as ligações válidas
// com os dois livros ativos descontam (com um livro arquivado o lado ativo volta a contar) e as marcas "fora do FinPi".
func (h *Handler) transferSets(active []models.Book) (linked, soloSet map[int64]bool, links []database.TransferLink, solo []models.Transaction, err error) {
	if links, err = h.db.ListTransferLinks(); err != nil {
		return nil, nil, nil, nil, err
	}
	if solo, err = h.db.ListSolo(); err != nil {
		return nil, nil, nil, nil, err
	}
	isActive := map[int64]bool{}
	for _, b := range active {
		isActive[b.ID] = true
	}
	linked, soloSet = map[int64]bool{}, map[int64]bool{}
	for _, l := range links {
		if l.Valid && isActive[l.Out.BookID] && isActive[l.In.BookID] {
			linked[l.Out.ID], linked[l.In.ID] = true, true
		}
	}
	for _, s := range solo {
		soloSet[s.ID] = true
	}
	return linked, soloSet, links, solo, nil
}

// allBooksData reúne o que o relatório precisa de todos os livros ativos.
type allBooksData struct {
	Txs        []models.Transaction // do período, já sem os ligados e os marcados (ainda com os internos)
	Categories []models.Category    // união por nome das categorias dos livros
	Excluded   excludedSums
	Candidates [][]models.Transaction // candidatas a parcela, uma lista por livro
}

// loadAllBooks lê os lançamentos do período de cada livro ativo. Os ligados e os marcados saem do fluxo e entram nas
// somas de excluídos (só os que passam pelos filtros, como a movimentação interna).
func (h *Handler) loadAllBooks(books []models.Book, from, to string, f filter.Filter, withInstallments bool) (allBooksData, error) {
	var data allBooksData
	active := activeBooks(books)
	linked, soloSet, _, _, err := h.transferSets(active)
	if err != nil {
		return data, err
	}
	seen := map[string]bool{}
	for _, b := range active {
		bdb := h.db.ForBook(b.ID)
		txs, err := bdb.GetTransactionsBetween(from, to)
		if err != nil {
			return data, err
		}
		for _, t := range txs {
			isLinked := linked[t.ID]
			// a movimentação interna já sai dos totais por outro caminho: não conta duas vezes como transferência
			if (isLinked || soloSet[t.ID]) && !models.IsInternal(t.Category) {
				if matchTx(f, t) {
					data.Excluded.add(t, isLinked)
				}
				continue
			}
			data.Txs = append(data.Txs, t)
		}
		cats, err := bdb.ListCategories()
		if err != nil {
			return data, err
		}
		for _, c := range cats {
			if key := textutil.Fold(c.Name); !seen[key] {
				seen[key] = true
				data.Categories = append(data.Categories, c)
			}
		}
		if withInstallments {
			cands, err := bdb.GetInstallmentCandidates()
			if err != nil {
				return data, err
			}
			data.Candidates = append(data.Candidates, cands)
		}
	}
	sort.SliceStable(data.Txs, func(i, j int) bool {
		if data.Txs[i].Date != data.Txs[j].Date {
			return data.Txs[i].Date < data.Txs[j].Date
		}
		return data.Txs[i].ID < data.Txs[j].ID
	})
	sort.SliceStable(data.Categories, func(i, j int) bool {
		return textutil.Fold(data.Categories[i].Name) < textutil.Fold(data.Categories[j].Name)
	})
	return data, nil
}

// projectInstallments projeta as parcelas que faltam em cada mês do período, uma lista de candidatas por vez (cada
// livro tem as suas séries), respeitando o filtro e deixando de fora a movimentação interna.
func projectInstallments(sets [][]models.Transaction, months []string, from, to string, f filter.Filter) []models.ProjectedInstallment {
	var out []models.ProjectedInstallment
	for _, candidates := range sets {
		for _, m := range months {
			for _, p := range installments.Project(candidates, m) {
				if p.Date >= from && p.Date <= to && !models.IsInternal(p.Category) && matchProjected(f, p) {
					out = append(out, p)
				}
			}
		}
	}
	return out
}
