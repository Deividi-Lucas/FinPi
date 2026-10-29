// Package bulk calcula as edições em lote de lançamentos (categoria, prioridade, data e descrição).
// São funções puras: não tocam o banco.
package bulk

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"finpi/internal/models"
)

const (
	MaxDescLen     = 200
	MaxFindLen     = 100
	MaxShiftDays   = 3650
	MaxShiftMonths = 120
	maxSummaryLen  = 300
	dateLayout     = "2006-01-02"
)

const (
	PriorityFollow = "follow" // limpa a prioridade: o lançamento passa a seguir a categoria
	DateSet        = "set"    // define a mesma data para todos
	DateShift      = "shift"  // soma ou subtrai dias ou meses
	UnitDays       = "days"
	UnitMonths     = "months"
)

// Spec descreve a edição em lote. Campo vazio = não alterar.
type Spec struct {
	Category  string // nome canônico de uma categoria existente (validado por quem chama)
	Priority  string // "", PriorityFollow, "essential" ou "superfluous"
	DateMode  string // "", DateSet ou DateShift
	DateSet   string // AAAA-MM-DD (DateMode == DateSet)
	ShiftN    int    // dias ou meses, com sinal e diferente de zero (DateMode == DateShift)
	ShiftUnit string // UnitDays ou UnitMonths
	Find      string // trecho da descrição a localizar (sem diferenciar maiúsculas)
	Replace   string // texto que entra no lugar; vazio remove o trecho
}

// Empty diz se a Spec não pede nenhuma alteração.
func (s Spec) Empty() bool {
	return s.Category == "" && s.Priority == "" && s.DateMode == "" && s.Find == ""
}

// Validate confere os limites e os formatos da Spec.
func (s Spec) Validate() error {
	switch s.Priority {
	case "", PriorityFollow, models.PriorityEssential, models.PrioritySuperfluous:
	default:
		return errors.New("prioridade inválida")
	}
	switch s.DateMode {
	case "":
	case DateSet:
		if _, err := time.Parse(dateLayout, s.DateSet); err != nil {
			return errors.New("data inválida: use o formato AAAA-MM-DD")
		}
	case DateShift:
		limit := MaxShiftDays
		switch s.ShiftUnit {
		case UnitDays:
		case UnitMonths:
			limit = MaxShiftMonths
		default:
			return errors.New("unidade de deslocamento inválida: use dias ou meses")
		}
		if s.ShiftN == 0 || s.ShiftN > limit || s.ShiftN < -limit {
			return fmt.Errorf("deslocamento inválido: use um número diferente de zero, de até %d", limit)
		}
	default:
		return errors.New("modo de data inválido")
	}
	if s.Find == "" && s.Replace != "" {
		return errors.New("informe o trecho a localizar")
	}
	if utf8.RuneCountInString(s.Find) > MaxFindLen {
		return fmt.Errorf("o trecho a localizar tem mais de %d caracteres", MaxFindLen)
	}
	if utf8.RuneCountInString(s.Replace) > MaxDescLen {
		return fmt.Errorf("o texto de substituição tem mais de %d caracteres", MaxDescLen)
	}
	return nil
}

// shift soma n dias ou meses a uma data; no deslocamento por meses, se o dia não existir no mês de destino
// vai para o último dia do mês.
func shift(d time.Time, n int, unit string) time.Time {
	if unit == UnitDays {
		return d.AddDate(0, 0, n)
	}
	first := time.Date(d.Year(), d.Month()+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(d.Day(), last), 0, 0, 0, 0, time.UTC)
}

// Apply devolve o lançamento como ficaria depois da edição. Valor, tipo e identificador de origem não mudam.
// Receitas terminam sempre sem prioridade. Descrição fora de 1 a MaxDescLen caracteres é um erro.
func Apply(t models.Transaction, s Spec) (models.Transaction, error) {
	after := t
	if s.Category != "" {
		after.Category = s.Category
	}
	switch s.Priority {
	case "":
	case PriorityFollow:
		after.Priority = ""
	default:
		after.Priority = s.Priority
	}
	if after.Type == "income" {
		after.Priority = ""
	}

	switch s.DateMode {
	case DateSet:
		after.Date = s.DateSet
	case DateShift:
		d, err := time.Parse(dateLayout, t.Date)
		if err != nil {
			return t, errors.New("a data do lançamento é inválida")
		}
		after.Date = shift(d, s.ShiftN, s.ShiftUnit).Format(dateLayout)
	}

	if s.Find != "" {
		re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(s.Find))
		desc := strings.Join(strings.Fields(re.ReplaceAllLiteralString(t.Description, s.Replace)), " ")
		switch n := utf8.RuneCountInString(desc); {
		case n == 0:
			return t, errors.New("a descrição ficaria vazia")
		case n > MaxDescLen:
			return t, fmt.Errorf("a descrição ficaria com mais de %d caracteres", MaxDescLen)
		}
		after.Description = desc
	}
	return after, nil
}

// Differs diz se a edição muda algum dos quatro campos editáveis.
func Differs(a, b models.Transaction) bool {
	return a.Description != b.Description || a.Category != b.Category || a.Date != b.Date || a.Priority != b.Priority
}

// Row é um lançamento na prévia: o antes, o depois, se mudou e o erro (se a edição não puder ser aplicada).
type Row struct {
	Before, After models.Transaction
	Changed       bool
	Err           string
}

// Preview calcula o antes → depois de cada lançamento, sem gravar nada.
func Preview(list []models.Transaction, s Spec) []Row {
	rows := make([]Row, 0, len(list))
	for _, t := range list {
		after, err := Apply(t, s)
		r := Row{Before: t, After: after}
		if err != nil {
			r.After, r.Err = t, err.Error()
		} else {
			r.Changed = Differs(t, after)
		}
		rows = append(rows, r)
	}
	return rows
}

// Summary descreve a edição em uma linha (guardada com o lote, para o aviso de desfazer).
func Summary(s Spec) string {
	var parts []string
	if s.Category != "" {
		parts = append(parts, "categoria → "+s.Category)
	}
	switch s.Priority {
	case PriorityFollow:
		parts = append(parts, "prioridade → seguir a categoria")
	case models.PriorityEssential:
		parts = append(parts, "prioridade → essencial")
	case models.PrioritySuperfluous:
		parts = append(parts, "prioridade → supérfluo")
	}
	switch s.DateMode {
	case DateSet:
		if d, err := time.Parse(dateLayout, s.DateSet); err == nil {
			parts = append(parts, "data → "+d.Format("02/01/2006"))
		}
	case DateShift:
		label := map[string][2]string{UnitDays: {"dia", "dias"}, UnitMonths: {"mês", "meses"}}[s.ShiftUnit]
		unit := label[1]
		if s.ShiftN == 1 || s.ShiftN == -1 {
			unit = label[0]
		}
		parts = append(parts, fmt.Sprintf("data deslocada %+d %s", s.ShiftN, unit))
	}
	if s.Find != "" {
		parts = append(parts, fmt.Sprintf("descrição: '%s' → '%s'", s.Find, s.Replace))
	}
	out := strings.Join(parts, "; ")
	if r := []rune(out); len(r) > maxSummaryLen {
		out = string(r[:maxSummaryLen-1]) + "…"
	}
	return out
}
