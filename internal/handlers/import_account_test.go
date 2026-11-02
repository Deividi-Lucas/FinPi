package handlers

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
)

// nuForm monta a confirmação de uma linha do Nubank (dados sintéticos) para o livro dado.
func nuForm(bookID string, extra url.Values) url.Values {
	f := url.Values{
		"n": {"1"}, "livro": {bookID}, "month": {"2025-11"},
		"sel_0": {"1"}, "date_0": {"2025-11-19"}, "desc_0": {"Pix recebido"}, "amount_0": {"8190"},
		"ref_0": {"nu:aaaaaaaa-0000-4000-8000-000000000001"}, "cat_0": {"Outros"},
	}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func accountNames(t *testing.T, db *database.DB) []string {
	t.Helper()
	list, err := db.ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range list {
		names = append(names, a.Name)
	}
	return names
}

func TestImportPreview_SuggestsExistingBankAccount(t *testing.T) {
	h, db := newTestHandler(t)
	nu, err := db.GetOrCreateBankAccount("nubank")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := db.CreateAccount("Reserva", "")
	body := previewFile(t, h, "extrato.csv", nubankCSVSample, "")
	if !strings.Contains(body, `name="conta"`) {
		t.Fatal("prévia sem o seletor de conta")
	}
	want := `<option value="` + itoa(nu.ID) + `" selected>Nubank</option>`
	if !strings.Contains(body, want) {
		t.Errorf("a conta do banco deveria vir selecionada: %s", want)
	}
	if !strings.Contains(body, `<option value="`+itoa(other.ID)+`">Reserva</option>`) || !strings.Contains(body, "Nova conta…") {
		t.Error("faltam as demais contas ou a opção Nova conta…")
	}
	if strings.Contains(body, "Nubank (será criada)") {
		t.Error("não deveria oferecer criar a conta do banco")
	}
}

func TestImportPreview_NoAccountYet_OffersToCreate(t *testing.T) {
	h, _ := newTestHandler(t)
	body := previewFile(t, h, "extrato.csv", nubankCSVSample, "")
	if !strings.Contains(body, `<option value="novo-banco" selected>Nubank (será criada)</option>`) {
		t.Error("prévia deveria oferecer 'Nubank (será criada)'")
	}
}

func TestImportConfirm_DefaultBankAccountIsCreated(t *testing.T) {
	h, db := newTestHandler(t)
	rec := post(h.ImportConfirm, "/import/confirm", nuForm("", url.Values{"conta": {"novo-banco"}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if names := accountNames(t, db); len(names) != 1 || names[0] != "Nubank" {
		t.Fatalf("contas = %v", names)
	}
	acc, ok, _ := db.AccountForBank("nubank")
	list, _ := db.GetTransactions("2025-11")
	if !ok || len(list) != 1 || list[0].AccountID != acc.ID {
		t.Errorf("a linha deveria estar na conta Nubank: %+v", list)
	}
}

func TestImportConfirm_ChoosesAnotherAccountOfTheBook(t *testing.T) {
	h, db := newTestHandler(t)
	other, _ := db.CreateAccount("Reserva", "")
	rec := post(h.ImportConfirm, "/import/confirm", nuForm("", url.Values{"conta": {itoa(other.ID)}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	list, _ := db.GetTransactions("2025-11")
	if len(list) != 1 || list[0].AccountID != other.ID {
		t.Errorf("lançamentos = %+v", list)
	}
	if names := accountNames(t, db); len(names) != 1 {
		t.Errorf("não deveria criar conta: %v", names)
	}
}

func TestImportConfirm_NewAccountWithName(t *testing.T) {
	h, db := newTestHandler(t)
	rec := post(h.ImportConfirm, "/import/confirm", nuForm("", url.Values{"conta": {"nova"}, "conta_nova": {"  Caixinha   Viagem "}}))
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if names := accountNames(t, db); len(names) != 1 || names[0] != "Caixinha Viagem" {
		t.Fatalf("contas = %v", names)
	}
	list, _ := db.GetTransactions("2025-11")
	accs, _ := db.ListAccounts()
	if len(list) != 1 || list[0].AccountID != accs[0].ID {
		t.Errorf("lançamentos = %+v", list)
	}
}

func TestImportConfirm_AccountErrors(t *testing.T) {
	h, db := newTestHandler(t)
	db.CreateAccount("Reserva", "")
	arch, _ := db.CreateAccount("Velha", "")
	db.SetAccountArchived(arch.ID, true)
	pj, _ := newPJ(t, db)
	foreign, _ := pj.CreateAccount("Da PJ", "")

	cases := []struct {
		name  string
		extra url.Values
		want  string
	}{
		{"nome vazio", url.Values{"conta": {"nova"}, "conta_nova": {"   "}}, "Informe o nome da conta."},
		{"nome duplicado", url.Values{"conta": {"nova"}, "conta_nova": {"reserva"}}, "Já existe uma conta com esse nome neste livro."},
		{"outro livro", url.Values{"conta": {itoa(foreign.ID)}}, "Conta não encontrada neste livro."},
		{"inexistente", url.Values{"conta": {"9999"}}, "Conta não encontrada neste livro."},
		{"lixo", url.Values{"conta": {"abc"}}, "Conta não encontrada neste livro."},
		{"arquivada", url.Values{"conta": {itoa(arch.ID)}}, "A conta está arquivada."},
	}
	for _, c := range cases {
		rec := post(h.ImportConfirm, "/import/confirm", nuForm("", c.extra))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s: status %d: %s", c.name, rec.Code, rec.Body.String())
		}
	}
	if list, _ := db.GetTransactions("2025-11"); len(list) != 0 {
		t.Errorf("nada deveria ter sido gravado: %+v", list)
	}
	if names := accountNames(t, db); len(names) != 2 {
		t.Errorf("nenhuma conta deveria ter sido criada: %v", names)
	}
}

func TestImportConfirm_NoAccountCreatedWhenRowIsInvalid(t *testing.T) {
	h, db := newTestHandler(t)
	rows := nuForm("", url.Values{"conta": {"nova"}, "conta_nova": {"Temporária"}, "cat_0": {"Inventada"}})
	rec := post(h.ImportConfirm, "/import/confirm", rows)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
	if names := accountNames(t, db); len(names) != 0 {
		t.Errorf("sobrou conta: %v", names)
	}
}

func TestImportConfirm_ReimportInAnotherAccountIsStillDuplicate(t *testing.T) {
	h, db := newTestHandler(t)
	post(h.ImportConfirm, "/import/confirm", nuForm("", url.Values{"conta": {"novo-banco"}}))
	other, _ := db.CreateAccount("Reserva", "")

	rec := post(h.ImportConfirm, "/import/confirm", nuForm("", url.Values{"conta": {itoa(other.ID)}}))
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "importados=0&ignorados=1") {
		t.Errorf("Location = %q", loc)
	}
	body := previewFile(t, h, "extrato.csv", nubankCSVSample, "")
	if !strings.Contains(body, "Já importado") {
		t.Error("a prévia deveria marcar 'Já importado'")
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
