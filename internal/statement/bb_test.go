package statement

import (
	"errors"
	"strings"
	"testing"
)

// bbCSV monta um extrato sintético do Banco do Brasil em Windows-1252 (ç = 0xE7, º = 0xBA, í = 0xED).
func bbCSV(rows ...string) []byte {
	head := "\"Data\",\"Lan\xe7amento\",\"Detalhes\",\"N\xba documento\",\"Valor\",\"Tipo Lan\xe7amento\"\r\n"
	return []byte(head + strings.Join(rows, "\r\n") + "\r\n")
}

const bbSaida = "Sa\xedda"

var bbRows = []string{
	`"02/09/2021","Saldo Anterior","","","0,00",""`,
	"\"01/10/2021\",\"Transfer\xeancia recebida\",\"01/10 1234      98765-0 MARIA S SILVA\",\"111111111111111\",\"371,00\",\"Entrada\"",
	`"01/10/2021","Telefone Pre-Pago","11999990000-Operadora CO","222222222222222","-30,00","` + bbSaida + `"`,
	`"01/10/2021","TED","001 0001 12345678901 MARIA SILVA SANTOS","100101","-330,00","` + bbSaida + `"`,
	`"01/10/2021","TEDinternet","Cobr parc ref a             02/09/2021","333333333333333","-0,95","` + bbSaida + `"`,
	`"01/10/2021","Saldo do dia","","","0,00",""`,
	"\"04/10/2021\",\"Transfer\xeancia recebida\",\"03/10 1234      98765-0 MARIA S SILVA\",\"111111111111111\",\"15,40\",\"Entrada\"",
	`"04/10/2021","PIX - Enviado","03/10 20:46 Joao Da Silva Souza","100401","-15,00","` + bbSaida + `"`,
	`"31/10/2021","S A L D O","","","0,00",""`,
}

func TestParse_BancoDoBrasil(t *testing.T) {
	st, err := Parse("extrato.csv", bbCSV(bbRows...))
	if err != nil {
		t.Fatal(err)
	}
	if st.Bank != BankBancoDoBrasil || st.Format != "csv" || len(st.Rows) != 6 {
		t.Fatalf("Bank=%s Format=%s linhas=%d", st.Bank, st.Format, len(st.Rows))
	}
	if st.Bank.Label() != "Banco do Brasil" {
		t.Errorf("Label = %q", st.Bank.Label())
	}
	want := []struct {
		date  string
		cents int64
		desc  string
		ref   string
	}{
		{"2021-10-01", 37100, "Transferência recebida MARIA S SILVA", "111111111111111"},
		{"2021-10-01", -3000, "Telefone Pre-Pago Operadora CO", "222222222222222"},
		{"2021-10-01", -33000, "TED MARIA SILVA SANTOS", "100101"},
		{"2021-10-01", -95, "TEDinternet Cobr parc ref a", "333333333333333"},
		{"2021-10-04", 1540, "Transferência recebida MARIA S SILVA", "111111111111111"},
		{"2021-10-04", -1500, "Pix enviado Joao Da Silva Souza", "100401"},
	}
	for i, w := range want {
		r := st.Rows[i]
		if r.Date != w.date || r.AmountCents != w.cents || r.Description != w.desc || r.RefID != w.ref {
			t.Errorf("linha %d = %+v, quer %+v", i, r, w)
		}
	}
	if got := st.Rows[0].Original; got != "Transferência recebida - 01/10 1234 98765-0 MARIA S SILVA" {
		t.Errorf("Original = %q", got)
	}
	if st.Rows[0].Line != 3 {
		t.Errorf("Line = %d, quer 3", st.Rows[0].Line)
	}
}

func TestBuildPreview_BancoDoBrasilSameDocumentDistinctIDs(t *testing.T) {
	st, err := Parse("extrato.csv", bbCSV(bbRows...))
	if err != nil {
		t.Fatal(err)
	}
	p := BuildPreview(st, PreviewInput{Bank: BankBancoDoBrasil, Categories: []string{"Outros"}})
	ids := map[string]bool{}
	for _, r := range p.Rows {
		if ids[r.ExternalID] {
			t.Errorf("identificador repetido: %s", r.ExternalID)
		}
		ids[r.ExternalID] = true
	}
	if got := p.Rows[0].ExternalID; got != "bb:111111111111111|2021-10-01|37100" {
		t.Errorf("ExternalID = %q", got)
	}
	if p.Rows[0].Status != StatusNew || !p.Rows[0].Selected {
		t.Errorf("Transferência recebida deveria ser nova e marcada: %+v", p.Rows[0])
	}

	// A mesma linha (documento, data e valor) repetida ganha o sufixo #2.
	dup := Statement{Bank: BankBancoDoBrasil, Rows: []Row{st.Rows[0], st.Rows[0]}}
	pd := BuildPreview(dup, PreviewInput{Bank: BankBancoDoBrasil})
	if pd.Rows[1].ExternalID != pd.Rows[0].ExternalID+"#2" {
		t.Errorf("repetida = %q / %q", pd.Rows[0].ExternalID, pd.Rows[1].ExternalID)
	}
}

func TestBuildPreview_BancoDoBrasilTypes(t *testing.T) {
	st := bankStatement(BankBancoDoBrasil,
		Row{Date: "2021-10-01", Description: "Tarifa qualquer", AmountCents: -100, RefID: "1"},
		Row{Date: "2021-10-02", Description: "Resgate RDB", AmountCents: 5000, RefID: "2"},
		Row{Date: "2021-10-03", Description: "Telefone Pre-Pago Operadora", AmountCents: -3000, RefID: "3"},
		Row{Date: "2021-10-04", Description: "TEDinternet Cobr", AmountCents: -95, RefID: "4"},
	)
	p := BuildPreview(st, PreviewInput{Bank: BankBancoDoBrasil, Categories: []string{"Outros", "Movimentação interna"}})
	if p.Rows[0].Status != StatusTypeNew || p.Rows[0].Selected {
		t.Errorf("tarifa deveria ser tipo novo: %+v", p.Rows[0])
	}
	if p.Rows[1].Category == "Movimentação interna" || p.Rows[1].Status != StatusTypeNew {
		t.Errorf("Resgate RDB no BB não é movimentação interna: %+v", p.Rows[1])
	}
	for _, i := range []int{2, 3} {
		if p.Rows[i].Status != StatusNew {
			t.Errorf("linha %d deveria ser conhecida: %+v", i, p.Rows[i])
		}
	}
}

func TestValidExternalID_BB(t *testing.T) {
	if !ValidExternalID("bb:x") || ValidExternalID("bb:") {
		t.Error("prefixo bb: mal tratado")
	}
	if got := ExternalIDFor(BankBancoDoBrasil, "r", "2021-10-01", -5); got != "bb:r|2021-10-01|-5" {
		t.Errorf("ExternalIDFor = %q", got)
	}
}

func TestCleanBancoDoBrasil(t *testing.T) {
	cases := []struct{ label, details, want string }{
		{"Transferência recebida", "01/10 1234      98765-0 MARIA S SILVA", "Transferência recebida MARIA S SILVA"},
		{"PIX - Enviado", "03/10 20:46 Joao Da Silva Souza", "Pix enviado Joao Da Silva Souza"},
		{"PIX - Recebido", "03/10 20:46 Ana Lima", "Pix recebido Ana Lima"},
		{"TED", "001 0001 12345678901 MARIA SILVA SANTOS", "TED MARIA SILVA SANTOS"},
		{"TEDinternet", "Cobr parc ref a             02/09/2021", "TEDinternet Cobr parc ref a"},
		{"Telefone Pre-Pago", "11999990000-Operadora CO", "Telefone Pre-Pago Operadora CO"},
		{"TEDinternet", "Cobrança referente 01/10/2021", "TEDinternet Cobrança referente"},
		{"Tarifa  Pacote", "", "Tarifa Pacote"},
		{"Pix", "99 TAXI", "Pix 99 TAXI"},
	}
	for _, c := range cases {
		if got := cleanBancoDoBrasil(c.label, c.details); got != c.want {
			t.Errorf("cleanBancoDoBrasil(%q, %q) = %q, quer %q", c.label, c.details, got, c.want)
		}
	}
}

func TestParse_BancoDoBrasilEmptyDocumentHashIsStable(t *testing.T) {
	row := "\"05/10/2021\",\"Tarifa\",\"Pacote servi\xe7os\",\"\",\"-10,00\",\"" + bbSaida + "\""
	a, err := Parse("a.csv", bbCSV(row))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Parse("b.csv", bbCSV(row))
	ra, rb := a.Rows[0].RefID, b.Rows[0].RefID
	if ra != rb || len(ra) != 9 || ra[0] != 'h' {
		t.Errorf("RefID = %q / %q", ra, rb)
	}
	other, _ := Parse("c.csv", bbCSV("\"05/10/2021\",\"Tarifa\",\"Outra\",\"\",\"-10,00\",\""+bbSaida+"\""))
	if other.Rows[0].RefID == ra {
		t.Error("detalhes diferentes deveriam gerar hash diferente")
	}
}

func TestParse_BancoDoBrasilRowErrors(t *testing.T) {
	s := bbSaida
	cases := map[string]struct {
		row  string
		line int
		msg  string
	}{
		"data":      {`"xx/10/2021","TED","d","1","-1,00","` + s + `"`, 2, "data inválida"},
		"valor":     {`"01/10/2021","TED","d","1","abc","` + s + `"`, 2, "valor inválido"},
		"zero":      {`"01/10/2021","TED","d","1","0,00","` + s + `"`, 2, "valor inválido"},
		"descrição": {`"01/10/2021","","d","1","-1,00","` + s + `"`, 2, "descrição vazia"},
		"longo":     {`"01/10/2021","TED","d","` + strings.Repeat("9", 151) + `","-1,00","` + s + `"`, 2, "identificador longo demais"},
	}
	for name, c := range cases {
		_, err := Parse("a.csv", bbCSV(c.row))
		var re *RowError
		if !errors.As(err, &re) || re.Line != c.line || re.Msg != c.msg {
			t.Errorf("%s: erro = %v, quer linha %d %q", name, err, c.line, c.msg)
		}
	}
	if _, err := Parse("a.csv", bbCSV(bbRows[0], bbRows[5])); !errors.Is(err, ErrEmpty) {
		t.Errorf("só saldos: %v, quer ErrEmpty", err)
	}
}

func TestParse_BancoDoBrasilOriginalCapped(t *testing.T) {
	long := strings.Repeat("x", 400)
	st, err := Parse("a.csv", bbCSV(`"01/10/2021","TED","`+long+`","1","-1,00","`+bbSaida+`"`))
	if err != nil {
		t.Fatal(err)
	}
	if n := len([]rune(st.Rows[0].Original)); n != maxOriginalRunes {
		t.Errorf("Original tem %d runes", n)
	}
}

func TestParse_UnrecognizedMentionsBancoDoBrasil(t *testing.T) {
	if !strings.Contains(ErrUnrecognized.Error(), "Banco do Brasil") {
		t.Errorf("mensagem: %v", ErrUnrecognized)
	}
}

func TestParse_BancoDoBrasilSignComesFromValor(t *testing.T) {
	rows := []string{
		"\"01/10/2021\",\"TED\",\"d\",\"1\",\"-5,00\",\"Entrada\"",
		"\"01/10/2021\",\"TED\",\"d\",\"2\",\"7,00\",\"" + bbSaida + "\"",
	}
	st, err := Parse("a.csv", bbCSV(rows...))
	if err != nil {
		t.Fatal(err)
	}
	if st.Rows[0].AmountCents != -500 || st.Rows[1].AmountCents != 700 {
		t.Errorf("valores = %d / %d, quer -500 / 700", st.Rows[0].AmountCents, st.Rows[1].AmountCents)
	}
}
