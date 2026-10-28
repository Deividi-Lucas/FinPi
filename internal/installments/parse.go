// Package installments lê parcelas da descrição dos lançamentos e projeta as parcelas seguintes.
// Tudo é calculado a partir dos lançamentos existentes; nada é gravado.
package installments

import (
	"regexp"
	"strconv"
	"strings"

	"finpi/internal/textutil"
)

const (
	minTotal = 2
	maxTotal = 60
)

// Info descreve a parcela lida de uma descrição.
type Info struct {
	Name   string // nome da série, sem o marcador (ex.: "Geladeira")
	Number int    // parcela atual
	Total  int    // total de parcelas
}

// Marcadores aceitos, sempre no fim da descrição:
// "Geladeira 3/10", "Geladeira - 03/10", "Geladeira (3 / 10)" e "Geladeira parcela 3 de 10".
var markers = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.*?)[\s:\-]*\(?\s*(\d{1,2})\s*/\s*(\d{1,2})\s*\)?\s*$`),
	regexp.MustCompile(`(?i)^(.*?)[\s:\-]*\(?\s*parcela\s+(\d{1,2})\s+de\s+(\d{1,2})\s*\)?\s*$`),
}

// Parse lê a parcela de uma descrição. Devolve false se não houver marcador válido.
func Parse(description string) (Info, bool) {
	text := strings.TrimSpace(description)
	for _, re := range markers {
		m := re.FindStringSubmatch(text)
		if m == nil {
			continue
		}
		number, _ := strconv.Atoi(m[2])
		total, _ := strconv.Atoi(m[3])
		name := strings.TrimSpace(m[1])
		if name == "" || total < minTotal || total > maxTotal || number < 1 || number > total {
			return Info{}, false
		}
		return Info{Name: name, Number: number, Total: total}, true
	}
	return Info{}, false
}

// Key identifica a série de uma parcela: nome normalizado + tipo + total de parcelas.
func Key(info Info, txType string) string {
	return textutil.Fold(info.Name) + "|" + txType + "|" + strconv.Itoa(info.Total)
}
