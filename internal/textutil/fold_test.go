package textutil

import "testing"

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
