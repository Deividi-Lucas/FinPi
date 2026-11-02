package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"finpi/internal/database"
)

// seedPair cria a saída no Pessoal e a entrada no PJ e devolve os ids.
func seedPair(t *testing.T, h *Handler, db *database.DB, cents int64, date string) (outID, inID int64) {
	t.Helper()
	pj, _ := newPJ(t, db)
	mustTx(t, db, "Pix para PJ", "Outros", "expense", cents, date)
	mustTx(t, pj, "Pix do Pessoal", "Outros", "income", cents, date)
	all, _, _ := db.AllTransactions(100)
	for _, tx := range all {
		switch tx.Description {
		case "Pix para PJ":
			outID = tx.ID
		case "Pix do Pessoal":
			inID = tx.ID
		}
	}
	return outID, inID
}

func idStr(n int64) string { return strconv.FormatInt(n, 10) }

// httptest_get faz um GET no handler e devolve o código da resposta.
func httptest_get(fn http.HandlerFunc) int {
	rec := httptest.NewRecorder()
	fn(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Code
}

func TestTransfers_ReviewShowsSuggestedPairs(t *testing.T) {
	h, db := newTestHandler(t)
	seedPair(t, h, db, 8190, "2026-10-05")
	_, body := getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=sugeridos")
	for _, want := range []string{"Pix para PJ", "Pix do Pessoal", "R$ 81,90", "/transferencias/ligar", "Descartar"} {
		if !strings.Contains(body, want) {
			t.Errorf("a revisão deveria conter %q", want)
		}
	}
}

func TestTransfers_LinkUnlinkFlow(t *testing.T) {
	h, db := newTestHandler(t)
	outID, inID := seedPair(t, h, db, 5000, "2026-10-05")

	rec := post(h.LinkTransferHandler, "/transferencias/ligar", url.Values{"saida": {idStr(outID)}, "entrada": {idStr(inID)}, "month": {"2026-10"}, "aba": {"sugeridos"}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "/visao-geral?month=2026-10") {
		t.Fatalf("ligar: %d %s", rec.Code, rec.Header().Get("Location"))
	}
	links, _ := db.ListTransferLinks()
	if len(links) != 1 || !links[0].Valid {
		t.Fatalf("ligações = %+v", links)
	}
	_, body := getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=ligados")
	if !strings.Contains(body, "/transferencias/desligar") || !strings.Contains(body, "Pix do Pessoal") {
		t.Error("a aba Ligados deveria listar o par com o botão Desfazer")
	}
	rec = post(h.UnlinkTransferHandler, "/transferencias/desligar", url.Values{"id": {idStr(links[0].ID)}, "month": {"2026-10"}, "aba": {"ligados"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("desligar: %d", rec.Code)
	}
	if links, _ := db.ListTransferLinks(); len(links) != 0 {
		t.Errorf("a ligação deveria ter sumido: %+v", links)
	}
}

func TestTransfers_LinkErrorsShowInThePage(t *testing.T) {
	h, db := newTestHandler(t)
	outID, _ := seedPair(t, h, db, 5000, "2026-10-05")
	cases := []struct {
		name string
		form url.Values
		code int
		msg  string
	}{
		{"sem escolha", url.Values{"month": {"2026-10"}}, http.StatusBadRequest, "Escolha uma saída e uma entrada."},
		{"ids inválidos", url.Values{"saida": {"x"}, "entrada": {"y"}, "month": {"2026-10"}}, http.StatusBadRequest, "Escolha uma saída e uma entrada."},
		{"inexistente", url.Values{"saida": {idStr(outID)}, "entrada": {"99999"}, "month": {"2026-10"}}, http.StatusNotFound, "Lançamento não encontrado."},
		{"mesma coisa", url.Values{"saida": {idStr(outID)}, "entrada": {idStr(outID)}, "month": {"2026-10"}}, http.StatusBadRequest, "Escolha uma saída e uma entrada."},
	}
	for _, c := range cases {
		rec := post(h.LinkTransferHandler, "/transferencias/ligar", c.form)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), c.msg) {
			t.Errorf("%s: %d, quer %d com %q: %.200s", c.name, rec.Code, c.code, c.msg, rec.Body.String())
		}
	}
	if rec := post(h.LinkTransferHandler, "/transferencias/ligar", url.Values{}); rec.Code == http.StatusSeeOther {
		t.Error("sem dados não pode redirecionar como sucesso")
	}
}

func TestTransfers_TakenAndAmountErrors(t *testing.T) {
	h, db := newTestHandler(t)
	outID, inID := seedPair(t, h, db, 5000, "2026-10-05")
	if rec := post(h.LinkTransferHandler, "/transferencias/ligar", url.Values{"saida": {idStr(outID)}, "entrada": {idStr(inID)}, "month": {"2026-10"}}); rec.Code != http.StatusSeeOther {
		t.Fatal(rec.Code)
	}
	rec := post(h.LinkTransferHandler, "/transferencias/ligar", url.Values{"saida": {idStr(outID)}, "entrada": {idStr(inID)}, "month": {"2026-10"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "já está ligado ou marcado") {
		t.Errorf("repetir: %d %.200s", rec.Code, rec.Body.String())
	}
}

func TestTransfers_SoloMarkAndUnmark(t *testing.T) {
	h, db := newTestHandler(t)
	outID, _ := seedPair(t, h, db, 5000, "2026-10-05")
	rec := post(h.MarkSoloHandler, "/transferencias/fora", url.Values{"ids": {idStr(outID)}, "month": {"2026-10"}, "aba": {"manual"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("marcar: %d", rec.Code)
	}
	if list, _ := db.ListSolo(); len(list) != 1 {
		t.Fatalf("ListSolo = %+v", list)
	}
	_, body := getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=fora")
	if !strings.Contains(body, "Pix para PJ") || !strings.Contains(body, "/transferencias/voltar") {
		t.Error("a aba Fora do FinPi deveria listar a marca com Voltar a contar")
	}
	if rec := post(h.UnmarkSoloHandler, "/transferencias/voltar", url.Values{"ids": {idStr(outID)}, "month": {"2026-10"}}); rec.Code != http.StatusSeeOther {
		t.Fatalf("voltar: %d", rec.Code)
	}
	if list, _ := db.ListSolo(); len(list) != 0 {
		t.Errorf("ListSolo = %+v", list)
	}
	if rec := post(h.MarkSoloHandler, "/transferencias/fora", url.Values{"month": {"2026-10"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("sem seleção deveria dar 400: %d", rec.Code)
	}
}

func TestTransfers_ManualTabFiltersFreeTransactions(t *testing.T) {
	h, db := newTestHandler(t)
	seedPair(t, h, db, 5000, "2026-10-05")
	mustTx(t, db, "Cinema", "Lazer", "expense", 4500, "2026-10-08")
	_, body := getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=manual")
	if !strings.Contains(body, "Cinema") || !strings.Contains(body, "Pix para PJ") {
		t.Error("a aba Ligar à mão deveria listar os lançamentos livres")
	}
	_, body = getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=manual&q=cinema")
	if !strings.Contains(body, "Cinema") || strings.Contains(body, "Pix para PJ") {
		t.Error("o filtro de texto deveria deixar só o Cinema")
	}
	_, body = getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=manual&valor=45,00")
	if !strings.Contains(body, "Cinema") || strings.Contains(body, "Pix para PJ") {
		t.Error("o filtro de valor deveria deixar só o Cinema")
	}
}

func TestTransfers_RoutesRejectGET(t *testing.T) {
	h, _ := newTestHandler(t)
	for name, fn := range map[string]http.HandlerFunc{
		"ligar": h.LinkTransferHandler, "desligar": h.UnlinkTransferHandler, "fora": h.MarkSoloHandler, "voltar": h.UnmarkSoloHandler,
	} {
		rec := httptest_get(fn)
		if rec != http.StatusMethodNotAllowed {
			t.Errorf("%s: GET deveria dar 405, deu %d", name, rec)
		}
	}
}

func TestTransfers_TabLinksKeepThePeriodUnescaped(t *testing.T) {
	h, _ := newTestHandler(t)
	_, body := getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=sugeridos")
	if strings.Contains(body, "month%3d") || !strings.Contains(body, "/visao-geral?month=2026-10&amp;revisao=1&amp;aba=ligados") {
		t.Error("os links das abas deveriam manter month=AAAA-MM sem escapar o sinal de igual")
	}
}
