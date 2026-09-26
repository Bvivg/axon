package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
)

const AttachmentUploadPath = "/internal/attachment"

const maxFilenameLength = 255

type AttachmentConfig struct {
	Verifier *authn.Verifier
	Store    *attachment.Store
	URLs     attachment.URLBuilder
	Logger   *slog.Logger
}

type AttachmentHandler struct {
	verifier *authn.Verifier
	store    *attachment.Store
	urls     attachment.URLBuilder
	log      *slog.Logger
}

func NewAttachmentHandler(cfg AttachmentConfig) (*AttachmentHandler, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("server: token verifier is required")
	case cfg.Store == nil:
		return nil, errors.New("server: attachment store is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}

	return &AttachmentHandler{
		verifier: cfg.Verifier,
		store:    cfg.Store,
		urls:     cfg.URLs,
		log:      cfg.Logger,
	}, nil
}

type attachmentUploadResponse struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	Mime      string `json:"mime"`
	SizeBytes int64  `json:"size_bytes"`
}

func (h *AttachmentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		http.Error(w, "content type is required", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, attachment.MaxUploadBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "the file is too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "could not read the upload", http.StatusBadRequest)
		return
	}

	if len(data) == 0 {
		http.Error(w, "the upload is empty", http.StatusBadRequest)
		return
	}

	filename := sanitizeFilename(r.Header.Get("X-Filename"))
	key := attachment.NewObjectKey(claims.UserID, filename)

	if err := h.store.Put(r.Context(), key, contentType, data); err != nil {
		h.log.ErrorContext(r.Context(), "could not store a chat attachment", "error", err)
		http.Error(w, "attachment storage is not available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(attachmentUploadResponse{
		URL:       h.urls.URL(key),
		Filename:  filename,
		Mime:      contentType,
		SizeBytes: int64(len(data)),
	})
}

func (h *AttachmentHandler) authenticate(headers http.Header) (authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, err
	}
	return h.verifier.Verify(raw)
}

func sanitizeFilename(raw string) string {
	if len(raw) > maxFilenameLength {
		return raw[:maxFilenameLength]
	}
	return raw
}
