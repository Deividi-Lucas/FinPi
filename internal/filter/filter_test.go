package filter

import (
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

func parse(t *testing.T, raw string) (Filter, []string) {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return Parse(v)
}

func TestParse_EmptyIsInactiveAndMatchesEverything(t *testing.T) {
	f, warnings := parse(t, "")
	if f.Active() != 0 || len(warnings) != 0 {
		t.Errorf("ativo=%d avisos=%v", f.Active(), warnings)
	}
	if !f.Match("Lazer", "Qualquer coisa", "expense", 1) {
		t.Error("filtro vazio deixa tudo passar")
	}
	if f.Query() != "" {
		t.Errorf("query = %q, quer vazia", f.Query())
	}
}

func TestMatch_TextIgnoresCaseAccentsAndNeedsEveryWord(t *testing.T) {
	f, _ := parse(t, "q=pix+FULANO")
	if !f.Match("Outros", "Pix recebido Fulano de Tal", "income", 100) {
		t.Error("deveria achar 'Pix recebido Fulano de Tal'")
	}
	if f.Match("Outros", "Pix recebido Beltrana", "income", 100) {
		t.Error("todas as palavras precisam aparecer")
	}
	g, _ := parse(t, "q=SAUDE")
	if !g.Match("x", "Plano de Saúde", "expense", 1) {
		t.Error("acentos e maiúsculas não importam")
	}
	// A busca é só na descrição, não na categoria.
	h, _ := parse(t, "q=lazer")
	if h.Match("Lazer", "Cinema", "expense", 1) {
		t.Error("a busca olha a descrição, não a categoria")
	}
}

func TestMatch_CategoriesAreAnyOfAndIgnoreCaseAndAccents(t *testing.T) {
	f, _ := parse(t, "cat=Lazer&cat=Saúde")
	for _, c := range []string{"Lazer", "lazer", "Saúde", "saude", "SAÚDE"} {
		if !f.Match(c, "x", "expense", 1) {
			t.Errorf("categoria %q deveria passar", c)
		}
	}
	if f.Match("Moradia", "x", "expense", 1) {
		t.Error("Moradia não foi marcada")
	}
	if f.Active() != 1 {
		t.Errorf("várias categorias contam como um filtro: %d", f.Active())
	}
}

func TestMatch_Type(t *testing.T) {
	r, _ := parse(t, "tipo=receita")
	d, _ := parse(t, "tipo=despesa")
	if !r.Match("x", "x", "income", 1) || r.Match("x", "x", "expense", 1) {
		t.Error("tipo=receita só deixa income")
	}
	if !d.Match("x", "x", "expense", 1) || d.Match("x", "x", "income", 1) {
		t.Error("tipo=despesa só deixa expense")
	}
	bad, warnings := parse(t, "tipo=outro")
	if bad.Active() != 0 || len(warnings) != 0 {
		t.Errorf("tipo desconhecido é ignorado: ativo=%d avisos=%v", bad.Active(), warnings)
	}
}

func TestMatch_ValueRangeIsInclusive(t *testing.T) {
	f, warnings := parse(t, "min=10,00&max=20.50")
	if len(warnings) != 0 {
		t.Fatalf("avisos = %v", warnings)
	}
	cases := map[int64]bool{999: false, 1000: true, 1500: true, 2050: true, 2051: false}
	for cents, want := range cases {
		if got := f.Match("x", "x", "expense", cents); got != want {
			t.Errorf("%d centavos: %v, quer %v", cents, got, want)
		}
	}
	onlyMin, _ := parse(t, "min=5")
	if onlyMin.Match("x", "x", "expense", 499) || !onlyMin.Match("x", "x", "expense", 500) {
		t.Error("só mínimo")
	}
	onlyMax, _ := parse(t, "max=0,50")
	if !onlyMax.Match("x", "x", "expense", 50) || onlyMax.Match("x", "x", "expense", 51) {
		t.Error("só máximo")
	}
	zero, _ := parse(t, "min=0")
	if !zero.HasMin || zero.Active() != 1 {
		t.Error("mínimo zero é um filtro válido")
	}
}

func TestParse_InvalidValuesWarnAndAreIgnored(t *testing.T) {
	for name, raw := range map[string]string{
		"texto":         "min=abc",
		"três casas":    "max=1,234",
		"negativo":      "min=-5",
		"gigante":       "max=99999999999999",
		"vírgulas":      "min=1,2,3",
		"mínimo > máx.": "min=50&max=10",
	} {
		f, warnings := parse(t, raw)
		if len(warnings) == 0 {
			t.Errorf("%s: deveria avisar", name)
		}
		if f.HasMin && name != "mínimo > máx." && raw[:3] == "min" {
			t.Errorf("%s: mínimo inválido deveria ser ignorado", name)
		}
	}
	f, _ := parse(t, "min=50&max=10")
	if f.HasMin || f.HasMax {
		t.Error("com mínimo maior que o máximo, os dois são ignorados")
	}
}

func TestParse_CombinationUsesAnd(t *testing.T) {
	f, _ := parse(t, "q=pix&cat=Outros&tipo=receita&min=1&max=100")
	if f.Active() != 4 {
		t.Fatalf("filtros ativos = %d, quer 4", f.Active())
	}
	if !f.Match("Outros", "Pix recebido", "income", 5000) {
		t.Error("deveria passar em todos")
	}
	for name, ok := range map[string]bool{
		"texto":     f.Match("Outros", "Mercado", "income", 5000),
		"categoria": f.Match("Lazer", "Pix recebido", "income", 5000),
		"tipo":      f.Match("Outros", "Pix recebido", "expense", 5000),
		"valor":     f.Match("Outros", "Pix recebido", "income", 20000),
	} {
		if ok {
			t.Errorf("falhar em %s deveria excluir", name)
		}
	}
}

func TestParse_CategoriesAreCleanedAndBounded(t *testing.T) {
	f, _ := parse(t, "cat=Lazer&cat=+lazer+&cat=&cat=Saúde")
	if !reflect.DeepEqual(f.Categories, []string{"Lazer", "Saúde"}) {
		t.Errorf("categorias = %v", f.Categories)
	}
	many := url.Values{}
	for i := 0; i < 200; i++ {
		many.Add("cat", "c"+string(rune('a'+i%26))+string(rune('a'+i/26)))
	}
	g, _ := Parse(many)
	if len(g.Categories) > MaxCategories {
		t.Errorf("categorias = %d, máximo %d", len(g.Categories), MaxCategories)
	}
}

func TestQueryRoundTripsAndNormalizes(t *testing.T) {
	f, _ := parse(t, "q=+Pix++Fulano+&cat=Lazer&tipo=despesa&min=10&max=20,5&todos=1&lixo=1")
	q := f.Query()
	g, _ := parse(t, q)
	if g.Query() != q {
		t.Errorf("a query não é estável: %q depois %q", q, g.Query())
	}
	if !g.AllMonths || g.Text != "Pix Fulano" || g.Type != "expense" || g.MinCents != 1000 || g.MaxCents != 2050 {
		t.Errorf("filtro lido de volta = %+v", g)
	}
	v, _ := url.ParseQuery(q)
	if v.Get("lixo") != "" {
		t.Error("parâmetros desconhecidos não passam")
	}
}

func TestParse_ClampsTextLength(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	f, _ := Parse(url.Values{"q": {string(long)}})
	if len([]rune(f.Text)) > MaxTextLen {
		t.Errorf("texto com %d caracteres, máximo %d", len([]rune(f.Text)), MaxTextLen)
	}
}

func TestSelected(t *testing.T) {
	f, _ := parse(t, "cat=saude&cat=LAZER")
	if !f.Selected("Saúde") || !f.Selected("Lazer") || f.Selected("Moradia") {
		t.Error("Selected compara pelo nome normalizado")
	}
}

func TestParse_AccountsRepeatedWithNoAccount(t *testing.T) {
	f, warnings := parse(t, "conta=3&conta=0&conta=3&conta=+7+")
	if len(warnings) != 0 {
		t.Errorf("avisos inesperados: %v", warnings)
	}
	if !reflect.DeepEqual(f.Accounts, []int64{3, 0, 7}) {
		t.Errorf("Accounts = %v, quer [3 0 7] (sem repetidas)", f.Accounts)
	}
	if !f.HasAccount(3) || !f.HasAccount(0) || !f.HasAccount(7) || f.HasAccount(4) {
		t.Error("HasAccount diz quais contas estão marcadas")
	}
	if f.Active() != 1 {
		t.Errorf("o filtro de conta conta como um só: ativo=%d", f.Active())
	}
}

func TestParse_InvalidAccountWarnsOnceAndIsIgnored(t *testing.T) {
	f, warnings := parse(t, "conta=abc&conta=-2&conta=5&conta=1.5&conta=")
	if !reflect.DeepEqual(f.Accounts, []int64{5}) {
		t.Errorf("Accounts = %v, quer [5]", f.Accounts)
	}
	if !reflect.DeepEqual(warnings, []string{"Conta inválida ignorada."}) {
		t.Errorf("avisos = %v, quer um só aviso de conta inválida", warnings)
	}
	g, warnings := parse(t, "conta=x")
	if len(g.Accounts) != 0 || g.Active() != 0 || len(warnings) != 1 {
		t.Errorf("só inválidas: contas=%v ativo=%d avisos=%v", g.Accounts, g.Active(), warnings)
	}
}

func TestParse_AccountsAreBounded(t *testing.T) {
	v := url.Values{}
	for i := 1; i <= MaxAccounts+10; i++ {
		v.Add("conta", strconv.Itoa(i))
	}
	f, _ := Parse(v)
	if len(f.Accounts) != MaxAccounts {
		t.Errorf("contas = %d, máximo %d", len(f.Accounts), MaxAccounts)
	}
}

func TestMatchAccount(t *testing.T) {
	none, _ := parse(t, "")
	if !none.MatchAccount(0) || !none.MatchAccount(9) {
		t.Error("sem filtro de conta tudo passa")
	}
	f, _ := parse(t, "conta=2")
	if !f.MatchAccount(2) || f.MatchAccount(3) || f.MatchAccount(0) {
		t.Error("conta=2 deixa passar só a conta 2")
	}
	g, _ := parse(t, "conta=0&conta=4")
	if !g.MatchAccount(0) || !g.MatchAccount(4) || g.MatchAccount(2) {
		t.Error("conta=0 é Sem conta; contas marcadas se combinam com ou")
	}
}

func TestQuery_AccountsRoundTrip(t *testing.T) {
	f, _ := parse(t, "conta=4&conta=0&cat=Lazer")
	if got, want := f.Query(), "cat=Lazer&conta=4&conta=0"; got != want {
		t.Errorf("Query = %q, quer %q", got, want)
	}
	g, warnings := parse(t, f.Query())
	if len(warnings) != 0 || !reflect.DeepEqual(g.Accounts, f.Accounts) || g.Query() != f.Query() {
		t.Errorf("ida e volta: %v %q", g.Accounts, g.Query())
	}
	if !reflect.DeepEqual(f.Values()["conta"], []string{"4", "0"}) {
		t.Errorf("Values conta = %v", f.Values()["conta"])
	}
}
