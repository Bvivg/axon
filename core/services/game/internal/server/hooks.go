package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

const (
	ClockHookPath  = "/internal/test/clock"
	TestUserHeader = "X-Axon-Test-User"
)

type clockAdjustment struct {
	SessionID   string   `json:"session_id"`
	RemainingMS [2]int64 `json:"remaining_ms"`
}

func (h *Handler) AdjustClock(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}
	var request clockAdjustment
	if json.Unmarshal(raw, &request) != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(request.SessionID)
	if err != nil || id == uuid.Nil || request.RemainingMS[0] < 0 || request.RemainingMS[1] < 0 {
		http.Error(w, "invalid clock", http.StatusBadRequest)
		return
	}
	if !h.owner.Serves(id) {
		if r.Header.Get(HopHeader) != "" {
			http.Error(w, "moved", http.StatusServiceUnavailable)
			return
		}
		h.forwardHook(w, r, id, raw)
		return
	}
	_, err = h.owner.AdjustClock(r.Context(), id, request.RemainingMS)
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, session.ErrNotFound):
		http.Error(w, "unknown game", http.StatusNotFound)
	case errors.Is(err, session.ErrNotPlaying):
		http.Error(w, "game is not running", http.StatusConflict)
	case errors.Is(err, owner.ErrNotOwner):
		http.Error(w, "moved", http.StatusServiceUnavailable)
	default:
		http.Error(w, "could not adjust the clock", http.StatusInternalServerError)
	}
}

func (h *Handler) forwardHook(w http.ResponseWriter, r *http.Request, id uuid.UUID, raw []byte) {
	routing, cancel := context.WithTimeout(r.Context(), routeTimeout)
	defer cancel()
	address, _, err := h.ownerAddress(routing, id)
	if err != nil || address == "" {
		http.Error(w, "no owner", http.StatusServiceUnavailable)
		return
	}
	forward, err := http.NewRequestWithContext(r.Context(), http.MethodPost, h.scheme+"://"+address+ClockHookPath, bytes.NewReader(raw))
	if err != nil {
		http.Error(w, "forward failed", http.StatusInternalServerError)
		return
	}
	forward.Header.Set("Content-Type", "application/json")
	forward.Header.Set(HopHeader, "1")
	res, err := h.peers.client.Do(forward)
	if err != nil {
		http.Error(w, "owner unreachable", http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = res.Body.Close() }()
	w.WriteHeader(res.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(res.Body, 1024))
}
