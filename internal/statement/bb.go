package statement

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"strings"

	"finpi/internal/textutil"
)

var (
	// dia/mês (e hora) no começo dos detalhes, como "03/10 20:46 ".
	bbLeadDateRe = regexp.MustCompile(`^\d{2}/\d{2}(?:\s+\d{2}:\d{2})?(?:\s+|$)`)
	bbFullDateRe = regexp.MustCompile(`\d{2}/\d{2}/\d{4}`)
	// agência, conta e código de banco: palavras só de dígitos e hífens, com 3 ou mais caracteres.
	bbNumberTokenRe = regexp.MustCompile(`^\d[\d-]{2,}(?:\s+|$)`)
)

// cleanBancoDoBrasil monta a descrição limpa: o lançamento (com Pix padronizado) seguido dos detalhes sem
// datas, documentos, agência, conta e códigos de banco.
func cleanBancoDoBrasil(label, details string) string {
	label = strings.Join(strings.Fields(label), " ")
	switch textutil.Fold(label) {
	case "pix - enviado":
		label = "Pix enviado"
	case "pix - recebido":
		label = "Pix recebido"
	}
	d := strings.Join(strings.Fields(details), " ")
	d = bbLeadDateRe.ReplaceAllString(d, "")
	d = bbFullDateRe.ReplaceAllString(d, "")
	d = docRe.ReplaceAllString(d, "")
	d = tidy(d)
	for {
		next := strings.TrimSpace(bbNumberTokenRe.ReplaceAllString(d, ""))
		if next == d {
			break
		}
		d = next
	}
	d = tidy(d)
	out := label
	if d != "" {
		out = label + " " + d
	}
	if out = tidy(out); out == "" {
		out = capRunes(label)
	}
	return out
}

// bbHashRef gera um identificador estável (h + 8 hexadecimais) quando o lançamento vem sem número de documento.
func bbHashRef(label, details string) string {
	h := fnv.New32a()
	h.Write([]byte(textutil.Fold(label + "|" + details)))
	return fmt.Sprintf("h%08x", h.Sum32())
}

// parseBancoDoBrasil lê as linhas do extrato do Banco do Brasil. As linhas de saldo (sem Tipo Lançamento) são ignoradas.
func parseBancoDoBrasil(grid [][]cell, header int, col map[string]int) (Statement, error) {
	docCol, hasDoc := 0, false
	for k, j := range col {
		if strings.HasSuffix(k, "documento") && (!hasDoc || j < docCol) {
			docCol, hasDoc = j, true
		}
	}
	st := Statement{Bank: BankBancoDoBrasil}
	for i := header + 1; i < len(grid); i++ {
		row := grid[i]
		if rowEmpty(row) {
			continue
		}
		line := i + 1
		get := func(j int) cell {
			if j < len(row) {
				return row[j]
			}
			return cell{}
		}
		label := strings.Join(strings.Fields(get(col["lancamento"]).text), " ")
		foldedLabel := textutil.Fold(label)
		if strings.TrimSpace(get(col["tipo lancamento"]).text) == "" ||
			strings.HasPrefix(foldedLabel, "saldo") || foldedLabel == "s a l d o" {
			continue
		}
		date, err := parseDate(get(col["data"]))
		if err != nil {
			return Statement{}, &RowError{line, "data inválida"}
		}
		if label == "" {
			return Statement{}, &RowError{line, "descrição vazia"}
		}
		details := strings.Join(strings.Fields(get(col["detalhes"]).text), " ")
		ref := ""
		if hasDoc {
			ref = strings.TrimSpace(get(docCol).text)
		}
		if ref == "" {
			ref = bbHashRef(label, details)
		}
		if len([]rune(ref)) > maxRefLen {
			return Statement{}, &RowError{line, "identificador longo demais"}
		}
		cents, err := parseCents(get(col["valor"]))
		if err != nil {
			return Statement{}, &RowError{line, "valor inválido"}
		}
		if len(st.Rows) >= MaxRows {
			return Statement{}, ErrTooManyRows
		}
		original := label
		if details != "" {
			original = label + " - " + details
		}
		st.Rows = append(st.Rows, Row{
			Line: line, Date: date, Description: cleanBancoDoBrasil(label, details),
			Original: truncateRunes(original, maxOriginalRunes), AmountCents: cents, RefID: ref,
		})
	}
	if len(st.Rows) == 0 {
		return Statement{}, ErrEmpty
	}
	return st, nil
}
