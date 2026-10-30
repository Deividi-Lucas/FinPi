package statement

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

const nubankCSV = "Data,Valor,Identificador,Descrição\n" +
	"19/11/2025,81.90,aaaaaaaa-0000-4000-8000-000000000001,Transferência recebida pelo Pix - MARIA DA SILVA - •••.123.456-•• - BANCO EXEMPLO S.A. (0999) Agência: 1 Conta: 2-3\n" +
	"21/11/2025,-81.90,aaaaaaaa-0000-4000-8000-000000000002,Pagamento de boleto efetuado - DAS-SIMPLES NACIONAL\n" +
	"09/12/2025,933.65,aaaaaaaa-0000-4000-8000-000000000003,Resgate RDB\n"

func TestParse_NubankCSV(t *testing.T) {
	st, err := Parse("extrato.csv", []byte(nubankCSV))
	if err != nil {
		t.Fatal(err)
	}
	if st.Bank != BankNubank || st.Format != "csv" || len(st.Rows) != 3 {
		t.Fatalf("Bank=%s Format=%s linhas=%d", st.Bank, st.Format, len(st.Rows))
	}
	r := st.Rows[0]
	if r.Date != "2025-11-19" || r.AmountCents != 8190 || r.RefID != "aaaaaaaa-0000-4000-8000-000000000001" {
		t.Errorf("linha 1 = %+v", r)
	}
	if r.Description != "Pix recebido MARIA DA SILVA" || r.Original == "" || r.Original == r.Description {
		t.Errorf("descrição/original = %q / %q", r.Description, r.Original)
	}
	if got := st.Rows[1]; got.AmountCents != -8190 || got.Description != "Pagamento de boleto DAS-SIMPLES NACIONAL" {
		t.Errorf("linha 2 = %+v", got)
	}
	if got := st.Rows[2]; got.AmountCents != 93365 || got.Description != "Resgate RDB" {
		t.Errorf("linha 3 = %+v", got)
	}
}

func TestParse_NubankCSVAcceptsHeaderWithoutAccentAndBOM(t *testing.T) {
	data := "\xef\xbb\xbfData,Valor,Identificador,Descricao\n19/11/2025,10.00,id-1,Pagamento de fatura\n"
	st, err := Parse("a.csv", []byte(data))
	if err != nil || st.Bank != BankNubank || len(st.Rows) != 1 {
		t.Fatalf("Parse = %+v, %v", st, err)
	}
}

func sicrediHeader() []xc {
	return []xc{inline("Data"), inline("Descricao"), inline("CodTransacao"), inline("Identificador"), inline("Tipo"), inline("Valor"), inline("Saldo")}
}

func TestParse_SicrediXLSXWithCSVExtension(t *testing.T) {
	data := buildXLSX(t, [][]xc{
		sicrediHeader(),
		{num("46012"), inline("DEBITO AUTOMATICO - 98765432000199 SEGURO EXEMPLO"), num("111"), inline("id-1"), inline("DEBITO"), num("-0.37"), num("0")},
		{num("46003"), inline("RECEBIMENTO PIX - 12345678901 MARIA DA SILVA"), num("222"), inline("id-2"), inline("CREDITO"), num("100.5"), num("100.5")},
	})
	st, err := Parse("Sicredi_Extrato.csv", data) // planilha com extensão .csv: vale o conteúdo
	if err != nil {
		t.Fatal(err)
	}
	if st.Bank != BankSicredi || st.Format != "xlsx" || len(st.Rows) != 2 {
		t.Fatalf("Bank=%s Format=%s linhas=%d", st.Bank, st.Format, len(st.Rows))
	}
	r := st.Rows[0]
	if r.Date != "2025-12-21" || r.AmountCents != -37 || r.RefID != "id-1" || r.Description != "Débito automático SEGURO EXEMPLO" {
		t.Errorf("linha 1 = %+v", r)
	}
	if got := st.Rows[1]; got.AmountCents != 10050 || got.Description != "Pix recebido MARIA DA SILVA" || got.Original != "RECEBIMENTO PIX - 12345678901 MARIA DA SILVA" {
		t.Errorf("linha 2 = %+v", got)
	}
}

func TestParse_BankGridErrors(t *testing.T) {
	cases := map[string]struct {
		csv  string
		want string
	}{
		"sem identificador": {"Data,Valor,Identificador,Descrição\n19/11/2025,10.00,,Pagamento de fatura\n", "sem identificador"},
		"data inválida":     {"Data,Valor,Identificador,Descrição\nxx,10.00,id-1,Pagamento de fatura\n", "data inválida"},
		"valor inválido":    {"Data,Valor,Identificador,Descrição\n19/11/2025,abc,id-1,Pagamento de fatura\n", "valor inválido"},
		"descrição vazia":   {"Data,Valor,Identificador,Descrição\n19/11/2025,10.00,id-1,\n", "descrição vazia"},
	}
	for name, c := range cases {
		_, err := Parse("a.csv", []byte(c.csv))
		var re *RowError
		if !errors.As(err, &re) || re.Line != 2 || re.Msg != c.want {
			t.Errorf("%s: erro = %v, quer RowError linha 2 %q", name, err, c.want)
		}
	}
	if _, err := Parse("a.csv", []byte("Data,Valor,Identificador,Descrição\n")); !errors.Is(err, ErrEmpty) {
		t.Errorf("sem linhas: %v, quer ErrEmpty", err)
	}
	if _, err := Parse("a.csv", []byte("foo,bar\n1,2\n")); !errors.Is(err, ErrUnrecognized) {
		t.Errorf("formato desconhecido: %v, quer ErrUnrecognized", err)
	}
}

func TestParse_MercadoPagoStillSetsBank(t *testing.T) {
	st, err := Parse("a.csv", []byte(csvHeader+"03/08/2026\tPix recebido Fulano\t123\t10,00\t0,00\n"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Bank != BankMercadoPago || st.Format != "csv" {
		t.Errorf("Bank=%s Format=%s", st.Bank, st.Format)
	}
}

func TestParse_OriginalCappedAt300Runes(t *testing.T) {
	long := "Pix recebido " + strings.Repeat("é", 400)
	st, err := Parse("a.csv", []byte("Data,Valor,Identificador,Descrição\n19/11/2025,10.00,id-1,"+long+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if n := utf8.RuneCountInString(st.Rows[0].Original); n != 300 || !utf8.ValidString(st.Rows[0].Original) {
		t.Errorf("Original com %d runes, quer 300 válidos", n)
	}
	if !strings.HasPrefix(st.Rows[0].Description, "Pix recebido") {
		t.Errorf("descrição = %q", st.Rows[0].Description)
	}
}

func TestParse_RefTooLong(t *testing.T) {
	_, err := Parse("a.csv", []byte("Data,Valor,Identificador,Descrição\n19/11/2025,10.00,"+strings.Repeat("x", 151)+",Pagamento de fatura\n"))
	var re *RowError
	if !errors.As(err, &re) || re.Line != 2 || re.Msg != "identificador longo demais" {
		t.Errorf("erro = %v", err)
	}
	if _, err := Parse("a.csv", []byte("Data,Valor,Identificador,Descrição\n19/11/2025,10.00,"+strings.Repeat("x", 150)+",Pagamento de fatura\n")); err != nil {
		t.Errorf("150 caracteres deveria passar: %v", err)
	}
}
