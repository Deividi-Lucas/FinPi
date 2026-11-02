package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"finpi/internal/transfer"
)

const maxSoloIDs = 100

func transferForm(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Método não permitido", http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Dados do formulário inválidos", http.StatusBadRequest)
		return false
	}
	return true
}

func formID(r *http.Request, key string) int64 {
	id, err := strconv.ParseInt(strings.TrimSpace(r.PostForm.Get(key)), 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// formIDs lê os ids repetidos (ids=1&ids=2…), sem repetir, até o máximo.
func formIDs(r *http.Request, key string, max int) ([]int64, bool) {
	var ids []int64
	seen := map[int64]bool{}
	for _, raw := range r.PostForm[key] {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 {
			return nil, false
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		if len(ids) > max {
			return nil, false
		}
	}
	return ids, true
}

// redirectOverview volta à visão geral no mesmo período, com a revisão aberta na aba pedida.
func redirectOverview(w http.ResponseWriter, r *http.Request) {
	period := parseOverviewPeriod(r.PostForm, timeNow())
	aba := r.PostForm.Get("aba")
	switch aba {
	case "sugeridos", "ligados", "fora", "manual":
	default:
		aba = "sugeridos"
	}
	http.Redirect(w, r, "/visao-geral?"+period.stateQuery()+"&revisao=1&aba="+aba, http.StatusSeeOther)
}

// transferError traduz o erro da regra para HTTP e re-renderiza a página com a mensagem no pop-up.
func (h *Handler) transferError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, transfer.ErrNeedOutIn), errors.Is(err, transfer.ErrSameBook), errors.Is(err, transfer.ErrAmount), errors.Is(err, transfer.ErrNothingSelected):
		status = http.StatusBadRequest
	case errors.Is(err, transfer.ErrTaken):
		status = http.StatusConflict
	case errors.Is(err, transfer.ErrNotFound):
		status = http.StatusNotFound
	}
	if status == http.StatusInternalServerError {
		serverError(w, "Erro ao salvar a transferência", err)
		return
	}
	h.renderOverview(w, r, status, err.Error())
}

// LinkTransferHandler liga uma saída a uma entrada.
func (h *Handler) LinkTransferHandler(w http.ResponseWriter, r *http.Request) {
	if !transferForm(w, r) {
		return
	}
	out, in := formID(r, "saida"), formID(r, "entrada")
	if out == 0 || in == 0 || out == in {
		h.transferError(w, r, transfer.ErrNeedOutIn)
		return
	}
	if err := h.db.LinkTransfer(out, in); err != nil {
		h.transferError(w, r, err)
		return
	}
	h.events.Publish()
	redirectOverview(w, r)
}

// UnlinkTransferHandler desfaz uma ligação.
func (h *Handler) UnlinkTransferHandler(w http.ResponseWriter, r *http.Request) {
	if !transferForm(w, r) {
		return
	}
	id := formID(r, "id")
	if id == 0 {
		h.transferError(w, r, transfer.ErrNotFound)
		return
	}
	if err := h.db.UnlinkTransfer(id); err != nil {
		h.transferError(w, r, err)
		return
	}
	h.events.Publish()
	redirectOverview(w, r)
}

// MarkSoloHandler marca lançamentos como "outro lado fora do FinPi".
func (h *Handler) MarkSoloHandler(w http.ResponseWriter, r *http.Request) {
	if !transferForm(w, r) {
		return
	}
	ids, ok := formIDs(r, "ids", maxSoloIDs)
	if !ok || len(ids) == 0 {
		h.transferError(w, r, transfer.ErrNothingSelected)
		return
	}
	marked, _, err := h.db.MarkSolo(ids)
	if err != nil {
		h.transferError(w, r, err)
		return
	}
	if marked == 0 { // todos já estavam ligados/marcados ou não existem
		h.transferError(w, r, transfer.ErrTaken)
		return
	}
	h.events.Publish()
	redirectOverview(w, r)
}

// UnmarkSoloHandler tira a marca "fora do FinPi".
func (h *Handler) UnmarkSoloHandler(w http.ResponseWriter, r *http.Request) {
	if !transferForm(w, r) {
		return
	}
	ids, ok := formIDs(r, "ids", maxSoloIDs)
	if !ok || len(ids) == 0 {
		h.transferError(w, r, transfer.ErrNothingSelected)
		return
	}
	n, err := h.db.UnmarkSolo(ids)
	if err != nil {
		h.transferError(w, r, err)
		return
	}
	if n > 0 {
		h.events.Publish()
	}
	redirectOverview(w, r)
}
