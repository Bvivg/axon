package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/media"
)

func TestUploadRefusalsSayWhyWithTheirOwnStatus(t *testing.T) {
	attachments := &AttachmentHandler{log: logger.Discard()}
	sessions := &UploadSessionsHandler{log: logger.Discard()}

	tests := map[string]struct {
		fail   func(http.ResponseWriter, error)
		err    error
		status int
		quota  bool
	}{
		"an animation bomb": {
			fail:   func(w http.ResponseWriter, err error) { attachments.fail(t.Context(), w, UploadAsMedia, err) },
			err:    fmt.Errorf("%w: more than 1000 frames", media.ErrAnimationTooLarge),
			status: http.StatusBadRequest,
		},
		"a spent budget on a direct upload": {
			fail:   func(w http.ResponseWriter, err error) { attachments.fail(t.Context(), w, UploadAsFile, err) },
			err:    domain.ErrUploadQuota,
			status: http.StatusRequestEntityTooLarge,
			quota:  true,
		},
		"a spent budget on a resumable upload": {
			fail:   func(w http.ResponseWriter, err error) { sessions.fail(t.Context(), w, err) },
			err:    domain.ErrUploadQuota,
			status: http.StatusRequestEntityTooLarge,
			quota:  true,
		},
		"a file over the size limit": {
			fail:   func(w http.ResponseWriter, err error) { attachments.fail(t.Context(), w, UploadAsMedia, err) },
			err:    media.ErrTooLarge,
			status: http.StatusRequestEntityTooLarge,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			tt.fail(rec, tt.err)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			var body struct {
				Code string `json:"code"`
			}
			quota := json.Unmarshal(rec.Body.Bytes(), &body) == nil && body.Code == "resource_exhausted"
			if quota != tt.quota {
				t.Fatalf("body %q: quota marker = %v, want %v", rec.Body.String(), quota, tt.quota)
			}
		})
	}
}
