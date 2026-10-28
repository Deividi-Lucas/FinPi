package statement

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"testing"
	"time"
)

// xc descreve uma célula de teste: kind "s" = texto compartilhado, "i" = texto inline, "n" = número.
type xc struct{ v, kind string }

func shared(v string) xc { return xc{v, "s"} }
func inline(v string) xc { return xc{v, "i"} }
func num(v string) xc    { return xc{v, "n"} }

// buildXLSX monta um XLSX mínimo e válido com uma planilha. Linhas nulas viram linhas em branco.
func buildXLSX(t *testing.T, rows [][]xc) []byte {
	t.Helper()
	var strs []string
	index := map[string]int{}
	var sheet strings.Builder
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	for r, row := range rows {
		if row == nil {
			continue
		}
		fmt.Fprintf(&sheet, `<row r="%d">`, r+1)
		for c, cell := range row {
			ref := fmt.Sprintf("%c%d", 'A'+c, r+1)
			switch cell.kind {
			case "s":
				i, ok := index[cell.v]
				if !ok {
					i = len(strs)
					strs = append(strs, cell.v)
					index[cell.v] = i
				}
				fmt.Fprintf(&sheet, `<c r="%s" t="s"><v>%d</v></c>`, ref, i)
			case "i":
				var esc bytes.Buffer
				xml.EscapeText(&esc, []byte(cell.v))
				fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, esc.String())
			default:
				fmt.Fprintf(&sheet, `<c r="%s"><v>%s</v></c>`, ref, cell.v)
			}
		}
		sheet.WriteString(`</row>`)
	}
	sheet.WriteString(`</sheetData></worksheet>`)

	var sst strings.Builder
	sst.WriteString(`<?xml version="1.0" encoding="UTF-8"?><sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">`)
	for _, s := range strs {
		var esc bytes.Buffer
		xml.EscapeText(&esc, []byte(s))
		fmt.Fprintf(&sst, `<si><t>%s</t></si>`, esc.String())
	}
	sst.WriteString(`</sst>`)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{
		"xl/worksheets/sheet1.xml": sheet.String(),
		"xl/sharedStrings.xml":     sst.String(),
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func xlsxHeaderRow() []xc {
	return []xc{shared("RELEASE_DATE"), shared("TRANSACTION_TYPE"), shared("REFERENCE_ID"), shared("TRANSACTION_NET_AMOUNT"), shared("PARTIAL_BALANCE")}
}

func TestParse_XLSXTextAndSharedStrings(t *testing.T) {
	data := buildXLSX(t, [][]xc{
		{shared("INITIAL_BALANCE"), shared("CREDITS"), shared("DEBITS"), shared("FINAL_BALANCE")},
		{shared("1.000,00"), shared("21,22"), shared("-20,00"), shared("1.001,22")},
		nil,
		xlsxHeaderRow(),
		{shared("03-08-2026"), shared("Rendimentos"), shared("1747712732569"), shared("1,22"), shared("1.001,22")},
		{inline("05-08-2026"), inline("Pix enviado Beltrana & Cia"), inline("172233555668"), inline("-20,00"), inline("0")},
	})
	st, err := Parse("extrato.xlsx", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Rows) != 2 {
		t.Fatalf("linhas = %d", len(st.Rows))
	}
	if st.Rows[0].Date != "2026-08-03" || st.Rows[0].AmountCents != 122 || st.Rows[0].RefID != "1747712732569" {
		t.Errorf("linha 0 = %+v", st.Rows[0])
	}
	if st.Rows[1].Description != "Pix enviado Beltrana & Cia" || st.Rows[1].AmountCents != -2000 {
		t.Errorf("linha 1 = %+v", st.Rows[1])
	}
	if !st.Summary.HasSummary || st.Summary.Credits != 2122 || st.Summary.Debits != -2000 {
		t.Errorf("resumo = %+v", st.Summary)
	}
}

func TestParse_XLSXNumericCells(t *testing.T) {
	serial := int(time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC).Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24)
	data := buildXLSX(t, [][]xc{
		xlsxHeaderRow(),
		{num(fmt.Sprint(serial)), shared("Rendimentos"), num("1747712732569"), num("1.22"), num("1001.22")},
		{num(fmt.Sprintf("%d.0", serial)), shared("Pix recebido X"), num("1.747712732570E12"), num("20.000000000000004"), num("0")},
		{num(fmt.Sprint(serial)), shared("Pix enviado Y"), num("1747712732571"), num("-1234.56"), num("0")},
	})
	st, err := Parse("extrato.xlsx", data)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		cents int64
		ref   string
	}{{122, "1747712732569"}, {2000, "1747712732570"}, {-123456, "1747712732571"}}
	for i, w := range want {
		r := st.Rows[i]
		if r.Date != "2026-08-03" || r.AmountCents != w.cents || r.RefID != w.ref {
			t.Errorf("linha %d = %+v, quer %d/%s", i, r, w.cents, w.ref)
		}
	}
}

func TestParse_XLSXScientificTextIDIsRejected(t *testing.T) {
	data := buildXLSX(t, [][]xc{
		xlsxHeaderRow(),
		{shared("03-08-2026"), shared("Rendimentos"), shared("1,74771E+12"), shared("1,22"), shared("0")},
	})
	if _, err := Parse("a.xlsx", data); err == nil || !strings.Contains(err.Error(), "notação científica") {
		t.Errorf("err = %v", err)
	}
}

func TestParse_XLSXInvalidZip(t *testing.T) {
	if _, err := Parse("a.xlsx", []byte("PK\x03\x04lixo")); err == nil {
		t.Error("zip corrompido deveria dar erro")
	}
}
