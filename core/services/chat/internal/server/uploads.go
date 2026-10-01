package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/uploads"
)

const UploadSessionsPath = "/internal/uploads"

const (
	maxStartBody = 4 << 10
	statusWait   = 25 * time.Second
)

type UploadSessionsConfig struct {
	Verifier *authn.Verifier
	Manager  *uploads.Manager
	Logger   *slog.Logger
}

type UploadSessionsHandler struct {
	verifier *authn.Verifier
	manager  *uploads.Manager
	log      *slog.Logger
}

func NewUploadSessionsHandler(cfg UploadSessionsConfig) (*UploadSessionsHandler, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("server: token verifier is required")
	case cfg.Manager == nil:
		return nil, errors.New("server: upload manager is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}
	return &UploadSessionsHandler{verifier: cfg.Verifier, manager: cfg.Manager, log: cfg.Logger}, nil
}

func (h *UploadSessionsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST "+UploadSessionsPath, h.start)
	mux.HandleFunc("PUT "+UploadSessionsPath+"/{id}/parts/{n}", h.putPart)
	mux.HandleFunc("GET "+UploadSessionsPath+"/{id}", h.status)
	mux.HandleFunc("POST "+UploadSessionsPath+"/{id}/complete", h.complete)
	mux.HandleFunc("DELETE "+UploadSessionsPath+"/{id}", h.remove)
}

type startRequest struct {
	As   string `json:"as"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

type sessionResponse struct {
	ID        string                    `json:"id"`
	Status    string                    `json:"status"`
	PartSize  int64                     `json:"part_size"`
	PartCount int                       `json:"part_count"`
	Parts     []int                     `json:"parts"`
	Failure   string                    `json:"failure,omitempty"`
	Upload    *attachmentUploadResponse `json:"upload,omitempty"`
}

func (h *UploadSessionsHandler) start(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}

	var req startRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxStartBody)).Decode(&req); err != nil {
		http.Error(w, "the request is not valid JSON", http.StatusBadRequest)
		return
	}
	mode, _, ok := uploadMode(req.As)
	if !ok {
		http.Error(w, "as must be media, file or voice", http.StatusBadRequest)
		return
	}

	s, err := h.manager.Start(r.Context(), user, domain.UploadMode(mode), sanitizeFilename(req.Name), req.Size)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	h.write(w, http.StatusCreated, uploads.View{Session: s})
}

func (h *UploadSessionsHandler) putPart(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := sessionID(w, r)
	if !ok {
		return
	}
	number, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		http.Error(w, "the part number is not valid", http.StatusBadRequest)
		return
	}
	if r.ContentLength <= 0 {
		http.Error(w, "the part needs a Content-Length", http.StatusLengthRequired)
		return
	}

	body := http.MaxBytesReader(w, r.Body, r.ContentLength)
	if err := h.manager.PutPart(r.Context(), user, id, number, body, r.ContentLength); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *UploadSessionsHandler) status(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := sessionID(w, r)
	if !ok {
		return
	}

	var (
		view uploads.View
		err  error
	)
	if r.URL.Query().Get("wait") != "" {
		view, err = h.manager.Wait(r.Context(), user, id, statusWait)
	} else {
		view, err = h.manager.Status(r.Context(), user, id)
	}
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	h.write(w, http.StatusOK, view)
}

func (h *UploadSessionsHandler) complete(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := sessionID(w, r)
	if !ok {
		return
	}

	s, err := h.manager.Complete(r.Context(), user, id)
	if err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	h.write(w, http.StatusAccepted, uploads.View{Session: s})
}

func (h *UploadSessionsHandler) remove(w http.ResponseWriter, r *http.Request) {
	user, ok := h.user(w, r)
	if !ok {
		return
	}
	id, ok := sessionID(w, r)
	if !ok {
		return
	}

	if err := h.manager.Delete(r.Context(), user, id); err != nil {
		h.fail(r.Context(), w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *UploadSessionsHandler) user(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	token, err := authn.BearerToken(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return uuid.Nil, false
	}
	claims, err := h.verifier.Verify(token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return uuid.Nil, false
	}
	return claims.UserID, true
}

func sessionID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "no such upload", http.StatusNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h *UploadSessionsHandler) write(w http.ResponseWriter, status int, v uploads.View) {
	resp := sessionResponse{
		ID:        v.Session.ID.String(),
		Status:    string(v.Session.Status),
		PartSize:  v.Session.PartSize,
		PartCount: v.Session.PartCount,
		Parts:     v.Parts,
		Failure:   v.Session.Failure,
	}
	if resp.Parts == nil {
		resp.Parts = []int{}
	}
	if v.Upload != nil {
		resp.Upload = &attachmentUploadResponse{
			UploadID: v.Upload.ID.String(),
			Kind:     string(v.Upload.Kind),
			Payload:  v.Upload.Payload,
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *UploadSessionsHandler) fail(ctx context.Context, w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.Is(err, domain.ErrSessionNotFound):
		http.Error(w, "no such upload", http.StatusNotFound)
	case errors.Is(err, domain.ErrSessionState),
		errors.Is(err, domain.ErrUploadInUse),
		errors.Is(err, domain.ErrPartsMissing):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, uploads.ErrTooLarge), errors.As(err, &tooLarge):
		http.Error(w, "the file is too large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, uploads.ErrPartSize),
		errors.Is(err, uploads.ErrBadPart),
		errors.Is(err, uploads.ErrEmpty):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, context.Canceled), errors.Is(err, io.ErrUnexpectedEOF):
		http.Error(w, "the upload was interrupted", http.StatusBadRequest)
	default:
		h.log.ErrorContext(ctx, "chat upload session failed", "error", err)
		http.Error(w, "attachment storage is not available", http.StatusServiceUnavailable)
	}
}
