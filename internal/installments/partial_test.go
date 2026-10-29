package installments

import (
	"reflect"
	"testing"

	"finpi/internal/models"
)

func txPartial(id int64, desc string) models.Transaction {
	return models.Transaction{ID: id, Description: desc, Type: "expense", Date: "2026-10-05"}
}

func TestPartialSeries(t *testing.T) {
	all := []models.Transaction{
		txPartial(1, "Geladeira 1/3"), txPartial(2, "Geladeira 2/3"), txPartial(3, "Geladeira 3/3"),
		txPartial(4, "Sofá 1/2"), txPartial(5, "Sofá 2/2"),
		txPartial(6, "Mercado"), // não é parcela
	}
	cases := map[string]struct {
		moved []models.Transaction
		want  []string
	}{
		"nenhuma parcela":       {[]models.Transaction{txPartial(6, "Mercado")}, nil},
		"série inteira":         {[]models.Transaction{txPartial(4, "Sofá 1/2"), txPartial(5, "Sofá 2/2")}, nil},
		"parte de uma série":    {[]models.Transaction{txPartial(1, "Geladeira 1/3")}, []string{"Geladeira"}},
		"duas séries divididas": {[]models.Transaction{txPartial(1, "Geladeira 1/3"), txPartial(4, "Sofá 1/2")}, []string{"Geladeira", "Sofá"}},
		"vazio":                 {nil, nil},
	}
	for name, c := range cases {
		if got := PartialSeries(c.moved, all); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: PartialSeries = %v, quer %v", name, got, c.want)
		}
	}
}
