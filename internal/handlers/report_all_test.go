package handlers

import (
	"strings"
	"testing"

	"finpi/internal/database"
)

// txIDs devolve o ID de cada lançamento (de qualquer livro) pela descrição.
func txIDs(t *testing.T, db *database.DB) map[string]int64 {
	t.Helper()
	all, _, err := db.AllTransactions(100)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int64{}
	for _, tx := range all {
		out[tx.Description] = tx.ID
	}
	return out
}

// seedTwoBooks: Pessoal (Salário 10.000, Mercado 2.000, Pix para PJ 3.000) e PJ (Pix do Pessoal 3.000, DAS 1.000).
func seedTwoBooks(t *testing.T, h *Handler, db *database.DB) *database.DB {
	t.Helper()
	pj, _ := newPJ(t, db)
	mustTx(t, db, "Salário", "Salário", "income", 1000000, "2026-10-05")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 200000, "2026-10-06")
	mustTx(t, db, "Pix para PJ", "Outros", "expense", 300000, "2026-10-07")
	mustTx(t, pj, "Pix do Pessoal", "Outros", "income", 300000, "2026-10-07")
	mustTx(t, pj, "DAS", "Impostos", "expense", 100000, "2026-10-08")
	return pj
}

func TestReport_AllBooks_SumsBooksWithoutLinkedTransfers(t *testing.T) {
	h, db := newTestHandler(t)
	seedTwoBooks(t, h, db)

	// sem ligar, a transferência conta nas duas pontas: entradas 13.000, saídas 6.000
	code, body := getReport(h, "/relatorio?ano=2026&livro=todos")
	if code != 200 || !strings.Contains(body, "R$ 13.000,00") || !strings.Contains(body, "R$ 6.000,00") {
		t.Fatalf("antes de ligar: %d %.300s", code, body)
	}
	if !strings.Contains(body, "Todos os livros") {
		t.Error("o seletor deveria mostrar Todos os livros")
	}

	ids := txIDs(t, db)
	if err := db.LinkTransfer(ids["Pix para PJ"], ids["Pix do Pessoal"]); err != nil {
		t.Fatal(err)
	}
	_, body = getReport(h, "/relatorio?ano=2026&livro=todos")
	if strings.Contains(body, "R$ 13.000,00") || !strings.Contains(body, "R$ 10.000,00") || !strings.Contains(body, "R$ 7.000,00") {
		t.Errorf("ligado: entradas 10.000, saídas 3.000, resultado 7.000 — %.500s", body)
	}
	if !strings.Contains(body, "Transferências entre livros (fora do fluxo): entradas R$ 3.000,00 &middot; saídas R$ 3.000,00") {
		t.Errorf("deveria mostrar a linha das transferências ligadas, %.200s", body)
	}

	// o relatório de um livro continua sendo só dele, sem a linha nova
	_, own := getReport(h, "/relatorio?ano=2026")
	if strings.Contains(own, "Transferências entre livros") || strings.Contains(own, "DAS") || !strings.Contains(own, "R$ 5.000,00") {
		t.Errorf("o relatório do Pessoal deveria seguir igual (saídas 5.000, sem o PJ): %.300s", own)
	}
}

func TestReport_AllBooks_SoloInternalAndArchivedAreOut(t *testing.T) {
	h, db := newTestHandler(t)
	pj := seedTwoBooks(t, h, db)
	if _, err := db.CreateCategory("Movimentação interna"); err != nil {
		t.Fatal(err)
	}
	mustTx(t, db, "Resgate", "Movimentação interna", "income", 55500, "2026-10-09")
	mustTx(t, pj, "Pix para fora", "Outros", "expense", 7700, "2026-10-10")
	ids := txIDs(t, db)
	if _, _, err := db.MarkSolo([]int64{ids["Pix para fora"]}); err != nil {
		t.Fatal(err)
	}

	_, body := getReport(h, "/relatorio?ano=2026&livro=todos")
	// saídas = Mercado 2.000 + Pix para PJ 3.000 + DAS 1.000 (a marcada R$ 77,00 e o resgate ficam de fora)
	if !strings.Contains(body, "R$ 6.000,00") {
		t.Errorf("saídas deveriam ser R$ 6.000,00: %.400s", body)
	}
	if !strings.Contains(body, "Movimentações internas") || !strings.Contains(body, "Fora do FinPi") || !strings.Contains(body, "R$ 77,00") {
		t.Errorf("deveria mostrar as linhas de movimentações internas e Fora do FinPi: %.300s", body)
	}

	// arquivar o PJ tira os lançamentos dele
	books, _ := db.ListBooks()
	for _, b := range books {
		if b.Name == "PJ" {
			if err := db.SetBookArchived(b.ID, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	_, body = getReport(h, "/relatorio?ano=2026&livro=todos")
	if strings.Contains(body, "DAS") || !strings.Contains(body, "R$ 5.000,00") { // Mercado 2.000 + Pix para PJ 3.000
		t.Errorf("com o PJ arquivado só contam as saídas do Pessoal: %.300s", body)
	}
}

func TestReport_AllBooks_LinkWithArchivedBookKeepsActiveSideCounting(t *testing.T) {
	h, db := newTestHandler(t)
	seedTwoBooks(t, h, db)
	ids := txIDs(t, db)
	if err := db.LinkTransfer(ids["Pix para PJ"], ids["Pix do Pessoal"]); err != nil {
		t.Fatal(err)
	}
	books, _ := db.ListBooks()
	for _, b := range books {
		if b.Name == "PJ" {
			_ = db.SetBookArchived(b.ID, true)
		}
	}
	_, body := getReport(h, "/relatorio?ano=2026&livro=todos")
	// o lado ativo volta a contar: saídas 5.000 (Mercado + Pix para PJ)
	if !strings.Contains(body, "R$ 5.000,00") {
		t.Errorf("a saída do Pessoal deveria contar de novo: %.300s", body)
	}
}

func TestReport_AllBooks_CategoryFilterMatchesByNameAcrossBooks(t *testing.T) {
	h, db := newTestHandler(t)
	pj := seedTwoBooks(t, h, db)
	if _, err := pj.CreateCategory("Impostos"); err != nil {
		t.Fatal(err)
	}

	// "Outros" existe nos dois livros: o filtro pega as duas saídas/entradas dessa categoria
	_, body := getReport(h, "/relatorio?ano=2026&livro=todos&cat=Outros")
	if !strings.Contains(body, "R$ 3.000,00") || strings.Contains(body, "R$ 13.000,00") {
		t.Errorf("filtro Outros: entradas 3.000 e saídas 3.000, %.300s", body)
	}
	// a categoria que só o PJ tem aparece na lista de escolha
	if !strings.Contains(body, "Impostos") {
		t.Error("a lista de categorias deveria trazer as dos dois livros")
	}
	if n := strings.Count(body, `type="checkbox" name="cat" value="Outros"`); n != 1 {
		t.Errorf("Outros deveria aparecer uma vez na lista (união por nome), apareceu %d", n)
	}
}

func TestReport_AllBooks_LinksAndFormsKeepAllBooks(t *testing.T) {
	h, db := newTestHandler(t)
	seedTwoBooks(t, h, db)
	_, body := getReport(h, "/relatorio?de=2026-10-01&ate=2026-10-31&livro=todos&previstas=1")
	if !strings.Contains(body, `name="livro" value="todos"`) {
		t.Error("os formulários deveriam carregar livro=todos")
	}
	if strings.Contains(body, `value="0"`) && strings.Contains(body, `name="livro" value="0"`) {
		t.Error("não pode haver livro=0")
	}
	if !strings.Contains(body, "livro=todos") {
		t.Error("os links deveriam manter livro=todos")
	}
	if !strings.Contains(body, `href="/relatorio?livro=todos"`) {
		t.Error("o seletor deveria ter a opção Todos os livros")
	}
	if !strings.Contains(body, `href="/visao-geral"`) {
		t.Error("deveria haver atalho para a Visão geral")
	}
	if strings.Contains(body, `href="/?livro=0"`) {
		t.Error("Voltar ao mês não pode levar livro=0")
	}
}

func TestReport_AllBooks_ProjectedInstallmentsFromAllBooks(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := newPJ(t, db)
	mustTx(t, db, "Geladeira 3/10", "Casa", "expense", 25000, "2026-10-15")
	mustTx(t, pj, "Notebook 2/5", "Equipamento", "expense", 40000, "2026-10-15")

	_, on := getReport(h, "/relatorio?ano=2026&livro=todos&previstas=1")
	// real 650,00 (250 + 400) e mais 650,00 em novembro e em dezembro, uma parcela de cada livro por mês
	if !strings.Contains(on, "R$ 1.950,00") {
		t.Errorf("total com as previstas dos dois livros deveria ser R$ 1.950,00: %.300s", on)
	}
	if n := strings.Count(on, "data-projected"); n != 2 {
		t.Errorf("fatias previstas = %d, quer 2 (novembro e dezembro)", n)
	}
}

func TestReport_SingleBookStillOffersAllBooksOption(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	mustTx(t, pj, "DAS", "Impostos", "expense", 1000, "2026-10-08")
	_, body := getReport(h, "/relatorio?ano=2026&livro="+pjID)
	if strings.Contains(body, `name="livro" value="todos"`) {
		t.Error("o relatório de um livro não deveria virar Todos os livros")
	}
	if !strings.Contains(body, `href="/relatorio?livro=todos"`) {
		t.Error("o seletor deveria oferecer Todos os livros")
	}
}
