package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
	"github.com/bvivg/axon/core/services/auth/internal/service"
)

type avatarURLsJSON struct {
	Small    string `json:"small"`
	Medium   string `json:"medium"`
	Large    string `json:"large"`
	Original string `json:"original"`
}

type avatarUploadResponse struct {
	AvatarURL  string         `json:"avatar_url"`
	AvatarURLs avatarURLsJSON `json:"avatar_urls"`
}

func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, avatar.MaxUploadBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "the image is too large", http.StatusRequestEntityTooLarge)
		return
	}

	user, err := h.svc.UploadAvatar(r.Context(), claims.UserID, raw)
	if err != nil {
		writeAvatarError(w, h.log, err)
		return
	}

	urls := h.avatarURLs.URLs(user.ID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(avatarUploadResponse{
		AvatarURL: urls.Medium,
		AvatarURLs: avatarURLsJSON{
			Small:    urls.Small,
			Medium:   urls.Medium,
			Large:    urls.Large,
			Original: urls.Original,
		},
	})
}

func writeAvatarError(w http.ResponseWriter, log *slog.Logger, err error) {
	switch {
	case errors.Is(err, avatar.ErrInvalidImage), errors.Is(err, avatar.ErrImageTooLarge):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, service.ErrAvatarStorageUnavailable):
		http.Error(w, "avatar storage is not available", http.StatusServiceUnavailable)
	default:
		log.Error("unhandled error uploading an avatar", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
