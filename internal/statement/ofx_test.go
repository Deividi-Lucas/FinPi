package statement

import (
	"errors"
	"strings"
	"testing"
)

func ofxFile(org string, txns ...string) string {
	return "OFXHEADER:100\r\nDATA:OFXSGML\r\nVERSION:102\r\nSECURITY:NONE\r\nENCODING:UTF-8\r\nCHARSET:NONE\r\n\r\n" +
		"<OFX>\r\n<SIGNONMSGSRSV1>\r\n<SONRS>\r\n<STATUS>\r\n<CODE>0</CODE>\r\n<SEVERITY>INFO</SEVERITY>\r\n</STATUS>\r\n" +
		"<DTSERVER>20261029191606[0:GMT]</DTSERVER>\r\n<LANGUAGE>POR</LANGUAGE>\r\n<FI>\r\n<ORG>" + org + "</ORG>\r\n<FID>260</FID>\r\n</FI>\r\n</SONRS>\r\n</SIGNONMSGSRSV1>\r\n" +
		"<BANKMSGSRSV1>\r\n<STMTTRNRS>\r\n<STMTRS>\r\n<BANKTRANLIST>\r\n" + strings.Join(txns, "") +
		"</BANKTRANLIST>\r\n</STMTRS>\r\n</STMTTRNRS>\r\n</BANKMSGSRSV1>\r\n</OFX>\r\n"
}

func ofxTxn(date, amt, id, memo string) string {
	return "<STMTTRN>\r\n<TRNTYPE>CREDIT</TRNTYPE>\r\n<DTPOSTED>" + date + "000000[-3:BRT]</DTPOSTED>\r\n<TRNAMT>" + amt +
		"</TRNAMT>\r\n<FITID>" + id + "</FITID>\r\n<MEMO>" + memo + "</MEMO>\r\n</STMTTRN>\r\n"
}

func TestParse_OFXNubank(t *testing.T) {
	data := ofxFile("NU PAGAMENTOS S.A.",
		ofxTxn("20251119", "81.90", "aaaaaaaa-0000-4000-8000-000000000001", "Transferência recebida pelo Pix - MARIA DA SILVA - •••.123.456-•• - BANCO EXEMPLO S.A. (0999) Agência: 1 Conta: 2-3"),
		ofxTxn("20251121", "-81.90", "aaaaaaaa-0000-4000-8000-000000000002", "Pagamento de boleto efetuado - DAS &amp; TAXAS"),
	)
	st, err := Parse("qualquer-nome.ofx", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if st.Bank != BankNubank || st.Format != "ofx" || len(st.Rows) != 2 {
		t.Fatalf("Bank=%s Format=%s linhas=%d", st.Bank, st.Format, len(st.Rows))
	}
	r := st.Rows[0]
	if r.Date != "2025-11-19" || r.AmountCents != 8190 || r.RefID != "aaaaaaaa-0000-4000-8000-000000000001" || r.Description != "Pix recebido MARIA DA SILVA" {
		t.Errorf("linha 1 = %+v", r)
	}
	if got := st.Rows[1]; got.AmountCents != -8190 || got.Description != "Pagamento de boleto DAS & TAXAS" || got.Original != "Pagamento de boleto efetuado - DAS & TAXAS" {
		t.Errorf("linha 2 = %+v", got)
	}
}

func TestParse_OFXSameIDAsNubankCSV(t *testing.T) {
	ofx, _ := Parse("a.ofx", []byte(ofxFile("NU PAGAMENTOS S.A.", ofxTxn("20251121", "-81.90", "aaaaaaaa-0000-4000-8000-000000000002", "Pagamento de boleto efetuado - DAS-SIMPLES NACIONAL"))))
	csv, _ := Parse("a.csv", []byte(nubankCSV))
	if ofx.Rows[0].RefID != csv.Rows[1].RefID {
		t.Errorf("o mesmo lançamento precisa ter o mesmo RefID no OFX (%s) e no CSV (%s)", ofx.Rows[0].RefID, csv.Rows[1].RefID)
	}
}

func TestParse_OFXLatin1(t *testing.T) {
	memo := "Pagamento de boleto efetuado - Jos\xe9 Comercial" // Latin-1: "José"
	data := strings.Replace(ofxFile("NU PAGAMENTOS S.A.", ofxTxn("20251121", "-10.00", "id-1", memo)), "CHARSET:NONE", "CHARSET:1252", 1)
	st, err := Parse("a.ofx", []byte(data))
	if err != nil || st.Rows[0].Description != "Pagamento de boleto José Comercial" {
		t.Fatalf("Parse = %+v, %v", st, err)
	}
}

func TestParse_OFXWithoutClosingTags(t *testing.T) {
	data := "OFXHEADER:100\nDATA:OFXSGML\n\n<OFX>\n<FI>\n<ORG>NU PAGAMENTOS S.A.\n</FI>\n<BANKTRANLIST>\n" +
		"<STMTTRN>\n<DTPOSTED>20251119000000\n<TRNAMT>10.00\n<FITID>id-1\n<MEMO>Pagamento de fatura\n" +
		"<STMTTRN>\n<DTPOSTED>20251120000000\n<TRNAMT>-5,50\n<FITID>id-2\n<NAME>Resgate RDB\n</BANKTRANLIST>\n</OFX>\n"
	st, err := Parse("a.ofx", []byte(data))
	if err != nil || len(st.Rows) != 2 {
		t.Fatalf("Parse = %+v, %v", st, err)
	}
	if st.Rows[1].AmountCents != -550 || st.Rows[1].Description != "Resgate RDB" {
		t.Errorf("linha 2 = %+v", st.Rows[1])
	}
}

func TestParse_OFXErrors(t *testing.T) {
	if _, err := Parse("a.ofx", []byte(ofxFile("BANCO DESCONHECIDO S.A.", ofxTxn("20251119", "1.00", "id", "x")))); !errors.Is(err, ErrUnknownBank) {
		t.Errorf("banco desconhecido: %v", err)
	}
	if _, err := Parse("a.ofx", []byte(ofxFile("NU PAGAMENTOS S.A."))); !errors.Is(err, ErrEmpty) {
		t.Errorf("sem lançamentos: %v", err)
	}
	cases := map[string]string{
		"data inválida":     ofxTxn("2025xx19", "1.00", "id", "x"),
		"valor inválido":    ofxTxn("20251119", "abc", "id", "x"),
		"sem identificador": ofxTxn("20251119", "1.00", "", "x"),
		"descrição vazia":   ofxTxn("20251119", "1.00", "id", ""),
	}
	for want, txn := range cases {
		_, err := Parse("a.ofx", []byte(ofxFile("NU PAGAMENTOS S.A.", txn)))
		var re *RowError
		if !errors.As(err, &re) || re.Line != 1 || re.Msg != want {
			t.Errorf("%s: erro = %v", want, err)
		}
	}
}
