package statement

import (
	"testing"

	"finpi/internal/models"
)

func sampleStatement() Statement {
	return Statement{
		Rows: []Row{
			{Line: 5, Date: "2026-08-03", Description: "Rendimentos", AmountCents: 122, RefID: "1"},
			{Line: 6, Date: "2026-08-05", Description: "Pix recebido Fulano", AmountCents: 2000, RefID: "2"},
			{Line: 7, Date: "2026-08-05", Description: "Pix enviado Beltrana", AmountCents: -2000, RefID: "3"},
		},
		Summary: Summary{HasSummary: true, Credits: 2122, Debits: -2000},
	}
}

func TestBuildPreview_TypeFromSignAndDefaults(t *testing.T) {
	p := BuildPreview(sampleStatement(), PreviewInput{Categories: []string{"Alimentação", "Outros"}})
	if len(p.Rows) != 3 {
		t.Fatalf("linhas = %d", len(p.Rows))
	}
	r := p.Rows[2]
	if r.Type != "expense" || r.AmountCents != 2000 || r.SignedCents != -2000 {
		t.Errorf("despesa = %+v", r)
	}
	if p.Rows[1].Type != "income" {
		t.Errorf("receita = %+v", p.Rows[1])
	}
	for i, r := range p.Rows {
		if r.Status != StatusNew || !r.Selected || r.Locked || r.Category != "Outros" || r.Index != i {
			t.Errorf("linha %d = %+v", i, r)
		}
	}
	if p.FirstDate != "2026-08-03" || p.LastDate != "2026-08-05" {
		t.Errorf("período = %s a %s", p.FirstDate, p.LastDate)
	}
	if p.Credits != 2122 || p.Debits != 2000 {
		t.Errorf("créditos/débitos = %d/%d", p.Credits, p.Debits)
	}
	if want := ExternalID("1", "2026-08-03", 122); p.Rows[0].ExternalID != want {
		t.Errorf("external_id = %q, quer %q", p.Rows[0].ExternalID, want)
	}
}

func TestExternalID_Format(t *testing.T) {
	if got := ExternalID("171328401485", "2026-08-05", -2000); got != "mp:171328401485|2026-08-05|-2000" {
		t.Errorf("got %q", got)
	}
}

func TestBuildPreview_StatusPrecedence(t *testing.T) {
	st := Statement{Rows: []Row{
		{Date: "2026-08-03", Description: "Rendimentos", AmountCents: 122, RefID: "1"},            // já importado
		{Date: "2026-08-04", Description: "Compra no débito", AmountCents: -500, RefID: "2"},      // tipo novo
		{Date: "2026-08-05", Description: "Pix enviado Beltrana", AmountCents: -2000, RefID: "3"}, // possível duplicado
		{Date: "2026-08-06", Description: "Pix recebido Fulano", AmountCents: 700, RefID: "4"},    // novo
		{Date: "2026-08-03", Description: "Compra x", AmountCents: -122, RefID: "5"},              // tipo novo + duplicado manual: tipo novo vence
	}}
	in := PreviewInput{
		ExistingIDs: map[string]bool{ExternalID("1", "2026-08-03", 122): true},
		ManualKeys: map[string]bool{
			models.TransactionKey("2026-08-05", 2000, "expense"): true,
			models.TransactionKey("2026-08-03", 122, "expense"):  true,
		},
		Categories: []string{"Outros"},
	}
	p := BuildPreview(st, in)

	want := []struct {
		status   Status
		selected bool
		locked   bool
	}{
		{StatusImported, false, true},
		{StatusTypeNew, false, false},
		{StatusPossibleDup, false, false},
		{StatusNew, true, false},
		{StatusTypeNew, false, false},
	}
	for i, w := range want {
		r := p.Rows[i]
		if r.Status != w.status || r.Selected != w.selected || r.Locked != w.locked {
			t.Errorf("linha %d = %+v, quer %+v", i, r, w)
		}
	}
}

func TestBuildPreview_KnownTypesIgnoreCaseAndAccents(t *testing.T) {
	st := Statement{Rows: []Row{
		{Date: "2026-08-03", Description: "PIX RECEBIDO Fulano", AmountCents: 100, RefID: "1"},
		{Date: "2026-08-03", Description: "rendimentos", AmountCents: 100, RefID: "2"},
	}}
	p := BuildPreview(st, PreviewInput{Categories: []string{"Outros"}})
	for i, r := range p.Rows {
		if r.Status != StatusNew {
			t.Errorf("linha %d: status %s, quer novo", i, r.Status)
		}
	}
}

func TestBuildPreview_RepeatedKeyInFileGetsOccurrenceSuffix(t *testing.T) {
	st := Statement{Rows: []Row{
		{Date: "2026-08-03", Description: "Rendimentos", AmountCents: 122, RefID: "1"},
		{Date: "2026-08-03", Description: "Rendimentos", AmountCents: 122, RefID: "1"},
		{Date: "2026-08-03", Description: "Rendimentos", AmountCents: 122, RefID: "1"},
	}}
	p := BuildPreview(st, PreviewInput{Categories: []string{"Outros"}})
	base := ExternalID("1", "2026-08-03", 122)
	if p.Rows[0].ExternalID != base || p.Rows[1].ExternalID != base+"#2" || p.Rows[2].ExternalID != base+"#3" {
		t.Errorf("ids = %q %q %q", p.Rows[0].ExternalID, p.Rows[1].ExternalID, p.Rows[2].ExternalID)
	}
}

func TestBuildPreview_CategorySuggestion(t *testing.T) {
	st := Statement{Rows: []Row{
		{Date: "2026-08-03", Description: "Pix enviado Padaria Central", AmountCents: -500, RefID: "1"},
		{Date: "2026-08-03", Description: "Pix enviado Outra Pessoa", AmountCents: -500, RefID: "2"},
	}}
	in := PreviewInput{
		CategoryByDesc: map[string]string{"pix enviado padaria central": "Alimentação"},
		Categories:     []string{"Alimentação", "Outros"},
	}
	p := BuildPreview(st, in)
	if p.Rows[0].Category != "Alimentação" || p.Rows[1].Category != "Outros" {
		t.Errorf("categorias = %q / %q", p.Rows[0].Category, p.Rows[1].Category)
	}

	// Sem "Outros" na lista, cai na primeira.
	p = BuildPreview(st, PreviewInput{Categories: []string{"Lazer", "Saúde"}})
	if p.Rows[1].Category != "Lazer" {
		t.Errorf("padrão = %q, quer Lazer", p.Rows[1].Category)
	}
}

func TestBuildPreview_SummaryCheck(t *testing.T) {
	p := BuildPreview(sampleStatement(), PreviewInput{Categories: []string{"Outros"}})
	if !p.HasSummary || p.SummaryMismatch || p.FileCredits != 2122 || p.FileDebits != 2000 {
		t.Errorf("resumo ok = %+v", p)
	}

	st := sampleStatement()
	st.Summary.Credits = 9999
	if p := BuildPreview(st, PreviewInput{Categories: []string{"Outros"}}); !p.SummaryMismatch {
		t.Error("créditos divergentes deveriam marcar SummaryMismatch")
	}

	st = sampleStatement()
	st.Summary = Summary{}
	if p := BuildPreview(st, PreviewInput{Categories: []string{"Outros"}}); p.HasSummary || p.SummaryMismatch {
		t.Error("sem resumo não há conferência")
	}
}

func TestExternalIDFor_PerBank(t *testing.T) {
	if got := ExternalIDFor(BankNubank, "uuid-1", "2026-10-01", 100); got != "nu:uuid-1" {
		t.Errorf("nubank = %q", got)
	}
	if got := ExternalIDFor(BankSicredi, "E123", "2026-10-01", 100); got != "sic:E123" {
		t.Errorf("sicredi = %q", got)
	}
	if got := ExternalIDFor(BankMercadoPago, "99", "2026-10-01", -100); got != ExternalID("99", "2026-10-01", -100) {
		t.Errorf("mercado pago = %q", got)
	}
	for _, id := range []string{"mp:1|2026-10-01|1", "nu:abc", "sic:abc"} {
		if !ValidExternalID(id) {
			t.Errorf("%q deveria ser válido", id)
		}
	}
	for _, id := range []string{"", "xx:abc", "nu", "NU:abc"} {
		if ValidExternalID(id) {
			t.Errorf("%q não deveria ser válido", id)
		}
	}
}

func bankStatement(bank Bank, rows ...Row) Statement { return Statement{Bank: bank, Rows: rows} }

func TestBuildPreview_NubankKnownTypesAndInternalMovement(t *testing.T) {
	st := bankStatement(BankNubank,
		Row{Date: "2026-10-01", Description: "Pix recebido MARIA DA SILVA", Original: "orig 1", AmountCents: 1000, RefID: "u1"},
		Row{Date: "2026-10-02", Description: "Resgate RDB", Original: "Resgate RDB", AmountCents: 93365, RefID: "u2"},
		Row{Date: "2026-10-03", Description: "Aplicação RDB", AmountCents: -500, RefID: "u3"},
		Row{Date: "2026-10-04", Description: "Tarifa qualquer", AmountCents: -100, RefID: "u4"},
	)
	p := BuildPreview(st, PreviewInput{Bank: BankNubank, Categories: []string{"Outros", models.InternalCategory}})
	if p.Bank != BankNubank {
		t.Errorf("Preview.Bank = %s", p.Bank)
	}
	r := p.Rows
	if r[0].ExternalID != "nu:u1" || r[0].Status != StatusNew || !r[0].Selected || r[0].Original != "orig 1" || r[0].Category != "Outros" {
		t.Errorf("Pix recebido = %+v", r[0])
	}
	for _, i := range []int{1, 2} {
		if r[i].Category != models.InternalCategory || r[i].Status != StatusNew || !r[i].Selected {
			t.Errorf("linha %d deveria vir marcada na categoria %q: %+v", i, models.InternalCategory, r[i])
		}
	}
	if r[3].Status != StatusTypeNew || r[3].Selected {
		t.Errorf("tipo desconhecido deveria vir desmarcado: %+v", r[3])
	}
}

func TestBuildPreview_SicrediKnownTypes(t *testing.T) {
	st := bankStatement(BankSicredi,
		Row{Date: "2026-10-01", Description: "Débito automático SEGURO EXEMPLO", AmountCents: -58, RefID: "i1"},
		Row{Date: "2026-10-02", Description: "Integralização de capital", AmountCents: -5000, RefID: "i2"},
		Row{Date: "2026-10-03", Description: "Resgate RDB", AmountCents: 100, RefID: "i3"}, // não é tipo conhecido do Sicredi
		Row{Date: "2026-10-04", Description: "Compra no débito MERCADO EXEMPLO CENTRO", AmountCents: -3500, RefID: "i4"},
		Row{Date: "2026-10-05", Description: "Recarga de celular", AmountCents: -2000, RefID: "i5"},
		Row{Date: "2026-10-06", Description: "Transferência conta salário MARIA DA SILVA", AmountCents: 300000, RefID: "i6"},
		Row{Date: "2026-10-07", Description: "Devolução Pix MARIA DA SILVA", AmountCents: 1000, RefID: "i7"},
	)
	p := BuildPreview(st, PreviewInput{Bank: BankSicredi, Categories: []string{"Outros"}})
	if p.Rows[0].ExternalID != "sic:i1" || p.Rows[0].Status != StatusNew || p.Rows[1].Status != StatusNew {
		t.Errorf("tipos do Sicredi: %+v", p.Rows)
	}
	if p.Rows[2].Status != StatusTypeNew {
		t.Errorf("RDB não é tipo do Sicredi: %+v", p.Rows[2])
	}
	for _, i := range []int{3, 4, 5, 6} {
		if p.Rows[i].Status != StatusNew || !p.Rows[i].Selected {
			t.Errorf("linha %d (%s) deveria vir marcada: %+v", i, p.Rows[i].Description, p.Rows[i])
		}
	}
}

func TestBuildPreview_RepeatedBankIDGetsSuffix(t *testing.T) {
	st := bankStatement(BankSicredi,
		Row{Date: "2026-10-01", Description: "Pix recebido A", AmountCents: 100, RefID: "dup"},
		Row{Date: "2026-10-01", Description: "Pix recebido A", AmountCents: 100, RefID: "dup"},
	)
	p := BuildPreview(st, PreviewInput{Bank: BankSicredi, Categories: []string{"Outros"}})
	if p.Rows[0].ExternalID != "sic:dup" || p.Rows[1].ExternalID != "sic:dup#2" {
		t.Errorf("ids = %q, %q", p.Rows[0].ExternalID, p.Rows[1].ExternalID)
	}
}

func TestBuildPreview_SameNubankIDInOFXAndCSVIsAlreadyImported(t *testing.T) {
	row := Row{Date: "2026-10-01", Description: "Pagamento de fatura", AmountCents: -100, RefID: "u9"}
	p := BuildPreview(bankStatement(BankNubank, row), PreviewInput{Bank: BankNubank, ExistingIDs: map[string]bool{"nu:u9": true}, Categories: []string{"Outros"}})
	if p.Rows[0].Status != StatusImported || !p.Rows[0].Locked {
		t.Errorf("deveria vir como já importado: %+v", p.Rows[0])
	}
}
