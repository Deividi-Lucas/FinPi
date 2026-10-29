package handlers

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// openStream abre /events em um servidor de teste e devolve o leitor do stream.
func openStream(t *testing.T, h *Handler) (*http.Response, *bufio.Reader) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(h.Events))
	resp, err := http.Get(srv.URL)
	if err != nil {
		srv.Close()
		t.Fatalf("GET /events: %v", err)
	}
	t.Cleanup(func() {
		resp.Body.Close()
		srv.Close()
	})
	return resp, bufio.NewReader(resp.Body)
}

// readLine lê uma linha do stream ou falha após 2s.
func readLine(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadString('\n')
		ch <- result{strings.TrimRight(line, "\r\n"), err}
	}()
	select {
	case res := <-ch:
		if res.err != nil && res.line == "" {
			t.Fatalf("stream encerrado: %v", res.err)
		}
		return res.line
	case <-time.After(2 * time.Second):
		t.Fatal("timeout lendo o stream")
		return ""
	}
}

// waitForLine lê linhas até encontrar want (ignora comentários e linhas em branco).
func waitForLine(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	for i := 0; i < 10; i++ {
		if readLine(t, r) == want {
			return
		}
	}
	t.Fatalf("linha %q não apareceu no stream", want)
}

func TestEvents_StreamsChangeAfterCreate(t *testing.T) {
	h, _ := newTestHandler(t)
	resp, r := openStream(t, h)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quer 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, quer text/event-stream", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Errorf("Cache-Control = %q, quer no-cache", cc)
	}
	waitForLine(t, r, ": connected")

	post(h.CreateTransaction, "/transactions", validForm())

	waitForLine(t, r, "event: change")
}

func TestEvents_SendsHeartbeat(t *testing.T) {
	h, _ := newTestHandler(t)
	h.heartbeat = 30 * time.Millisecond
	_, r := openStream(t, h)

	waitForLine(t, r, ": ping")
}

func TestEvents_EndsOnShutdown(t *testing.T) {
	h, _ := newTestHandler(t)
	_, r := openStream(t, h)
	waitForLine(t, r, ": connected")

	h.Shutdown()

	deadline := time.After(2 * time.Second)
	done := make(chan struct{})
	go func() {
		for {
			if _, err := r.ReadString('\n'); err != nil {
				close(done)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-deadline:
		t.Fatal("o stream deveria terminar após Shutdown")
	}
}

func TestEvents_RejectsNonGET(t *testing.T) {
	h, _ := newTestHandler(t)
	rec := httptest.NewRecorder()
	h.Events(rec, httptest.NewRequest(http.MethodPost, "/events", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, quer 405", rec.Code)
	}
}

func TestEvents_RejectsWhenFull(t *testing.T) {
	h, _ := newTestHandler(t)
	h.events = NewBroker(1)
	if _, _, err := h.events.Subscribe(); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	h.Events(rec, httptest.NewRequest(http.MethodGet, "/events", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, quer 503", rec.Code)
	}
}

func TestMutationsPublishChangeEvents(t *testing.T) {
	h, db := newTestHandler(t)
	ch, unsub, err := h.events.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	drain := func() { pending(ch) }

	// criar
	post(h.CreateTransaction, "/transactions", validForm())
	if !pending(ch) {
		t.Error("criar deveria publicar evento")
	}

	// editar
	tx := seed(t, db, 100)
	form := validForm()
	form.Set("id", strconv.FormatInt(tx.ID, 10))
	post(h.UpdateTransaction, "/transactions/update", form)
	if !pending(ch) {
		t.Error("editar deveria publicar evento")
	}

	// excluir
	post(h.DeleteTransaction, "/transactions/delete", url.Values{"id": {strconv.FormatInt(tx.ID, 10)}})
	if !pending(ch) {
		t.Error("excluir deveria publicar evento")
	}

	// falhas não publicam
	drain()
	bad := validForm()
	bad.Set("amount", "abc")
	post(h.CreateTransaction, "/transactions", bad)
	post(h.DeleteTransaction, "/transactions/delete", url.Values{"id": {"999"}})
	missing := validForm()
	missing.Set("id", "999")
	post(h.UpdateTransaction, "/transactions/update", missing)
	if pending(ch) {
		t.Error("operações que falham não devem publicar evento")
	}
}

func TestDashboard_ListensToEvents(t *testing.T) {
	h, _ := newTestHandler(t)

	rec := httptest.NewRecorder()
	h.Dashboard(rec, httptest.NewRequest(http.MethodGet, "/?month=2026-10", nil))

	if !strings.Contains(rec.Body.String(), "new EventSource('/events')") {
		t.Error("a página deve se inscrever em /events")
	}
}

func TestDashboard_UsesSharedLiveUpdateScript(t *testing.T) {
	h, _ := newTestHandler(t)
	body := getDashboard(h, "/")
	for _, want := range []string{"function startLiveUpdates(", "startLiveUpdates(function", `id="live-update-banner"`, "cdn.tailwindcss.com"} {
		if !strings.Contains(body, want) {
			t.Errorf("página principal sem %q", want)
		}
	}
	if strings.Count(body, "new EventSource('/events')") != 1 {
		t.Error("deveria haver exatamente uma conexão SSE na página")
	}
}
