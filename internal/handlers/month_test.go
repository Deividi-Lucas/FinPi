package handlers

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"finpi/internal/filter"
	"finpi/internal/models"
)

// O caminho rápido (somas no SQL + só as linhas exibidas) precisa dar exatamente o mesmo resultado que o
// caminho completo (todos os lançamentos do mês em memória) quando não há filtro.
func TestLoadMonth_FastMatchesFull(t *testing.T) {
	_, db := newTestHandler(t)
	if _, err := db.GetOrCreateCategory("Saúde"); err != nil {
		t.Fatal(err)
	}
	cats := []string{"Saúde", "saude", "SAÚDE ", "Lazer", "Outros", models.InternalCategory, "movimentacao interna", models.InvestmentCategory, "Moradia"}
	priorities := []string{"", "", "essential", "superfluous"}
	descs := []string{"Mercado", "Geladeira 3/10", "Aluguel", "Notebook parcela 2 de 12", "Café"}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 640; i++ {
		typ := "expense"
		if rng.Intn(4) == 0 {
			typ = "income"
		}
		day := rng.Intn(28) + 1
		month := "2026-11"
		if i%40 == 0 {
			month = "2026-12" // fora do mês: não pode entrar
		}
		tx := models.Transaction{
			Description: descs[rng.Intn(len(descs))],
			AmountCents: int64(rng.Intn(50)+1) * 100, // poucos valores distintos: força empates no maior/menor
			Type:        typ,
			Category:    cats[rng.Intn(len(cats))],
			Date:        fmt.Sprintf("%s-%02d", month, day),
			Priority:    priorities[rng.Intn(len(priorities))],
		}
		if typ == "income" {
			tx.Priority = ""
		}
		if err := db.CreateTransaction(&tx); err != nil {
			t.Fatal(err)
		}
	}
	categories, err := db.ListCategories()
	if err != nil {
		t.Fatal(err)
	}

	full, err := loadMonthFull(db, "2026-11", categories, filter.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	fast, err := loadMonthFast(db, "2026-11", categories)
	if err != nil {
		t.Fatal(err)
	}
	if full.Count <= maxHistoryRows || len(full.Shown) != maxHistoryRows {
		t.Fatalf("o teste precisa de mais de %d lançamentos no mês (tem %d)", maxHistoryRows, full.Count)
	}
	if full.Outside == (fast.Outside) && full.Outside.InternalIn+full.Outside.InternalOut+full.Outside.Invested+full.Outside.Redeemed == 0 {
		t.Fatal("o teste precisa de lançamentos fora dos totais")
	}
	if !reflect.DeepEqual(full, fast) {
		t.Errorf("caminho rápido difere do completo:\nfull: %+v\nfast: %+v", summaryOf(full), summaryOf(fast))
		if !reflect.DeepEqual(full.Stats, fast.Stats) {
			t.Errorf("Stats:\nfull %+v %+v %+v\nfast %+v %+v %+v", full.Stats, full.Stats.Largest, full.Stats.Smallest, fast.Stats, fast.Stats.Largest, fast.Stats.Smallest)
		}
		if !reflect.DeepEqual(full.Shown, fast.Shown) {
			t.Error("Shown difere")
		}
	}
}

func summaryOf(m monthData) string {
	return fmt.Sprintf("summary=%+v outside=%+v count=%d hist=%d/%d shown=%d", m.Summary, m.Outside, m.Count, m.HistIncome, m.HistExpense, len(m.Shown))
}

// Mês sem lançamentos: os dois caminhos devolvem zeros (sem maior/menor despesa).
func TestLoadMonth_EmptyMonth(t *testing.T) {
	_, db := newTestHandler(t)
	categories, _ := db.ListCategories()
	full, err := loadMonthFull(db, "2026-11", categories, filter.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	fast, err := loadMonthFast(db, "2026-11", categories)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, fast) {
		t.Errorf("full %+v\nfast %+v", full, fast)
	}
	if fast.Stats.Largest != nil || fast.Count != 0 {
		t.Errorf("mês vazio: %+v", fast)
	}
}
