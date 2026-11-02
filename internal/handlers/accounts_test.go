package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"finpi/internal/models"
)

func TestCreateAccount_RedirectsAndStores(t *testing.T) {
	h, db := newTestHandler(t)
	rec := post(h.CreateAccount, "/contas", url.Values{"livro": {"1"}, "name": {"  Carteira  "}, "month": {"2026-10"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "livros=1") || !strings.Contains(loc, "livro=1") {
		t.Errorf("deveria voltar à tela de livros: %s", loc)
	}
	accs, _ := db.ListAccounts()
	if len(accs) != 1 || accs[0].Name != "Carteira" {
		t.Fatalf("contas: %+v", accs)
	}
}

func TestCreateAccount_Errors(t *testing.T) {
	h, db := newTestHandler(t)
	if _, err := db.CreateAccount("Carteira", ""); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		form   url.Values
		status int
		msg    string
	}{
		{"vazio", url.Values{"livro": {"1"}, "name": {"   "}}, http.StatusBadRequest, "Informe o nome da conta."},
		{"longo", url.Values{"livro": {"1"}, "name": {strings.Repeat("a", 41)}}, http.StatusBadRequest, "Informe o nome da conta."},
		{"duplicado", url.Values{"livro": {"1"}, "name": {"carteira"}}, http.StatusConflict, "Já existe uma conta com esse nome neste livro."},
		{"livro inválido", url.Values{"livro": {"99"}, "name": {"X"}}, http.StatusBadRequest, "Livro inválido"},
	}
	for _, c := range cases {
		rec := post(h.CreateAccount, "/contas", c.form)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.msg) {
			t.Errorf("%s: status %d, corpo sem %q: %.200s", c.name, rec.Code, c.msg, rec.Body.String())
		}
	}
}

func TestCreateAccount_SameNameInAnotherBookIsFine(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := db.CreateBook("PJ", "sky", 0)
	_, _ = db.CreateAccount("Carteira", "")
	rec := post(h.CreateAccount, "/contas", url.Values{"livro": {idStr(pj.ID)}, "name": {"Carteira"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if accs, _ := db.ForBook(pj.ID).ListAccounts(); len(accs) != 1 {
		t.Errorf("contas do PJ: %+v", accs)
	}
}

func TestRenameAccount(t *testing.T) {
	h, db := newTestHandler(t)
	a, _ := db.CreateAccount("Carteira", "")
	_, _ = db.CreateAccount("Cofre", "")
	pj, _ := db.CreateBook("PJ", "sky", 0)
	other, _ := db.ForBook(pj.ID).CreateAccount("Caixa", "")

	if rec := post(h.UpdateAccount, "/contas/editar", url.Values{"livro": {"1"}, "id": {idStr(a.ID)}, "name": {"Carteira nova"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("renomear: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := db.GetAccount(a.ID); got.Name != "Carteira nova" {
		t.Errorf("não renomeou: %+v", got)
	}
	cases := []struct {
		name   string
		form   url.Values
		status int
	}{
		{"vazio", url.Values{"livro": {"1"}, "id": {idStr(a.ID)}, "name": {""}}, http.StatusBadRequest},
		{"duplicado", url.Values{"livro": {"1"}, "id": {idStr(a.ID)}, "name": {"COFRE"}}, http.StatusConflict},
		{"outro livro", url.Values{"livro": {"1"}, "id": {idStr(other.ID)}, "name": {"X"}}, http.StatusNotFound},
		{"sem id", url.Values{"livro": {"1"}, "name": {"X"}}, http.StatusBadRequest},
	}
	for _, c := range cases {
		if rec := post(h.UpdateAccount, "/contas/editar", c.form); rec.Code != c.status {
			t.Errorf("%s: status %d, esperado %d", c.name, rec.Code, c.status)
		}
	}
}

func TestArchiveAccount(t *testing.T) {
	h, db := newTestHandler(t)
	a, _ := db.CreateAccount("Carteira", "")
	id := idStr(a.ID)
	if rec := post(h.ArchiveAccount, "/contas/arquivar", url.Values{"livro": {"1"}, "id": {id}, "arquivar": {"1"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("arquivar: %d %s", rec.Code, rec.Body.String())
	}
	if got, _ := db.GetAccount(a.ID); !got.Archived {
		t.Error("deveria estar arquivada")
	}
	if rec := post(h.ArchiveAccount, "/contas/arquivar", url.Values{"livro": {"1"}, "id": {id}, "arquivar": {"0"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("reativar: %d", rec.Code)
	}
	if got, _ := db.GetAccount(a.ID); got.Archived {
		t.Error("deveria estar ativa")
	}
	if rec := post(h.ArchiveAccount, "/contas/arquivar", url.Values{"livro": {"1"}, "id": {"999"}, "arquivar": {"1"}}); rec.Code != http.StatusNotFound {
		t.Errorf("conta inexistente: %d", rec.Code)
	}
}

func TestAccountRoutes_RejectGet(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, fn := range []http.HandlerFunc{h.CreateAccount, h.UpdateAccount, h.ArchiveAccount} {
		rec := httptest.NewRecorder()
		fn(rec, httptest.NewRequest(http.MethodGet, "/contas", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET deveria dar 405: %d", rec.Code)
		}
	}
}

func TestAccountWrites_Publish(t *testing.T) {
	h, db := newTestHandler(t)
	ch, cancel, err := h.events.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	post(h.CreateAccount, "/contas", url.Values{"livro": {"1"}, "name": {"Carteira"}})
	select {
	case <-ch:
	default:
		t.Error("criar conta deveria publicar o evento")
	}
	a, _ := db.GetAccount(1)
	post(h.ArchiveAccount, "/contas/arquivar", url.Values{"livro": {"1"}, "id": {idStr(a.ID)}, "arquivar": {"1"}})
	select {
	case <-ch:
	default:
		t.Error("arquivar conta deveria publicar o evento")
	}
}

func getPage(h *Handler, target string) string {
	rec := httptest.NewRecorder()
	h.Dashboard(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec.Body.String()
}

func TestBooksScreen_ListsAccountsPerBook(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := db.CreateBook("PJ", "sky", 0)
	_, _ = db.CreateAccount("Carteira Pessoal", "")
	_, _ = db.ForBook(pj.ID).CreateAccount("Caixa da Empresa", "")
	body := getPage(h, "/?month=2026-10&livros=1")
	for _, want := range []string{"Carteira Pessoal", "Caixa da Empresa", `action="/contas"`, `action="/contas/editar"`, `action="/contas/arquivar"`} {
		if !strings.Contains(body, want) {
			t.Errorf("tela de livros sem %q", want)
		}
	}
}

func TestAccountForms_CarryBookField(t *testing.T) {
	h, db := newTestHandler(t)
	_, _ = db.CreateAccount("Carteira", "")
	body := getPage(h, "/?month=2026-10&livros=1")
	for _, action := range []string{"/contas", "/contas/editar", "/contas/arquivar"} {
		i := strings.Index(body, `action="`+action+`"`)
		if i < 0 {
			t.Fatalf("sem formulário %s", action)
		}
		end := strings.Index(body[i:], "</form>")
		if !strings.Contains(body[i:i+end], `name="livro"`) || !strings.Contains(body[i:i+end], `name="month"`) {
			t.Errorf("formulário %s sem os campos livro/month", action)
		}
	}
}

func TestCreateTransaction_StoresAccount(t *testing.T) {
	h, db := newTestHandler(t)
	a, _ := db.CreateAccount("Carteira", "")
	form := validForm()
	form.Set("conta", idStr(a.ID))
	if rec := post(h.CreateTransaction, "/transactions", form); rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	list, _ := db.GetTransactions("2026-10")
	if len(list) != 1 || list[0].AccountID != a.ID {
		t.Fatalf("conta não gravada: %+v", list)
	}

	// Vazio ou ausente = sem conta.
	for _, v := range []string{"", "0"} {
		f := validForm()
		f.Set("conta", v)
		if rec := post(h.CreateTransaction, "/transactions", f); rec.Code != http.StatusSeeOther {
			t.Fatalf("conta=%q: %d", v, rec.Code)
		}
	}
	if rec := post(h.CreateTransaction, "/transactions", validForm()); rec.Code != http.StatusSeeOther {
		t.Fatalf("sem campo: %d", rec.Code)
	}
	list, _ = db.GetTransactions("2026-10")
	without := 0
	for _, tx := range list {
		if tx.AccountID == 0 {
			without++
		}
	}
	if without != 3 {
		t.Errorf("esperava 3 sem conta, achei %d", without)
	}
}

func TestCreateTransaction_InvalidAccountIs400(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := db.CreateBook("PJ", "sky", 0)
	foreign, _ := db.ForBook(pj.ID).CreateAccount("Caixa", "")
	archived, _ := db.CreateAccount("Velha", "")
	_ = db.SetAccountArchived(archived.ID, true)
	for name, v := range map[string]string{"outro livro": idStr(foreign.ID), "arquivada": idStr(archived.ID), "inexistente": "999", "texto": "abc", "negativo": "-3"} {
		f := validForm()
		f.Set("conta", v)
		if rec := post(h.CreateTransaction, "/transactions", f); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
	if list, _ := db.GetTransactions("2026-10"); len(list) != 0 {
		t.Errorf("nada deveria ter sido gravado: %+v", list)
	}
}

func updateForm(id int64) url.Values {
	f := validForm()
	f.Set("id", idStr(id))
	return f
}

func TestUpdateTransaction_AccountRules(t *testing.T) {
	h, db := newTestHandler(t)
	a, _ := db.CreateAccount("Carteira", "")
	b, _ := db.CreateAccount("Cofre", "")
	tx := models.Transaction{Description: "Mercado", AmountCents: 1000, Type: "expense", Category: "Alimentação", Date: "2026-10-05", AccountID: a.ID}
	if err := db.CreateTransaction(&tx); err != nil {
		t.Fatal(err)
	}
	accountOf := func() int64 {
		list, _ := db.GetTransactionsByIDs([]int64{tx.ID})
		return list[0].AccountID
	}

	// Sem o campo conta: preserva (nunca zera por omissão).
	if rec := post(h.UpdateTransaction, "/transactions/update", updateForm(tx.ID)); rec.Code != http.StatusSeeOther {
		t.Fatalf("sem conta: %d %s", rec.Code, rec.Body.String())
	}
	if got := accountOf(); got != a.ID {
		t.Fatalf("editar sem o campo conta zerou/trocou a conta: %d", got)
	}

	// Conta válida diferente: troca.
	f := updateForm(tx.ID)
	f.Set("conta", idStr(b.ID))
	if rec := post(h.UpdateTransaction, "/transactions/update", f); rec.Code != http.StatusSeeOther {
		t.Fatalf("trocar: %d %s", rec.Code, rec.Body.String())
	}
	if got := accountOf(); got != b.ID {
		t.Fatalf("não trocou: %d", got)
	}

	// conta=0 limpa.
	f = updateForm(tx.ID)
	f.Set("conta", "0")
	if rec := post(h.UpdateTransaction, "/transactions/update", f); rec.Code != http.StatusSeeOther {
		t.Fatalf("limpar: %d", rec.Code)
	}
	if got := accountOf(); got != 0 {
		t.Fatalf("não limpou: %d", got)
	}

	// Conta de outro livro: 400, mantém.
	pj, _ := db.CreateBook("PJ", "sky", 0)
	foreign, _ := db.ForBook(pj.ID).CreateAccount("Caixa", "")
	f = updateForm(tx.ID)
	f.Set("conta", idStr(a.ID))
	post(h.UpdateTransaction, "/transactions/update", f)
	f.Set("conta", idStr(foreign.ID))
	if rec := post(h.UpdateTransaction, "/transactions/update", f); rec.Code != http.StatusBadRequest {
		t.Fatalf("outro livro: %d", rec.Code)
	}
	if got := accountOf(); got != a.ID {
		t.Fatalf("conta mudou após erro: %d", got)
	}

	// Conta arquivada nova: 400; a mesma arquivada que já tinha: ok.
	_ = db.SetAccountArchived(b.ID, true)
	f.Set("conta", idStr(b.ID))
	if rec := post(h.UpdateTransaction, "/transactions/update", f); rec.Code != http.StatusBadRequest {
		t.Fatalf("arquivada nova: %d", rec.Code)
	}
	_ = db.SetAccountArchived(b.ID, false)
	post(h.UpdateTransaction, "/transactions/update", func() url.Values { g := updateForm(tx.ID); g.Set("conta", idStr(b.ID)); return g }())
	_ = db.SetAccountArchived(b.ID, true)
	f.Set("conta", idStr(b.ID))
	f.Set("description", "Mercado 2")
	if rec := post(h.UpdateTransaction, "/transactions/update", f); rec.Code != http.StatusSeeOther {
		t.Fatalf("mantida arquivada: %d %s", rec.Code, rec.Body.String())
	}
	if got := accountOf(); got != b.ID {
		t.Fatalf("deveria manter a arquivada: %d", got)
	}
	// Sem o campo, com a conta arquivada em uso: preserva também.
	g := updateForm(tx.ID)
	g.Set("description", "Mercado 3")
	if rec := post(h.UpdateTransaction, "/transactions/update", g); rec.Code != http.StatusSeeOther {
		t.Fatalf("sem campo, arquivada em uso: %d %s", rec.Code, rec.Body.String())
	}
	if got := accountOf(); got != b.ID {
		t.Fatalf("preservar falhou: %d", got)
	}
}

func TestUpdateTransaction_MissingTransactionWithoutAccountField(t *testing.T) {
	h, _ := newTestHandler(t)
	if rec := post(h.UpdateTransaction, "/transactions/update", updateForm(999)); rec.Code != http.StatusNotFound {
		t.Errorf("status %d", rec.Code)
	}
}

func TestHistory_ShowsAccountBadgeOnlyWhenSet(t *testing.T) {
	h, db := newTestHandler(t)
	a, _ := db.CreateAccount("Carteira Azul", "")
	with := models.Transaction{Description: "Com conta", AmountCents: 1000, Type: "expense", Category: "Alimentação", Date: "2026-10-05", AccountID: a.ID}
	without := models.Transaction{Description: "Sem conta", AmountCents: 500, Type: "expense", Category: "Alimentação", Date: "2026-10-06"}
	_ = db.CreateTransaction(&with)
	_ = db.CreateTransaction(&without)

	body := getPage(h, "/?month=2026-10")
	if n := strings.Count(body, `data-account-badge`); n != 1 {
		t.Errorf("esperava 1 selo, achei %d", n)
	}
	if !strings.Contains(body, `data-account-badge>Carteira Azul<`) && !strings.Contains(body, "Carteira Azul</span>") {
		t.Error("selo sem o nome da conta")
	}

	// Conta arquivada em uso continua com selo.
	_ = db.SetAccountArchived(a.ID, true)
	body = getPage(h, "/?month=2026-10")
	if n := strings.Count(body, `data-account-badge`); n != 1 {
		t.Errorf("conta arquivada em uso deveria manter o selo, achei %d", n)
	}
}

func TestTransactionForms_HaveAccountField(t *testing.T) {
	h, db := newTestHandler(t)
	_, _ = db.CreateAccount("Carteira", "")
	arch, _ := db.CreateAccount("Velha", "")
	_ = db.SetAccountArchived(arch.ID, true)
	body := getPage(h, "/?month=2026-10")
	if n := strings.Count(body, `name="conta"`); n < 2 {
		t.Errorf("campo conta nos formulários de lançamento: %d", n)
	}
	if !strings.Contains(body, "Sem conta") {
		t.Error("falta a opção Sem conta")
	}
	if !strings.Contains(body, `value="`+idStr(1)+`">Carteira`) {
		t.Error("falta a conta ativa na lista")
	}
	if strings.Contains(body, `value="`+idStr(arch.ID)+`">Velha</option>`) && strings.Count(body, `>Velha</option>`) > 0 {
		// arquivadas só entram na edição, via JavaScript (data-account-name), nunca como opção fixa.
		t.Error("conta arquivada não deve ser opção fixa dos formulários")
	}
}
