package handlers

import (
	"fmt"
	"net/http"
	"time"
)

const (
	// Limite de abas/telas conectadas ao mesmo tempo (cada uma mantém uma conexão aberta).
	maxEventClients = 32

	// Intervalo entre "pings" para manter a conexão viva em proxies/redes e detectar clientes mortos.
	defaultHeartbeat = 25 * time.Second
)

// Events mantém uma conexão Server-Sent Events aberta e avisa o navegador sempre que
// um lançamento é criado, editado ou excluído (em qualquer tela).
func (h *Handler) Events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return
	}

	changes, unsubscribe, err := h.events.Subscribe()
	if err != nil {
		http.Error(w, "Atualização em tempo real indisponível", http.StatusServiceUnavailable)
		return
	}
	defer unsubscribe()

	// A conexão fica aberta por tempo indeterminado: remove o limite de escrita do servidor
	// (o WriteTimeout global de 10s a derrubaria).
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	hdr := w.Header()
	hdr.Set("Content-Type", "text/event-stream")
	hdr.Set("Cache-Control", "no-cache")
	hdr.Set("X-Accel-Buffering", "no") // evita buffer em proxies reversos

	send := func(msg string) bool {
		if _, err := fmt.Fprint(w, msg); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	if !send(": connected\n\n") {
		return
	}

	ticker := time.NewTicker(h.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.events.Done():
			return
		case <-changes:
			if !send("event: change\ndata: 1\n\n") {
				return
			}
		case <-ticker.C:
			if !send(": ping\n\n") {
				return
			}
		}
	}
}

// Shutdown encerra as conexões em tempo real abertas, permitindo o shutdown gracioso do servidor.
func (h *Handler) Shutdown() {
	h.events.Close()
}
