package server

import (
	"context"
	"net/http"
	"path"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/shared/pkg/authn"
)

var objectKey = regexp.MustCompile(`^[0-9a-f-]{36}/[0-9a-f-]{36}(?:\.[a-z0-9]{1,8}){0,2}$`)

type MediaAccess interface {
	CanReadObject(context.Context, uuid.UUID, string) (bool, error)
}

type MediaFamily interface {
	ActiveFamily(context.Context, string, uuid.UUID) error
}

type MediaDownload struct {
	Verifier *authn.Verifier
	Access   MediaAccess
	Families MediaFamily
	Objects  *attachment.Store
}

func (h MediaDownload) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Vary", "Authorization, Cookie")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token, err := authn.BearerToken(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	claims, err := h.Verifier.Verify(token)
	if err != nil || claims.FamilyID == uuid.Nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.Families.ActiveFamily(r.Context(), token, claims.FamilyID) != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/internal/media/")
	if !objectKey.MatchString(key) {
		http.NotFound(w, r)
		return
	}
	allowed, err := h.Access.CanReadObject(r.Context(), claims.UserID, key)
	if err != nil {
		http.Error(w, "media unavailable", http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		http.NotFound(w, r)
		return
	}
	object, err := h.Objects.Open(r.Context(), key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = object.Close() }()
	info, err := object.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", info.ContentType)
	for _, name := range []string{"Content-Disposition", "Content-Encoding"} {
		if value := info.Metadata.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.Header().Set("ETag", `"`+info.ETag+`"`)
	http.ServeContent(w, r, path.Base(key), info.LastModified, object)
}
