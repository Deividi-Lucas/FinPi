package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/models"
)

const importCSV = "INITIAL_BALANCE\tCREDITS\tDEBITS\tFINAL_BALANCE\t\n" +
	"1.000,00\t21,22\t-20,00\t1.001,22\t\n" +
	"\t\t\t\t\n" +
	"RELEASE_DATE\tTRANSACTION_TYPE\tREFERENCE_ID\tTRANSACTION_NET_AMOUNT\tPARTIAL_BALANCE\n" +
	"03/08/2026\tRendimentos \t1747712732569\t1,22\t1.001,22\n" +
	"05/08/2026\tPix recebido Fulano de Tal\t171328401485\t20,00\t1.021,22\n" +
	"05/08/2026\tPix enviado Beltrana Souza\t172233555668\t-20,00\t1.001,22\n"

func postFile(handler http.HandlerFunc, path, filename string, data []byte, fields url.Values) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	fw.Write(data)
	for k, vs := range fields {
		for _, v := range vs {
			mw.WriteField(k, v)
		}
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec
}

type confirmRow struct {
	sel                  bool
	date, desc, ref, cat string
	cents                int64
}

func confirmForm(rows []confirmRow) url.Values {
	f := url.Values{"n": {strconv.Itoa(len(rows))}, "month": {"2026-08"}}
	for i, r := range rows {
		s := strconv.Itoa(i)
		if r.sel {
			f.Set("sel_"+s, "1")
		}
		f.Set("date_"+s, r.date)
		f.Set("desc_"+s, r.desc)
		f.Set("amount_"+s, strconv.FormatInt(r.cents, 10))
		f.Set("ref_"+s, r.ref)
		f.Set("cat_"+s, r.cat)
	}
	return f
}

func TestImportPreview_ShowsRowsAndWritesNothing(t *testing.T) {
	h, db := newTestHandler(t)

	rec := postFile(h.ImportPreview, "/import/preview", "extrato.csv", []byte(importCSV), nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Pix recebido Fulano de Tal", "Pix enviado Beltrana Souza", "Rendimentos", "Novo", "R$ 20,00", "extrato.csv"} {
		if !strings.Contains(body, want) {
			t.Errorf("revisão sem %q", want)
		}
	}
	if strings.Contains(body, "não bate") {
		t.Error("resumo confere, não deveria avisar divergência")
	}
	if list, _ := db.GetTransactions("2026-08"); len(list) != 0 {
		t.Errorf("a prévia não pode gravar nada: %+v", list)
	}
}

func TestImportPreview_WarnsWhenSummaryDoesNotMatch(t *testing.T) {
	h, _ := newTestHandler(t)
	bad := strings.Replace(importCSV, "21,22", "99,99", 1)
	rec := postFile(h.ImportPreview, "/import/preview", "extrato.csv", []byte(bad), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "não bate") {
		t.Errorf("status=%d, aviso de divergência ausente", rec.Code)
	}
}

func TestImportPreview_SuggestsCategoryFromHistoryAndMarksStatuses(t *testing.T) {
	h, db := newTestHandler(t)
	seedTx := func(desc string, cents int64, typ, cat, date string) {
		tx := models.Transaction{Description: desc, AmountCents: cents, Type: typ, Category: cat, Date: date}
		if err := db.CreateTransaction(&tx); err != nil {
			t.Fatal(err)
		}
	}
	seedTx("Pix recebido Fulano de Tal", 100, "income", "Lazer", "2026-07-01") // histórico → categoria
	seedTx("Qualquer coisa", 2000, "expense", "Outros", "2026-08-05")          // manual: mesma data/valor/tipo da linha 3

	body := postFile(h.ImportPreview, "/import/preview", "extrato.csv", []byte(importCSV), nil).Body.String()

	if !strings.Contains(body, `<option value="Lazer" selected>`) {
		t.Error("deveria sugerir a categoria Lazer pelo histórico")
	}
	if !strings.Contains(body, "Possível duplicado") {
		t.Error("deveria marcar possível duplicado")
	}
}

func TestImportPreview_Errors(t *testing.T) {
	h, _ := newTestHandler(t)

	if rec := postFile(h.ImportPreview, "/import/preview", "extrato.pdf", []byte("x"), nil); rec.Code != http.StatusBadRequest {
		t.Errorf("formato não suportado: %d", rec.Code)
	}
	if rec := postFile(h.ImportPreview, "/import/preview", "a.csv", []byte("foo;bar\n1;2\n"), nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "formato não reconhecido") {
		t.Errorf("formato não reconhecido: %d %s", rec.Code, rec.Body.String())
	}
	sci := strings.Replace(importCSV, "1747712732569", "1,74771E+12", 1)
	if rec := postFile(h.ImportPreview, "/import/preview", "a.csv", []byte(sci), nil); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "notação científica") {
		t.Errorf("notação científica: %d %s", rec.Code, rec.Body.String())
	}
	big := bytes.Repeat([]byte("a"), 6<<20)
	if rec := postFile(h.ImportPreview, "/import/preview", "a.csv", big, nil); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("arquivo grande: %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/import/preview", strings.NewReader("x=1"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ImportPreview(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("sem arquivo: %d", rec.Code)
	}

	get := httptest.NewRecorder()
	h.ImportPreview(get, httptest.NewRequest(http.MethodGet, "/import/preview", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", get.Code)
	}
}

func TestImportConfirm_SavesOnlySelectedAndRedirects(t *testing.T) {
	h, db := newTestHandler(t)
	ch, unsub, err := h.events.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()

	form := confirmForm([]confirmRow{
		{true, "2026-08-03", "Rendimentos", "mp:1747712732569|2026-08-03|122", "Outros", 122},
		{false, "2026-08-05", "Pix recebido Fulano", "mp:171328401485|2026-08-05|2000", "Outros", 2000},
		{true, "2026-08-05", "Pix enviado Beltrana", "mp:172233555668|2026-08-05|-2000", "saude", -2000},
	})
	form.Set("previstas", "1")
	rec := post(h.ImportConfirm, "/import/confirm", form)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/?month=2026-08&importados=2&ignorados=0&previstas=1" {
		t.Errorf("Location = %q", loc)
	}
	list, _ := db.GetTransactions("2026-08")
	if len(list) != 2 {
		t.Fatalf("lançamentos = %d, quer 2: %+v", len(list), list)
	}
	for _, tx := range list {
		switch tx.Description {
		case "Rendimentos":
			if tx.Type != "income" || tx.AmountCents != 122 || tx.Category != "Outros" {
				t.Errorf("rendimentos = %+v", tx)
			}
		case "Pix enviado Beltrana":
			if tx.Type != "expense" || tx.AmountCents != 2000 || tx.Category != "Saúde" {
				t.Errorf("pix enviado = %+v (categoria deveria virar o nome canônico)", tx)
			}
		default:
			t.Errorf("lançamento inesperado: %+v", tx)
		}
	}
	if !pending(ch) {
		t.Error("importar deveria publicar evento")
	}
}

func TestImportConfirm_ReimportIsIgnored(t *testing.T) {
	h, db := newTestHandler(t)
	form := confirmForm([]confirmRow{
		{true, "2026-08-03", "Rendimentos", "mp:1|2026-08-03|122", "Outros", 122},
	})
	post(h.ImportConfirm, "/import/confirm", form)

	ch, unsub, _ := h.events.Subscribe()
	defer unsub()
	rec := post(h.ImportConfirm, "/import/confirm", form)

	if loc := rec.Header().Get("Location"); loc != "/?month=2026-08&importados=0&ignorados=1" {
		t.Errorf("Location = %q", loc)
	}
	if list, _ := db.GetTransactions("2026-08"); len(list) != 1 {
		t.Errorf("lançamentos = %d, quer 1", len(list))
	}
	if pending(ch) {
		t.Error("nada foi importado: não deveria publicar evento")
	}
}

func TestImportConfirm_Validation(t *testing.T) {
	h, db := newTestHandler(t)
	good := confirmRow{true, "2026-08-03", "Rendimentos", "mp:1|2026-08-03|122", "Outros", 122}

	mutate := func(f func(*confirmRow)) confirmRow { r := good; f(&r); return r }
	cases := map[string][]confirmRow{
		"data inválida":         {mutate(func(r *confirmRow) { r.date = "03/08/2026" })},
		"descrição vazia":       {mutate(func(r *confirmRow) { r.desc = "  " })},
		"descrição longa":       {mutate(func(r *confirmRow) { r.desc = strings.Repeat("a", 201) })},
		"valor zero":            {mutate(func(r *confirmRow) { r.cents = 0 })},
		"valor gigante":         {mutate(func(r *confirmRow) { r.cents = maxAmountCents + 1 })},
		"ref sem prefixo":       {mutate(func(r *confirmRow) { r.ref = "xx:1" })},
		"ref longa":             {mutate(func(r *confirmRow) { r.ref = "mp:" + strings.Repeat("1", 200) })},
		"categoria inexistente": {mutate(func(r *confirmRow) { r.cat = "Inventada" })},
		"nenhuma selecionada":   {mutate(func(r *confirmRow) { r.sel = false })},
	}
	for name, rows := range cases {
		rec := post(h.ImportConfirm, "/import/confirm", confirmForm(rows))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, quer 400", name, rec.Code)
		}
	}

	// Uma linha inválida cancela todas (nada é gravado pela metade).
	rec := post(h.ImportConfirm, "/import/confirm", confirmForm([]confirmRow{good, {true, "2026-08-04", "X", "mp:2|2026-08-04|1", "Inventada", 1}}))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("lote com linha inválida: %d", rec.Code)
	}
	if list, _ := db.GetTransactions("2026-08"); len(list) != 0 {
		t.Errorf("nada deveria ter sido gravado: %+v", list)
	}

	for _, bad := range []string{"", "x", "-1", "5001"} {
		f := url.Values{"n": {bad}}
		if rec := post(h.ImportConfirm, "/import/confirm", f); rec.Code != http.StatusBadRequest {
			t.Errorf("n=%q: status = %d, quer 400", bad, rec.Code)
		}
	}

	get := httptest.NewRecorder()
	h.ImportConfirm(get, httptest.NewRequest(http.MethodGet, "/import/confirm", nil))
	if get.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", get.Code)
	}
}

func TestImportFlow_PreviewThenConfirmThenPreviewShowsImported(t *testing.T) {
	h, _ := newTestHandler(t)
	post(h.ImportConfirm, "/import/confirm", confirmForm([]confirmRow{
		{true, "2026-08-03", "Rendimentos", "mp:1747712732569|2026-08-03|122", "Outros", 122},
	}))

	body := postFile(h.ImportPreview, "/import/preview", "extrato.csv", []byte(importCSV), nil).Body.String()
	if !strings.Contains(body, "Já importado") {
		t.Error("a linha já gravada deveria aparecer como Já importado")
	}
}

func TestDashboard_ShowsImportResultBanner(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/?month=2026-08&importados=3&ignorados=1")
	if !strings.Contains(body, "3 lançamentos importados") || !strings.Contains(body, "1 já existia e foi ignorado") {
		t.Error("aviso de importação ausente")
	}
	if strings.Contains(getDashboard(h, "/?month=2026-08"), `id="import-result"`) {
		t.Error("sem parâmetros não deveria mostrar o aviso")
	}
	for _, q := range []string{"importados=abc", "importados=-1", "importados=999999999"} {
		if strings.Contains(getDashboard(h, "/?month=2026-08&"+q), `id="import-result"`) {
			t.Errorf("%s não deveria mostrar o aviso", q)
		}
	}
}

func TestTemplate_ImportControls(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/?month=2026-08")
	for _, want := range []string{"Importar extrato", `id="import-modal"`, `id="import-form"`, `action="/import/preview"`, `id="import-preview"`, `accept=".xlsx,.csv,.ofx"`} {
		if !strings.Contains(body, want) {
			t.Errorf("página sem %q", want)
		}
	}
}

func TestImportPreview_FragmentHasControls(t *testing.T) {
	h, _ := newTestHandler(t)
	body := postFile(h.ImportPreview, "/import/preview", "extrato.csv", []byte(importCSV), url.Values{"month": {"2026-08"}}).Body.String()
	for _, want := range []string{
		"data-import-row", "data-import-confirm", "data-import-discard", "data-import-count",
		`name="month" value="2026-08"`, `data-amount="-2000"`, `data-ref="mp:172233555668|2026-08-05|-2000"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("revisão sem %q", want)
		}
	}
	if strings.Contains(body, "<script") {
		t.Error("o trecho da revisão não deve conter scripts")
	}
}

// importPreviewBody envia o extrato sintético para a prévia, escolhendo o livro de destino.
func importPreviewBody(t *testing.T, h *Handler, bookID string) string {
	t.Helper()
	rec := postFile(h.ImportPreview, "/import/preview", "extrato.csv", []byte(importCSV), url.Values{"livro": {bookID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestImportPreview_ShowsTheDestinationBook(t *testing.T) {
	h, db := newTestHandler(t)
	_, pjID := newPJ(t, db)
	body := importPreviewBody(t, h, pjID)
	if !strings.Contains(body, "Importando para") || !strings.Contains(body, "PJ") {
		t.Errorf("a prévia deveria dizer o livro de destino: %.300s", body)
	}
}
