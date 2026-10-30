package statement

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const csvHeader = "INITIAL_BALANCE\tCREDITS\tDEBITS\tFINAL_BALANCE\t\n" +
	"1.000,00\t21,22\t-20,00\t1.001,22\t\n" +
	"\t\t\t\t\n" +
	"RELEASE_DATE\tTRANSACTION_TYPE\tREFERENCE_ID\tTRANSACTION_NET_AMOUNT\tPARTIAL_BALANCE\n"

const sampleCSV = csvHeader +
	"03/08/2026\tRendimentos \t1747712732569\t1,22\t1.001,22\n" +
	"05/08/2026\tPix recebido Fulano de Tal\t171328401485\t20,00\t1.021,22\n" +
	"05/08/2026\tPix enviado Beltrana Souza\t172233555668\t-20,00\t1.001,22\n"

func TestParse_CSVTab(t *testing.T) {
	st, err := Parse("extrato.csv", []byte(sampleCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Rows) != 3 {
		t.Fatalf("linhas = %d, quer 3", len(st.Rows))
	}
	r := st.Rows[0]
	if r.Date != "2026-08-03" || r.Description != "Rendimentos" || r.AmountCents != 122 || r.RefID != "1747712732569" {
		t.Errorf("linha 0 = %+v", r)
	}
	if st.Rows[2].AmountCents != -2000 || st.Rows[2].Description != "Pix enviado Beltrana Souza" {
		t.Errorf("linha 2 = %+v", st.Rows[2])
	}
	if !st.Summary.HasSummary || st.Summary.Credits != 2122 || st.Summary.Debits != -2000 {
		t.Errorf("resumo = %+v", st.Summary)
	}
}

func TestParse_CSVSemicolonAndQuotedComma(t *testing.T) {
	semi := strings.ReplaceAll(sampleCSV, "\t", ";")
	if st, err := Parse("a.csv", []byte(semi)); err != nil || len(st.Rows) != 3 || st.Rows[1].AmountCents != 2000 {
		t.Errorf("ponto e vírgula: %v %+v", err, st.Rows)
	}

	comma := "RELEASE_DATE,TRANSACTION_TYPE,REFERENCE_ID,TRANSACTION_NET_AMOUNT\n" +
		"03/08/2026,Rendimentos,1747712732569,\"1,22\"\n"
	if st, err := Parse("a.csv", []byte(comma)); err != nil || len(st.Rows) != 1 || st.Rows[0].AmountCents != 122 {
		t.Errorf("vírgula: %v %+v", err, st.Rows)
	}
}

func TestParse_CSVBOMAndWindows1252(t *testing.T) {
	withBOM := "\xef\xbb\xbf" + sampleCSV
	if _, err := Parse("a.csv", []byte(withBOM)); err != nil {
		t.Errorf("BOM: %v", err)
	}

	// "Transferência" com ê em Windows-1252 (byte 0xEA), inválido como UTF-8.
	latin := csvHeader + "03/08/2026\tTransfer\xeancia recebida\t1747712732569\t10,00\t0\n"
	st, err := Parse("a.csv", []byte(latin))
	if err != nil {
		t.Fatal(err)
	}
	if st.Rows[0].Description != "Transferência recebida" {
		t.Errorf("descrição = %q", st.Rows[0].Description)
	}
}

func TestParse_RejectsScientificID(t *testing.T) {
	bad := csvHeader + "03/08/2026\tRendimentos\t1,74771E+12\t1,22\t0\n"
	if _, err := Parse("a.csv", []byte(bad)); !errors.Is(err, ErrScientificID) {
		t.Errorf("err = %v, quer ErrScientificID", err)
	}
}

func TestParse_IgnoresBlankLinesAndPartialBalance(t *testing.T) {
	csv := csvHeader + "\t\t\t\t\n03/08/2026\tRendimentos\t1747712732569\t1,22\t\n"
	st, err := Parse("a.csv", []byte(csv))
	if err != nil || len(st.Rows) != 1 {
		t.Errorf("err=%v linhas=%d", err, len(st.Rows))
	}
}

func TestParse_DateFormats(t *testing.T) {
	for _, d := range []string{"03-08-2026", "03/08/2026", "2026-08-03"} {
		csv := csvHeader + d + "\tRendimentos\t1747712732569\t1,22\t0\n"
		st, err := Parse("a.csv", []byte(csv))
		if err != nil || st.Rows[0].Date != "2026-08-03" {
			t.Errorf("data %q: err=%v rows=%+v", d, err, st.Rows)
		}
	}
}

func TestParse_AmountFormats(t *testing.T) {
	good := map[string]int64{
		"1,22": 122, "1.234,56": 123456, "-20,00": -2000, "20": 2000,
		"-1.000,5": -100050, "R$ 5,00": 500, "1.234": 123400, "0,01": 1,
	}
	for raw, want := range good {
		csv := csvHeader + "03/08/2026\tX\t1\t" + raw + "\t0\n"
		st, err := Parse("a.csv", []byte(csv))
		if err != nil || st.Rows[0].AmountCents != want {
			t.Errorf("valor %q: err=%v rows=%+v, quer %d", raw, err, st.Rows, want)
		}
	}
	for _, raw := range []string{"abc", "1,234", "0,00", "", "1/2", "1,2,3", "99999999999999,00"} {
		csv := csvHeader + "03/08/2026\tX\t1\t" + raw + "\t0\n"
		_, err := Parse("a.csv", []byte(csv))
		var re *RowError
		if !errors.As(err, &re) {
			t.Errorf("valor %q deveria dar *RowError, deu %v", raw, err)
		}
	}
}

func TestParse_RowErrorHasLineNumber(t *testing.T) {
	bad := csvHeader + "99/99/2026\tX\t1\t1,00\t0\n"
	_, err := Parse("a.csv", []byte(bad))
	var re *RowError
	if !errors.As(err, &re) || re.Line != 5 {
		t.Errorf("err = %v (linha %v), quer RowError na linha 5", err, re)
	}
}

func TestParse_StructuralErrors(t *testing.T) {
	cases := map[string]struct {
		name string
		data string
		want error
	}{
		"sem cabeçalho":   {"a.csv", "foo;bar\n1;2\n", ErrUnrecognized},
		"coluna ausente":  {"a.csv", "RELEASE_DATE;TRANSACTION_TYPE\n03/08/2026;X\n", ErrUnrecognized},
		"sem lançamentos": {"a.csv", csvHeader, ErrEmpty},
		"extensão errada": {"a.pdf", sampleCSV, ErrUnsupportedFormat},
		"xlsx não é zip":  {"a.xlsx", sampleCSV, ErrUnsupportedFormat},
	}
	for name, c := range cases {
		if _, err := Parse(c.name, []byte(c.data)); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, quer %v", name, err, c.want)
		}
	}
}

func TestParse_Limits(t *testing.T) {
	if _, err := Parse("a.csv", make([]byte, MaxFileBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("arquivo grande: %v", err)
	}

	var b strings.Builder
	b.WriteString(csvHeader)
	for i := 0; i < MaxRows+1; i++ {
		fmt.Fprintf(&b, "03/08/2026\tX\t%d\t1,00\t0\n", i+1)
	}
	if _, err := Parse("a.csv", []byte(b.String())); !errors.Is(err, ErrTooManyRows) {
		t.Errorf("muitas linhas: %v", err)
	}
}

func TestParse_SummaryAbsentIsNotAnError(t *testing.T) {
	csv := "RELEASE_DATE;TRANSACTION_TYPE;REFERENCE_ID;TRANSACTION_NET_AMOUNT\n03/08/2026;X;1;1,00\n"
	st, err := Parse("a.csv", []byte(csv))
	if err != nil || st.Summary.HasSummary {
		t.Errorf("err=%v resumo=%+v", err, st.Summary)
	}
}

func TestParse_SpreadsheetMentioningOFXIsNotOFX(t *testing.T) {
	data := buildXLSX(t, [][]xc{
		sicrediHeader(),
		{num("46003"), inline("<OFX> teste"), num("1"), inline("id-1"), inline("CREDITO"), num("10"), num("10")},
	})
	st, err := Parse("a.xlsx", data)
	if err != nil || st.Format != "xlsx" || st.Bank != BankSicredi {
		t.Fatalf("Parse = %+v, %v", st, err)
	}
}
