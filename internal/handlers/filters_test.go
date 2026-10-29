package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
)

// history devolve o trecho do HTML da tabela do histórico (a lista de lançamentos).
func history(body string) string {
	return slice(body, `id="history"`, `id="history-end"`)
}

func seedFilterData(t *testing.T, db *database.DB) {
	t.Helper()
	mustTx(t, db, "Mercado", "Alimentação", "expense", 10000, "2026-10-05")
	mustTx(t, db, "Cinema", "Lazer", "expense", 4500, "2026-10-08")
	mustTx(t, db, "Pix recebido Fulano de Tal", "Outros", "income", 50000, "2026-10-10")
	mustTx(t, db, "Mercado", "Alimentação", "expense", 8000, "2026-09-15") // outro mês
}

func TestDashboard_NoFilterShowsWholeMonth(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	list := history(getDashboard(h, "/?month=2026-10"))
	for _, want := range []string{"Mercado", "Cinema", "Pix recebido Fulano de Tal"} {
		if !strings.Contains(list, want) {
			t.Errorf("histórico sem %q", want)
		}
	}
	if strings.Contains(list, "15/09/2026") {
		t.Error("sem filtro o histórico continua sendo só do mês selecionado")
	}
}

func TestDashboard_FilterByTextCategoryTypeAndValue(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	cases := map[string]struct {
		query string
		want  []string
		not   []string
	}{
		"texto":           {"q=pix+fulano", []string{"Pix recebido Fulano de Tal"}, []string{"Mercado", "Cinema"}},
		"categoria":       {"cat=Lazer", []string{"Cinema"}, []string{"Mercado", "Pix recebido"}},
		"várias":          {"cat=Lazer&cat=" + url.QueryEscape("Alimentação"), []string{"Cinema", "Mercado"}, []string{"Pix recebido"}},
		"tipo receita":    {"tipo=receita", []string{"Pix recebido"}, []string{"Mercado", "Cinema"}},
		"tipo despesa":    {"tipo=despesa", []string{"Mercado", "Cinema"}, []string{"Pix recebido"}},
		"valor mínimo":    {"min=100", []string{"Mercado", "Pix recebido"}, []string{"Cinema"}},
		"valor máximo":    {"max=45,00", []string{"Cinema"}, []string{"Mercado", "Pix recebido"}},
		"combinados":      {"tipo=despesa&min=50&max=200&q=mercado", []string{"Mercado"}, []string{"Cinema", "Pix recebido"}},
		"sem resultados":  {"q=inexistente", nil, []string{"Mercado", "Cinema", "Pix recebido"}},
		"acentos e caixa": {"q=" + url.QueryEscape("CINÉMA"), []string{"Cinema"}, []string{"Mercado"}},
	}
	for name, c := range cases {
		list := history(getDashboard(h, "/?month=2026-10&"+c.query))
		for _, w := range c.want {
			if !strings.Contains(list, w) {
				t.Errorf("%s: histórico sem %q", name, w)
			}
		}
		for _, n := range c.not {
			if strings.Contains(list, n) {
				t.Errorf("%s: histórico não deveria ter %q", name, n)
			}
		}
	}
}

func TestDashboard_FilterDoesNotChangeMonthCards(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	body := getDashboard(h, "/?month=2026-10&q=cinema")
	// Receitas R$ 500,00; despesas R$ 145,00; saldo R$ 355,00 — do mês inteiro, não da lista filtrada.
	for _, want := range []string{"R$ 500,00", "R$ 145,00", "R$ 355,00"} {
		if !strings.Contains(body, want) {
			t.Errorf("os cartões do mês deveriam continuar sem filtro: falta %q", want)
		}
	}
	if !strings.Contains(body, "Dashboard do mês") {
		t.Error("o dashboard do mês continua na página")
	}
}

func TestDashboard_FilteredSummaryAndTitle(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	body := getDashboard(h, "/?month=2026-10&tipo=despesa")
	summary := slice(body, `id="history-summary"`, `</p>`)
	for _, want := range []string{"2 lançamentos", "receitas R$ 0,00", "despesas R$ 145,00"} {
		if !strings.Contains(summary, want) {
			t.Errorf("resumo da lista sem %q: %s", want, summary)
		}
	}
	if one := slice(getDashboard(h, "/?month=2026-10&q=cinema"), `id="history-summary"`, `</p>`); !strings.Contains(one, "1 lançamento ") {
		t.Errorf("singular: %s", one)
	}
}

func TestDashboard_AllMonthsSearchesEverythingNewestFirst(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)

	only := history(getDashboard(h, "/?month=2026-10&q=mercado"))
	if !strings.Contains(only, "05/10/2026") || strings.Contains(only, "15/09/2026") {
		t.Error("sem 'todos os meses' a busca fica no mês selecionado")
	}

	all := history(getDashboard(h, "/?month=2026-10&q=mercado&todos=1"))
	oct, sep := strings.Index(all, "05/10/2026"), strings.Index(all, "15/09/2026")
	if oct < 0 || sep < 0 {
		t.Fatalf("a busca em todos os meses deveria achar outubro e setembro: %s", all)
	}
	if oct > sep {
		t.Error("do mais recente para o mais antigo")
	}
	// Os cartões continuam sendo do mês escolhido.
	if body := getDashboard(h, "/?month=2026-10&q=mercado&todos=1"); !strings.Contains(body, "R$ 145,00") {
		t.Error("cartões do mês intactos com 'todos os meses'")
	}
}

func TestDashboard_ListIsCappedAt500(t *testing.T) {
	h, db := newTestHandler(t)
	for i := 0; i < 501; i++ {
		mustTx(t, db, fmt.Sprintf("Item %03d", i), "Outros", "expense", 100, "2026-10-05")
	}
	body := getDashboard(h, "/?month=2026-10&q=item")
	list := history(body)
	if n := strings.Count(list, "Editar lançamento"); n != 500 {
		t.Errorf("linhas = %d, quer 500", n)
	}
	if !strings.Contains(slice(body, `id="history-summary"`, `</p>`), "501 lançamentos") || !strings.Contains(body, "500 mais recentes") {
		t.Error("deveria avisar que a lista foi limitada aos 500 mais recentes")
	}
}

func TestDashboard_InvalidFilterValuesWarn(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	body := getDashboard(h, "/?month=2026-10&min=abc")
	if !strings.Contains(body, "Valor mínimo inválido") {
		t.Error("deveria avisar do valor inválido")
	}
	if !strings.Contains(history(body), "Mercado") {
		t.Error("o valor inválido é ignorado: a lista continua completa")
	}
}

func TestDashboard_FilterBarReflectsState(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	body := getDashboard(h, "/?month=2026-10&q=pix&cat=Lazer&tipo=receita&min=1,50&max=99&todos=1&previstas=1")
	bar := slice(body, `id="filters"`, `id="history"`)
	for _, want := range []string{
		`id="filters-form"`, `name="q" value="pix"`, `name="min" value="1,50"`, `name="max" value="99,00"`,
		`name="todos"`, `name="cat" value="Lazer" checked`, `value="receita" checked`, "4 filtros ativos", "Limpar",
	} {
		if !strings.Contains(bar, want) {
			t.Errorf("barra de filtros sem %q", want)
		}
	}
	if strings.Contains(bar, `name="cat" value="Alimentação" checked`) {
		t.Error("só as categorias marcadas ficam marcadas")
	}
	if !strings.Contains(bar, `name="month" value="2026-10"`) || !strings.Contains(bar, `name="previstas" value="1"`) {
		t.Error("o formulário de filtros preserva o mês e as previstas")
	}
	// Os filtros também acompanham a troca de mês (formulário do cabeçalho).
	header := slice(body, `<header`, `</header>`)
	if !strings.Contains(header, `name="q" value="pix"`) || !strings.Contains(header, `name="cat" value="Lazer"`) {
		t.Error("o seletor de mês deveria levar os filtros junto")
	}
}

func TestDashboard_NoActiveFiltersHidesBadge(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/")
	if strings.Contains(body, "filtro ativo") || strings.Contains(body, "filtros ativos") {
		t.Error("sem filtros não há selo de filtros ativos")
	}
	if !strings.Contains(body, `id="filters"`) {
		t.Error("a barra de filtros sempre existe")
	}
}

func TestRedirectsKeepNormalizedFilters(t *testing.T) {
	h, db := newTestHandler(t)
	tx := seed(t, db, 1999)
	filtros := "q=pix&tipo=receita&cat=Lazer&todos=1&lixo=1"

	form := validForm()
	form.Set("filtros", filtros)
	loc := post(h.CreateTransaction, "/transactions", form).Header().Get("Location")
	for _, want := range []string{"/?month=2026-10", "q=pix", "tipo=receita", "cat=Lazer", "todos=1"} {
		if !strings.Contains(loc, want) {
			t.Errorf("criar: Location %q sem %q", loc, want)
		}
	}
	if strings.Contains(loc, "lixo") {
		t.Errorf("parâmetros desconhecidos não passam: %q", loc)
	}

	up := validForm()
	up.Set("id", strconv.FormatInt(tx.ID, 10))
	up.Set("filtros", filtros)
	if loc := post(h.UpdateTransaction, "/transactions/update", up).Header().Get("Location"); !strings.Contains(loc, "q=pix") {
		t.Errorf("editar: Location %q", loc)
	}

	del := url.Values{"id": {strconv.FormatInt(tx.ID, 10)}, "month": {"2026-10"}, "filtros": {filtros}}
	if loc := post(h.DeleteTransaction, "/transactions/delete", del).Header().Get("Location"); !strings.Contains(loc, "tipo=receita") {
		t.Errorf("excluir: Location %q", loc)
	}

	cat := url.Values{"name": {"Pets"}, "month": {"2026-10"}, "filtros": {"q=pix"}}
	if loc := post(h.CreateCategory, "/categories", cat).Header().Get("Location"); !strings.Contains(loc, "categorias=1") || !strings.Contains(loc, "q=pix") {
		t.Errorf("categorias: Location %q", loc)
	}
}

func TestRedirectsWithoutFiltersStayTheSame(t *testing.T) {
	h, _ := newTestHandler(t)
	loc := post(h.CreateTransaction, "/transactions", validForm()).Header().Get("Location")
	if loc != "/?month=2026-10" {
		t.Errorf("Location = %q", loc)
	}
}

func TestFormsCarryTheCurrentFilters(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	body := getDashboard(h, "/?month=2026-10&q=cinema&cat=Lazer")
	// Novo lançamento, edição, exclusão e categorias levam os filtros em um campo oculto.
	if n := strings.Count(body, `name="filtros" value="`); n < 4 {
		t.Errorf("campos 'filtros' = %d, quer pelo menos 4", n)
	}
	if !strings.Contains(body, `name="filtros" value="cat=Lazer&amp;q=cinema"`) {
		t.Error("o campo 'filtros' deveria trazer a query normalizada")
	}
}

func TestFiltersFormGuardsLiveUpdates(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/")
	if !strings.Contains(body, "filters-form") || !strings.Contains(body, "defaultValue") {
		t.Error("o tempo real deve proteger o formulário de filtros em edição")
	}
}

func TestErrorFromCategoryActionKeepsFilters(t *testing.T) {
	h, db := newTestHandler(t)
	seedFilterData(t, db)
	form := url.Values{"name": {" "}, "month": {"2026-10"}, "filtros": {"q=cinema"}}
	rec := post(h.CreateCategory, "/categories", form)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	if list := history(rec.Body.String()); !strings.Contains(list, "Cinema") || strings.Contains(list, "Mercado") {
		t.Error("a página de erro também respeita o filtro")
	}
}
