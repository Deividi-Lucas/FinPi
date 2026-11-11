package textutil

import (
	"strconv"
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Saúde":          "saude",
		"  SAÚDE  ":      "saude",
		"Pão  de Açúcar": "pao de acucar",
		"Educação":       "educacao",
		"Ônibus":         "onibus",
		"Lazer":          "lazer",
		"":               "",
		"   ":            "",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, quer %q", in, got, want)
		}
	}
}

func TestFoldCachedMatchesFold(t *testing.T) {
	for _, s := range []string{"Saúde", "  SAÚDE  ", "Movimentação interna", "Investimento", "", "Pão  de Açúcar", "Saúde"} {
		if got, want := FoldCached(s), Fold(s); got != want {
			t.Errorf("FoldCached(%q) = %q, quer %q", s, got, want)
		}
	}
}

func TestFoldCachedBounded(t *testing.T) {
	for i := 0; i < maxFoldCache+50; i++ {
		s := "Categoria " + string(rune('A'+i%26)) + string(rune('0'+i%10)) + strings.Repeat("x", i%7) + strconv.Itoa(i)
		if FoldCached(s) != Fold(s) {
			t.Fatalf("FoldCached(%q) difere de Fold", s)
		}
	}
	if n := foldCacheLen(); n > maxFoldCache {
		t.Fatalf("cache com %d entradas, limite %d", n, maxFoldCache)
	}
}
