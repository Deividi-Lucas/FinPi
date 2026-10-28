package handlers

import (
	"net/url"
	"testing"
)

func TestParseAmountCents(t *testing.T) {
	valid := map[string]int64{
		"10":      1000,
		"10.5":    1050,
		"10,50":   1050,
		"0.01":    1,
		"1234.56": 123456,
		" 7.25 ":  725,
	}
	for in, want := range valid {
		got, err := parseAmountCents(in)
		if err != nil {
			t.Errorf("parseAmountCents(%q): erro inesperado: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("parseAmountCents(%q) = %d, quer %d", in, got, want)
		}
	}

	invalid := []string{"", "0", "0.00", "-5", "abc", "10.999", "1.234,56", "1e3", "99999999999999999999"}
	for _, in := range invalid {
		if got, err := parseAmountCents(in); err == nil {
			t.Errorf("parseAmountCents(%q) = %d, quer erro", in, got)
		}
	}
}

func validForm() url.Values {
	return url.Values{
		"description": {"  Mercado  "},
		"amount":      {"45,90"},
		"type":        {"expense"},
		"category":    {" Alimentação "},
		"date":        {"2026-10-05"},
	}
}

func TestParseTransactionForm_Valid(t *testing.T) {
	tx, err := parseTransactionForm(validForm())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if tx.Description != "Mercado" || tx.Category != "Alimentação" {
		t.Errorf("campos de texto sem trim: %+v", tx)
	}
	if tx.AmountCents != 4590 {
		t.Errorf("AmountCents = %d, quer 4590", tx.AmountCents)
	}
	if tx.Type != "expense" || tx.Date != "2026-10-05" {
		t.Errorf("tipo/data incorretos: %+v", tx)
	}
}

func TestParseTransactionForm_Invalid(t *testing.T) {
	cases := map[string]func(url.Values){
		"descrição vazia":       func(v url.Values) { v.Set("description", "   ") },
		"valor inválido":        func(v url.Values) { v.Set("amount", "abc") },
		"valor zero":            func(v url.Values) { v.Set("amount", "0") },
		"tipo inválido":         func(v url.Values) { v.Set("type", "transfer") },
		"data vazia":            func(v url.Values) { v.Set("date", "") },
		"data com formato ruim": func(v url.Values) { v.Set("date", "05/10/2026") },
		"data inexistente":      func(v url.Values) { v.Set("date", "2026-02-30") },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			v := validForm()
			mutate(v)
			if _, err := parseTransactionForm(v); err == nil {
				t.Error("quer erro de validação, veio nil")
			}
		})
	}
}
