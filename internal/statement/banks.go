package statement

import (
	"strings"

	"finpi/internal/textutil"
)

var (
	sicrediColumns = []string{"data", "descricao", "identificador", "tipo", "valor"}
	nubankColumns  = []string{"data", "valor", "identificador", "descricao"}
)

func headerKey(s string) string {
	return textutil.Fold(strings.TrimPrefix(s, string(rune(0xFEFF))))
}

// findHeader procura a primeira linha que tem todas as colunas pedidas e devolve o índice dela e o mapa coluna → posição.
func findHeader(grid [][]cell, required []string) (int, map[string]int, bool) {
	for i, row := range grid {
		col := map[string]int{}
		for j, c := range row {
			if k := headerKey(c.text); k != "" {
				if _, ok := col[k]; !ok {
					col[k] = j
				}
			}
		}
		ok := true
		for _, name := range required {
			if _, has := col[name]; !has {
				ok = false
				break
			}
		}
		if ok {
			return i, col, true
		}
	}
	return 0, nil, false
}

// parseGrid reconhece o banco pelo cabeçalho: Mercado Pago (RELEASE_DATE), Sicredi (tem Tipo) ou Nubank.
func parseGrid(grid [][]cell) (Statement, error) {
	for _, row := range grid {
		for _, c := range row {
			if normHeader(c.text) == "RELEASE_DATE" {
				return parseMercadoPago(grid)
			}
		}
	}
	// As colunas do Nubank são um subconjunto das do Sicredi: o Sicredi vem primeiro.
	if i, col, ok := findHeader(grid, sicrediColumns); ok {
		return parseBankRows(BankSicredi, grid, i, col)
	}
	if i, col, ok := findHeader(grid, nubankColumns); ok {
		return parseBankRows(BankNubank, grid, i, col)
	}
	return Statement{}, ErrUnrecognized
}

// parseBankRows lê as linhas de um extrato com colunas Data, Descricao, Identificador e Valor (sinal no valor).
func parseBankRows(bank Bank, grid [][]cell, header int, col map[string]int) (Statement, error) {
	st := Statement{Bank: bank}
	for i := header + 1; i < len(grid); i++ {
		row := grid[i]
		if rowEmpty(row) {
			continue
		}
		line := i + 1
		get := func(name string) cell {
			if j := col[name]; j < len(row) {
				return row[j]
			}
			return cell{}
		}
		date, err := parseDate(get("data"))
		if err != nil {
			return Statement{}, &RowError{line, "data inválida"}
		}
		original := strings.Join(strings.Fields(get("descricao").text), " ")
		if original == "" {
			return Statement{}, &RowError{line, "descrição vazia"}
		}
		ref := strings.TrimSpace(get("identificador").text)
		if ref == "" {
			return Statement{}, &RowError{line, "sem identificador"}
		}
		cents, err := parseCents(get("valor"))
		if err != nil {
			return Statement{}, &RowError{line, "valor inválido"}
		}
		if len(st.Rows) >= MaxRows {
			return Statement{}, ErrTooManyRows
		}
		st.Rows = append(st.Rows, Row{
			Line: line, Date: date, Description: CleanDescription(bank, original), Original: original,
			AmountCents: cents, RefID: ref,
		})
	}
	if len(st.Rows) == 0 {
		return Statement{}, ErrEmpty
	}
	return st, nil
}
