package handlers

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// Dados sintéticos: nenhum nome, documento ou banco real.
const nubankCSVSample = "Data,Valor,Identificador,Descrição\n" +
	"19/11/2025,81.90,aaaaaaaa-0000-4000-8000-000000000001,Transferência recebida pelo Pix - MARIA DA SILVA - •••.123.456-•• - BANCO EXEMPLO S.A. (0999) Agência: 1 Conta: 2-3\n" +
	"09/12/2025,933.65,aaaaaaaa-0000-4000-8000-000000000003,Resgate RDB\n"

// previewFile envia o extrato para a prévia do livro dado e devolve o HTML (status 200 obrigatório).
func previewFile(t *testing.T, h *Handler, filename, content, bookID string) string {
	t.Helper()
	rec := postFile(h.ImportPreview, "/import/preview", filename, []byte(content), url.Values{"livro": {bookID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// attrValues apaga o valor dos atributos que guardam o texto original (não exibido na célula).
var attrValues = regexp.MustCompile(`(data-orig|title)="[^"]*"`)

func TestImportPreview_NubankInAnEmptyBook(t *testing.T) {
	h, db := newTestHandler(t)
	_, pjID := newPJ(t, db) // livro novo, sem categorias
	body := previewFile(t, h, "extrato.csv", nubankCSVSample, pjID)
	for _, want := range []string{"Nubank", "Pix recebido MARIA DA SILVA", "Movimentação interna", "Outros", "(será criada)", `data-orig="Transferência recebida pelo Pix - MARIA DA SILVA`} {
		if !strings.Contains(body, want) {
			t.Errorf("prévia sem %q", want)
		}
	}
	if strings.Contains(attrValues.ReplaceAllString(body, ""), "•••.123.456") {
		t.Error("a descrição exibida não pode trazer o CPF mascarado")
	}
}

func TestImportConfirm_CreatesCategoriesAndStoresOriginal(t *testing.T) {
	h, db := newTestHandler(t)
	pj, pjID := newPJ(t, db)
	form := url.Values{
		"n": {"2"}, "livro": {pjID}, "month": {"2025-12"},
		"sel_0": {"1"}, "date_0": {"2025-11-19"}, "desc_0": {"Pix recebido MARIA DA SILVA"}, "amount_0": {"8190"},
		"ref_0": {"nu:aaaaaaaa-0000-4000-8000-000000000001"}, "cat_0": {"Outros"}, "orig_0": {"Transferência recebida pelo Pix - MARIA DA SILVA - doc"},
		"sel_1": {"1"}, "date_1": {"2025-12-09"}, "desc_1": {"Resgate RDB"}, "amount_1": {"93365"},
		"ref_1": {"nu:aaaaaaaa-0000-4000-8000-000000000003"}, "cat_1": {"Movimentação interna"}, "orig_1": {"Resgate RDB"},
	}
	rec := post(h.ImportConfirm, "/import/confirm", form)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	for _, name := range []string{"Outros", "Movimentação interna"} {
		if ok, _ := pj.CategoryExists(name); !ok {
			t.Errorf("a categoria %q deveria ter sido criada no PJ", name)
		}
	}
	if ok, _ := db.CategoryExists("Movimentação interna"); ok {
		t.Error("a categoria não pode ser criada no Pessoal")
	}
	list, _ := pj.GetTransactions("2025-11")
	if len(list) != 1 || list[0].OriginalDescription != "Transferência recebida pelo Pix - MARIA DA SILVA - doc" {
		t.Errorf("lançamento importado = %+v", list)
	}
}

func TestImportConfirm_OtherMissingCategoryStillFails(t *testing.T) {
	h, db := newTestHandler(t)
	_, pjID := newPJ(t, db)
	rec := post(h.ImportConfirm, "/import/confirm", url.Values{
		"n": {"1"}, "livro": {pjID}, "sel_0": {"1"}, "date_0": {"2025-11-19"}, "desc_0": {"X"}, "amount_0": {"100"},
		"ref_0": {"nu:u1"}, "cat_0": {"Inventada"},
	})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "categoria inexistente") {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestImportConfirm_RejectsUnknownIDPrefix(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := post(h.ImportConfirm, "/import/confirm", url.Values{
		"n": {"1"}, "sel_0": {"1"}, "date_0": {"2025-11-19"}, "desc_0": {"X"}, "amount_0": {"100"}, "ref_0": {"zz:u1"}, "cat_0": {"Outros"},
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("prefixo desconhecido deveria dar 400, deu %d", rec.Code)
	}
}
