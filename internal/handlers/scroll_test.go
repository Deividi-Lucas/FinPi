package handlers

import (
	"os"
	"strings"
	"testing"
)

// Depois de salvar, a página recarrega; o script de rolagem devolve você ao ponto onde estava.
func TestPages_KeepScrollPositionAfterSaving(t *testing.T) {
	h, _ := newTestHandler(t)
	_, report := getReport(h, "/relatorio?ano=2026")
	pages := map[string]string{"/?month=2026-10": getDashboard(h, "/?month=2026-10"), "/relatorio": report}
	for target, body := range pages {
		for _, want := range []string{"finpiGo", "finpi:scroll"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s sem %q", target, want)
			}
		}
	}
}

// Redirecionar direto por resp.url pularia o salvamento da rolagem.
func TestTemplates_NoDirectRedirectAfterFetch(t *testing.T) {
	raw, err := os.ReadFile("../../templates/index.html")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "window.location.href = resp.url") {
		t.Error("use finpiGo(resp.url) para manter a posição da rolagem")
	}
}
