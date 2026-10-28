package installments

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		desc   string
		want   Info
		wantOK bool
	}{
		{"Geladeira 3/10", Info{"Geladeira", 3, 10}, true},
		{"Geladeira 03/10", Info{"Geladeira", 3, 10}, true},
		{"Geladeira 3 / 10", Info{"Geladeira", 3, 10}, true},
		{"Geladeira - 3/10", Info{"Geladeira", 3, 10}, true},
		{"Geladeira (3/10)", Info{"Geladeira", 3, 10}, true},
		{"Geladeira parcela 3 de 10", Info{"Geladeira", 3, 10}, true},
		{"Geladeira PARCELA 3 DE 10", Info{"Geladeira", 3, 10}, true},
		{"  Geladeira 3/10  ", Info{"Geladeira", 3, 10}, true},
		{"Geladeira 13/60", Info{"Geladeira", 13, 60}, true},
		{"Item 2024 3/10", Info{"Item 2024", 3, 10}, true},
		{"Mercado", Info{}, false},
		{"Geladeira 0/10", Info{}, false},
		{"Geladeira 5/3", Info{}, false},
		{"Geladeira 1/1", Info{}, false},
		{"Geladeira 1/99", Info{}, false},
		{"3/10", Info{}, false},
		{"3/10 Geladeira", Info{}, false},
		{"Pagamento 10/11/2026", Info{}, false},
	}
	for _, c := range cases {
		got, ok := Parse(c.desc)
		if ok != c.wantOK || got != c.want {
			t.Errorf("Parse(%q) = %+v, %v; quer %+v, %v", c.desc, got, ok, c.want, c.wantOK)
		}
	}
}

func TestKey(t *testing.T) {
	a := Key(Info{"Geladeira", 3, 10}, "expense")

	if b := Key(Info{"  geladeira ", 4, 10}, "expense"); a != b {
		t.Errorf("mesma série com caixa/espaços/número diferentes deveria ter a mesma chave: %q != %q", a, b)
	}
	if Key(Info{"Pão de Açúcar", 1, 5}, "expense") != Key(Info{"pao  de acucar", 2, 5}, "expense") {
		t.Error("acentos e espaços repetidos devem ser normalizados")
	}
	if a == Key(Info{"Geladeira", 3, 12}, "expense") {
		t.Error("totais diferentes são séries diferentes")
	}
	if a == Key(Info{"Geladeira", 3, 10}, "income") {
		t.Error("tipos diferentes são séries diferentes")
	}
}
