// Package filter lê os filtros da URL (texto, categorias, tipo e faixa de valor) e decide se um lançamento
// passa por eles. São funções puras, usadas pela página principal e pelo relatório.
package filter

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"finpi/internal/textutil"
)

const (
	MaxCategories = 50  // categorias aceitas num filtro
	MaxTextLen    = 100 // caracteres da busca por texto
	maxCents      = 10_000_000_000_000
)

// Filter reúne os filtros ativos. Todos se combinam com "e"; as categorias marcadas se combinam com "ou".
type Filter struct {
	Text       string   // busca na descrição, como digitada (espaços colapsados)
	Categories []string // categorias marcadas, como vieram (sem repetidas)
	Type       string   // "", "income" ou "expense"
	MinCents   int64
	MaxCents   int64
	HasMin     bool
	HasMax     bool
	AllMonths  bool // só a página principal usa: procurar em todos os meses

	terms   []string        // palavras da busca, normalizadas (textutil.Fold)
	catKeys map[string]bool // categorias normalizadas
}

// Parse lê os filtros dos parâmetros q, cat (repetido), tipo (receita|despesa), min, max e todos (1).
// Valores inválidos são ignorados e devolvem um aviso em português.
func Parse(v url.Values) (Filter, []string) {
	var f Filter
	var warnings []string

	f.Text = strings.Join(strings.Fields(v.Get("q")), " ")
	if r := []rune(f.Text); len(r) > MaxTextLen {
		f.Text = strings.TrimSpace(string(r[:MaxTextLen]))
	}
	f.terms = strings.Fields(textutil.Fold(f.Text))

	f.catKeys = map[string]bool{}
	for _, c := range v["cat"] {
		c = strings.Join(strings.Fields(c), " ")
		key := textutil.Fold(c)
		if c == "" || f.catKeys[key] || len(f.Categories) >= MaxCategories {
			continue
		}
		f.catKeys[key] = true
		f.Categories = append(f.Categories, c)
	}

	switch strings.TrimSpace(v.Get("tipo")) {
	case "receita":
		f.Type = "income"
	case "despesa":
		f.Type = "expense"
	}

	if raw := strings.TrimSpace(v.Get("min")); raw != "" {
		if c, err := parseMoney(raw); err != nil {
			warnings = append(warnings, "Valor mínimo inválido: use números como 12,50.")
		} else {
			f.MinCents, f.HasMin = c, true
		}
	}
	if raw := strings.TrimSpace(v.Get("max")); raw != "" {
		if c, err := parseMoney(raw); err != nil {
			warnings = append(warnings, "Valor máximo inválido: use números como 12,50.")
		} else {
			f.MaxCents, f.HasMax = c, true
		}
	}
	if f.HasMin && f.HasMax && f.MinCents > f.MaxCents {
		warnings = append(warnings, "Filtro de valor ignorado: o mínimo é maior que o máximo.")
		f.HasMin, f.HasMax, f.MinCents, f.MaxCents = false, false, 0, 0
	}

	f.AllMonths = v.Get("todos") == "1"
	return f, warnings
}

// parseMoney converte "12,50" ou "12.50" em centavos (zero é válido). Aceita no máximo duas casas decimais.
// É a mesma regra do valor de um lançamento (handlers.parseAmountCents), só que aceitando zero.
func parseMoney(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, fmt.Errorf("valor inválido")
	}
	if len(frac) > 2 || strings.Contains(frac, ".") {
		return 0, fmt.Errorf("valor inválido")
	}
	for _, part := range []string{whole, frac} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("valor inválido")
			}
		}
	}
	frac += strings.Repeat("0", 2-len(frac))
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > maxCents/100 {
		return 0, fmt.Errorf("valor inválido")
	}
	f, _ := strconv.ParseInt(frac, 10, 64)
	if cents := w*100 + f; cents <= maxCents {
		return cents, nil
	}
	return 0, fmt.Errorf("valor inválido")
}

// Active conta quantos filtros estão ligados (texto, categorias, tipo e valor contam um cada).
func (f Filter) Active() int {
	n := 0
	if len(f.terms) > 0 {
		n++
	}
	if len(f.Categories) > 0 {
		n++
	}
	if f.Type != "" {
		n++
	}
	if f.HasMin || f.HasMax {
		n++
	}
	return n
}

// Selected diz se a categoria está marcada (compara pelo nome normalizado).
func (f Filter) Selected(category string) bool {
	return f.catKeys[textutil.Fold(category)]
}

// Match diz se um lançamento (categoria, descrição, tipo "income"/"expense" e valor em centavos) passa pelos filtros.
func (f Filter) Match(category, description, typ string, cents int64) bool {
	if len(f.catKeys) > 0 && !f.catKeys[textutil.Fold(category)] {
		return false
	}
	if f.Type != "" && typ != f.Type {
		return false
	}
	if f.HasMin && cents < f.MinCents {
		return false
	}
	if f.HasMax && cents > f.MaxCents {
		return false
	}
	if len(f.terms) > 0 {
		folded := textutil.Fold(description)
		for _, t := range f.terms {
			if !strings.Contains(folded, t) {
				return false
			}
		}
	}
	return true
}

// MinText e MaxText devolvem os limites de valor para o formulário ("12,50"), ou "" se não houver.
func (f Filter) MinText() string { return formatCents(f.MinCents, f.HasMin) }
func (f Filter) MaxText() string { return formatCents(f.MaxCents, f.HasMax) }

func formatCents(c int64, has bool) string {
	if !has {
		return ""
	}
	return fmt.Sprintf("%d,%02d", c/100, c%100)
}

// TypeParam devolve o valor de tipo da URL ("receita", "despesa" ou "").
func (f Filter) TypeParam() string {
	switch f.Type {
	case "income":
		return "receita"
	case "expense":
		return "despesa"
	}
	return ""
}

// Values devolve os filtros normalizados como parâmetros de URL (só o que está ativo).
func (f Filter) Values() url.Values {
	v := url.Values{}
	if f.Text != "" {
		v.Set("q", f.Text)
	}
	for _, c := range f.Categories {
		v.Add("cat", c)
	}
	if t := f.TypeParam(); t != "" {
		v.Set("tipo", t)
	}
	if f.HasMin {
		v.Set("min", f.MinText())
	}
	if f.HasMax {
		v.Set("max", f.MaxText())
	}
	if f.AllMonths {
		v.Set("todos", "1")
	}
	return v
}

// Query devolve Values codificado ("" sem filtros).
func (f Filter) Query() string { return f.Values().Encode() }
