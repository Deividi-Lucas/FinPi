package handlers

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"finpi/internal/models"
)

func TestOverview_LinkWithArchivedBookCountsActiveSideAndHidesArchived(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := newPJ(t, db)
	mustTx(t, db, "Salário", "Salário", "income", 1000000, "2026-10-05")
	mustTx(t, db, "Pix para PJ", "Outros", "expense", 300000, "2026-10-07")
	mustTx(t, pj, "Pix do Pessoal", "Outros", "income", 300000, "2026-10-07")
	all, _, _ := db.AllTransactions(100)
	var outID, inID int64
	for _, tx := range all {
		switch tx.Description {
		case "Pix para PJ":
			outID = tx.ID
		case "Pix do Pessoal":
			inID = tx.ID
		}
	}
	if err := db.LinkTransfer(outID, inID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetBookArchived(pj.BookID(), true); err != nil {
		t.Fatal(err)
	}

	_, body := getOverview(h, "/visao-geral?month=2026-10")
	if !strings.Contains(body, "R$ 3.000,00") {
		t.Errorf("a despesa do lado ativo deveria contar inteira (R$ 3.000,00): %.400s", body)
	}
	if strings.Contains(body, "no período)") {
		t.Error("ligação com livro arquivado não deveria gerar a linha de transferências")
	}
	if strings.Contains(body, "R$ 13.000,00") || strings.Contains(body, ">PJ<") {
		t.Error("o livro arquivado não deveria entrar na tabela nem nos totais")
	}
}

func TestTransfers_MarkSoloAllSkippedIsConflict(t *testing.T) {
	h, db := newTestHandler(t)
	outID, inID := seedPair(t, h, db, 5000, "2026-10-05")
	if err := db.LinkTransfer(outID, inID); err != nil {
		t.Fatal(err)
	}
	rec := post(h.MarkSoloHandler, "/transferencias/fora", url.Values{"ids": {idStr(outID)}, "month": {"2026-10"}, "aba": {"manual"}})
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "já está ligado ou marcado") {
		t.Errorf("marcar lançamento já ligado: %d %.200s", rec.Code, rec.Body.String())
	}
}

func TestOverview_InternalMovementPairIsNotSuggested(t *testing.T) {
	h, db := newTestHandler(t)
	pj, _ := newPJ(t, db)
	if _, err := db.CreateCategory(models.InternalCategory); err != nil {
		t.Fatal(err)
	}
	if _, err := pj.CreateCategory(models.InternalCategory); err != nil {
		t.Fatal(err)
	}
	mustTx(t, db, "Aplicação", models.InternalCategory, "expense", 40000, "2026-10-05")
	mustTx(t, pj, "Resgate", models.InternalCategory, "income", 40000, "2026-10-05")

	_, body := getOverview(h, "/visao-geral?month=2026-10&revisao=1&aba=sugeridos")
	if strings.Contains(body, "para confirmar") {
		t.Error("movimentação interna não deveria gerar aviso de par pendente")
	}
	if strings.Contains(body, "/transferencias/ligar") {
		t.Error("movimentação interna não deveria ser sugerida como par")
	}
}
