package transfer

import (
	"errors"
	"fmt"
	"testing"
)

func exp(id, book int64, cents int64, date string) Tx {
	return Tx{ID: id, BookID: book, Type: "expense", AmountCents: cents, Date: date, Description: fmt.Sprintf("saída %d", id)}
}
func inc(id, book int64, cents int64, date string) Tx {
	return Tx{ID: id, BookID: book, Type: "income", AmountCents: cents, Date: date, Description: fmt.Sprintf("entrada %d", id)}
}

func TestValid(t *testing.T) {
	cases := []struct {
		name    string
		out, in Tx
		want    error
	}{
		{"par válido", exp(1, 1, 5000, "2026-10-05"), inc(2, 2, 5000, "2026-10-05"), nil},
		{"duas saídas", exp(1, 1, 5000, "2026-10-05"), exp(2, 2, 5000, "2026-10-05"), ErrNeedOutIn},
		{"ordem trocada", inc(1, 1, 5000, "2026-10-05"), exp(2, 2, 5000, "2026-10-05"), ErrNeedOutIn},
		{"mesmo livro", exp(1, 1, 5000, "2026-10-05"), inc(2, 1, 5000, "2026-10-05"), ErrSameBook},
		{"valores diferentes", exp(1, 1, 5000, "2026-10-05"), inc(2, 2, 4999, "2026-10-05"), ErrAmount},
	}
	for _, c := range cases {
		if got := Valid(c.out, c.in); !errors.Is(got, c.want) {
			t.Errorf("%s: Valid = %v, quer %v", c.name, got, c.want)
		}
	}
}

func ids(p Pair) [2]int64 { return [2]int64{p.Out.ID, p.In.ID} }

func TestSuggest_PairsOnlyDifferentBooksWithinWindow(t *testing.T) {
	txs := []Tx{
		exp(1, 1, 8190, "2026-10-05"), inc(2, 2, 8190, "2026-10-05"), // mesmo dia
		exp(3, 1, 5000, "2026-10-10"), inc(4, 2, 5000, "2026-10-13"), // 3 dias: entra
		exp(5, 1, 7000, "2026-10-10"), inc(6, 2, 7000, "2026-10-14"), // 4 dias: não entra
		exp(7, 1, 9000, "2026-10-10"), inc(8, 1, 9000, "2026-10-10"), // mesmo livro: não entra
		exp(9, 1, 1100, "2026-10-10"), inc(10, 2, 1200, "2026-10-10"), // valores diferentes
		exp(11, 1, 400, "2026-10-10"), exp(12, 2, 400, "2026-10-10"), // duas saídas
	}
	got := Suggest(txs)
	if len(got) != 2 || ids(got[0]) != [2]int64{1, 2} || ids(got[1]) != [2]int64{3, 4} {
		t.Fatalf("Suggest = %+v", got)
	}
	if got[0].DaysApart != 0 || got[1].DaysApart != 3 {
		t.Errorf("DaysApart = %d e %d", got[0].DaysApart, got[1].DaysApart)
	}
}

func TestSuggest_WindowCrossesMonthAndYear(t *testing.T) {
	got := Suggest([]Tx{exp(1, 1, 100, "2026-12-31"), inc(2, 2, 100, "2027-01-02")})
	if len(got) != 1 || got[0].DaysApart != 2 {
		t.Errorf("virada de ano: %+v", got)
	}
}

func TestSuggest_OneToOnePrefersClosestDate(t *testing.T) {
	// duas saídas iguais disputam uma entrada: a mais próxima fica com ela
	txs := []Tx{exp(1, 1, 5000, "2026-10-01"), exp(2, 1, 5000, "2026-10-04"), inc(3, 2, 5000, "2026-10-04")}
	got := Suggest(txs)
	if len(got) != 1 || ids(got[0]) != [2]int64{2, 3} {
		t.Fatalf("Suggest = %+v", got)
	}
	// a saída 1 aparece como alternativa de saída para a entrada 3
	if len(got[0].AltOut) != 1 || got[0].AltOut[0].ID != 1 {
		t.Errorf("AltOut = %+v", got[0].AltOut)
	}
}

func TestSuggest_AlternativesAreLimitedAndExcludeTheChosen(t *testing.T) {
	txs := []Tx{exp(1, 1, 100, "2026-10-05")}
	for i := int64(0); i < 6; i++ {
		txs = append(txs, inc(10+i, 2, 100, "2026-10-05"))
	}
	got := Suggest(txs)
	if len(got) != 1 || got[0].In.ID != 10 {
		t.Fatalf("Suggest = %+v", got)
	}
	if len(got[0].AltIn) != 3 {
		t.Errorf("AltIn deveria ter no máximo 3: %d", len(got[0].AltIn))
	}
	for _, a := range got[0].AltIn {
		if a.ID == got[0].In.ID {
			t.Error("a alternativa não pode repetir a entrada escolhida")
		}
	}
}

func TestSuggest_IsDeterministicAndCapped(t *testing.T) {
	var txs []Tx
	for i := int64(0); i < 700; i++ {
		txs = append(txs, exp(i*2+1, 1, 100+i, "2026-10-05"), inc(i*2+2, 2, 100+i, "2026-10-05"))
	}
	a, b := Suggest(txs), Suggest(txs)
	if len(a) != MaxSuggestions || len(b) != MaxSuggestions {
		t.Fatalf("limite: %d e %d, quer %d", len(a), len(b), MaxSuggestions)
	}
	for i := range a {
		if ids(a[i]) != ids(b[i]) {
			t.Fatalf("resultado não determinístico na posição %d", i)
		}
	}
}

func TestSuggest_IgnoresBadDatesAndEmpty(t *testing.T) {
	if got := Suggest(nil); len(got) != 0 {
		t.Errorf("Suggest(nil) = %+v", got)
	}
	if got := Suggest([]Tx{exp(1, 1, 100, "não é data"), inc(2, 2, 100, "2026-10-05")}); len(got) != 0 {
		t.Errorf("data inválida não pode formar par: %+v", got)
	}
}
