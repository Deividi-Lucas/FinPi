package handlers

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"finpi/internal/models"
)

// maxAmountCents limita o valor de um lançamento (R$ 100 bilhões) para evitar estouro de int64.
const maxAmountCents = 10_000_000_000_000

// parseAmountCents converte "12.34" ou "12,34" em centavos, sem passar por ponto flutuante.
// Aceita no máximo um separador decimal e duas casas.
func parseAmountCents(s string) (int64, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, errors.New("valor numérico inválido")
	}
	if len(frac) > 2 || strings.Contains(frac, ".") {
		return 0, errors.New("valor numérico inválido")
	}
	for _, part := range []string{whole, frac} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, errors.New("valor numérico inválido")
			}
		}
	}

	frac += strings.Repeat("0", 2-len(frac))
	if whole == "" {
		whole = "0"
	}
	wholeNum, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, errors.New("valor numérico inválido")
	}
	fracNum, _ := strconv.ParseInt(frac, 10, 64)

	cents := wholeNum*100 + fracNum
	if wholeNum > maxAmountCents/100 || cents <= 0 || cents > maxAmountCents {
		return 0, errors.New("valor numérico inválido")
	}
	return cents, nil
}

// parseTransactionForm valida os campos do formulário de lançamento (usado ao criar e ao editar).
func parseTransactionForm(form url.Values) (models.Transaction, error) {
	var t models.Transaction

	cents, err := parseAmountCents(form.Get("amount"))
	if err != nil {
		return t, err
	}

	t.Type = strings.TrimSpace(form.Get("type"))
	if t.Type != "income" && t.Type != "expense" {
		return t, errors.New("tipo inválido: use 'income' ou 'expense'")
	}

	t.Description = strings.TrimSpace(form.Get("description"))
	if t.Description == "" {
		return t, errors.New("descrição é obrigatória")
	}

	t.Date = strings.TrimSpace(form.Get("date"))
	if _, err := time.Parse("2006-01-02", t.Date); err != nil {
		return t, errors.New("data inválida: use o formato AAAA-MM-DD")
	}

	t.AmountCents = cents
	t.Category = strings.TrimSpace(form.Get("category"))
	return t, nil
}
