package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"finpi/internal/models"
	"finpi/internal/statement"
)

const scriptName = "<script>alert(1)</script>"

func TestAccountName_IsEscapedEverywhere(t *testing.T) {
	h, db := newTestHandler(t)
	a, err := db.CreateAccount(scriptName, "")
	if err != nil {
		t.Fatal(err)
	}
	mustAccTx(t, db, "Mercado", "Alimentação", "expense", 1000, "2026-10-05", a.ID)

	body := getPage(h, "/?month=2026-10")
	if strings.Contains(body, scriptName) {
		t.Fatal("nome da conta sem escape no histórico")
	}
	badge := slice(body, "data-account-badge", "</span>")
	if !strings.Contains(badge, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("selo sem o nome escapado: %q", badge)
	}
	attr := slice(body, `data-account-name="`, `"`)
	if !strings.Contains(attr, "&lt;script&gt;") {
		t.Errorf("data-account-name sem escape: %q", attr)
	}
	menu := slice(body, "data-account-filter", "</details>")
	if !strings.Contains(menu, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("menu Conta sem o nome escapado: %q", menu)
	}

	// prévia da importação: a conta do banco com nome perigoso
	nu, err := db.GetOrCreateBankAccount(models.BankNubank)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RenameAccount(nu.ID, "<script>nu()</script>"); err != nil {
		t.Fatal(err)
	}
	body = previewFile(t, h, "extrato.csv", nubankCSVSample, "")
	if strings.Contains(body, "<script>nu()") || strings.Contains(body, scriptName) {
		t.Error("nome da conta sem escape na prévia da importação")
	}
	if !strings.Contains(body, "&lt;script&gt;nu()&lt;/script&gt;</option>") {
		t.Error("prévia deveria mostrar o nome escapado da conta do banco")
	}
}

func TestAccountActions_ArchivedBookIs400(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	a, err := pj.CreateAccount("Caixa", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBookArchived(pj.BookID(), true); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		handler http.HandlerFunc
		form    url.Values
	}{
		"/contas":          {h.CreateAccount, url.Values{"livro": {pjID}, "name": {"Nova"}}},
		"/contas/editar":   {h.UpdateAccount, url.Values{"livro": {pjID}, "id": {idStr(a.ID)}, "name": {"Outra"}}},
		"/contas/arquivar": {h.ArchiveAccount, url.Values{"livro": {pjID}, "id": {idStr(a.ID)}, "arquivar": {"1"}}},
	}
	for path, c := range cases {
		if rec := post(c.handler, path, c.form); rec.Code != http.StatusBadRequest {
			t.Errorf("%s em livro arquivado: status %d, quer 400", path, rec.Code)
		}
	}
	got, _ := pj.GetAccount(a.ID)
	if got.Name != "Caixa" || got.Archived {
		t.Errorf("a conta não deveria mudar: %+v", got)
	}
	if names := accountNames(t, pj); len(names) != 1 {
		t.Errorf("nenhuma conta deveria ser criada: %v", names)
	}
}

func TestBankKey_MapsEveryBank(t *testing.T) {
	cases := map[statement.Bank]string{
		statement.BankNubank:        models.BankNubank,
		statement.BankSicredi:       models.BankSicredi,
		statement.BankBancoDoBrasil: models.BankBB,
		statement.BankMercadoPago:   models.BankMercadoPago,
	}
	for in, want := range cases {
		if got := bankKey(in); got != want {
			t.Errorf("bankKey(%q) = %q, quer %q", in, got, want)
		}
	}
	if statement.BankBancoDoBrasil != "bancodobrasil" || models.BankBB != "bb" {
		t.Error("as chaves de banco mudaram: bancodobrasil (extrato) deveria virar bb (conta)")
	}
}

// Extrato sintético do Banco do Brasil em Windows-1252 (ç = 0xE7, º = 0xBA, í = 0xED).
const bbCSVSample = "\"Data\",\"Lan\xe7amento\",\"Detalhes\",\"N\xba documento\",\"Valor\",\"Tipo Lan\xe7amento\"\r\n" +
	"\"01/10/2021\",\"Transfer\xeancia recebida\",\"01/10 1234      98765-0 MARIA S SILVA\",\"111111111111111\",\"371,00\",\"Entrada\"\r\n"

const sicrediCSVSample = "Data,Descricao,Identificador,Tipo,Valor\n" +
	"21/12/2025,RECEBIMENTO PIX - 12345678901 MARIA DA SILVA,sic-id-1,CREDITO,100.50\n"

func TestImportPreview_BBAndSicrediSuggestTheirAccounts(t *testing.T) {
	for _, c := range []struct {
		name, file, content, bank, label string
	}{
		{"BB", "extrato.csv", bbCSVSample, models.BankBB, "Banco do Brasil"},
		{"Sicredi", "Sicredi_Extrato.csv", sicrediCSVSample, models.BankSicredi, "Sicredi"},
	} {
		h, db := newTestHandler(t)
		body := previewFile(t, h, c.file, c.content, "")
		if !strings.Contains(body, `<option value="novo-banco" selected>`+c.label+` (será criada)</option>`) {
			t.Errorf("%s: prévia deveria oferecer criar %q", c.name, c.label)
		}
		acc, err := db.GetOrCreateBankAccount(c.bank)
		if err != nil {
			t.Fatal(err)
		}
		body = previewFile(t, h, c.file, c.content, "")
		if !strings.Contains(body, `<option value="`+itoa(acc.ID)+`" selected>`+c.label+`</option>`) {
			t.Errorf("%s: a conta existente do banco deveria vir selecionada", c.name)
		}
		if strings.Contains(body, c.label+" (será criada)") {
			t.Errorf("%s: não deveria oferecer criar a conta que já existe", c.name)
		}
	}
}

// Conta de outro livro na URL: não é opção do menu, então não entra no "(N)" exibido; o resultado não muda.
func TestHistory_ForeignAccountDoesNotCountInMenu(t *testing.T) {
	h, db := newTestHandler(t)
	carteira, _ := seedAccounts(t, db)
	pj, _ := newPJ(t, db)
	foreign, err := pj.CreateAccount("Caixa PJ", "")
	if err != nil {
		t.Fatal(err)
	}

	body := getPage(h, "/?month=2026-10&conta="+idStr(foreign.ID))
	menu := slice(body, "data-account-filter", "</summary>")
	if strings.Contains(menu, "(1)") {
		t.Errorf("conta de outro livro não deveria contar no menu: %q", menu)
	}
	if got := historySummary(body); !strings.Contains(got, "0 lançamentos") {
		t.Errorf("o resultado do filtro não deveria mudar: %q", got)
	}

	body = getPage(h, "/?month=2026-10&conta="+idStr(foreign.ID)+"&conta="+idStr(carteira.ID)+"&conta=0")
	menu = slice(body, "data-account-filter", "</summary>")
	if !strings.Contains(menu, "(2)") {
		t.Errorf("menu deveria contar só as 2 opções marcadas: %q", menu)
	}
	if got := historySummary(body); !strings.Contains(got, "2 lançamentos") {
		t.Errorf("resumo: %q", got)
	}
}
