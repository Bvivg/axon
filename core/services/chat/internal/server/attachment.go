package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"unicode"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/media"
)

const AttachmentUploadPath = "/internal/attachment"

const (
	maxFilenameLength = 255
	defaultFilename   = "file"
)

type UploadMode string

const (
	UploadAsMedia UploadMode = "media"
	UploadAsFile  UploadMode = "file"
	UploadAsVoice UploadMode = "voice"
)

type UploadRecorder interface {
	RecordUpload(ctx context.Context, uploaderID uuid.UUID, kind domain.MessageKind, payload json.RawMessage) (domain.Upload, error)
}

type AttachmentConfig struct {
	Verifier *authn.Verifier
	Pipeline *attachment.Pipeline
	Recorder UploadRecorder
	Logger   *slog.Logger
}

type AttachmentHandler struct {
	verifier *authn.Verifier
	pipeline *attachment.Pipeline
	recorder UploadRecorder
	log      *slog.Logger
}

func NewAttachmentHandler(cfg AttachmentConfig) (*AttachmentHandler, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("server: token verifier is required")
	case cfg.Pipeline == nil:
		return nil, errors.New("server: attachment pipeline is required")
	case cfg.Recorder == nil:
		return nil, errors.New("server: upload recorder is required")
	case cfg.Logger == nil:
		return nil, errors.New("server: logger is required")
	}

	return &AttachmentHandler{
		verifier: cfg.Verifier,
		pipeline: cfg.Pipeline,
		recorder: cfg.Recorder,
		log:      cfg.Logger,
	}, nil
}

type attachmentUploadResponse struct {
	UploadID string          `json:"upload_id"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
}

func (h *AttachmentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	mode, limit, ok := uploadMode(r.URL.Query().Get("as"))
	if !ok {
		http.Error(w, "as must be media, file or voice", http.StatusBadRequest)
		return
	}

	tmp, size, err := spool(w, r, limit)
	if err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			http.Error(w, "the file is too large", http.StatusRequestEntityTooLarge)
		case errors.Is(err, errEmptyUpload):
			http.Error(w, "the upload is empty", http.StatusBadRequest)
		default:
			h.log.ErrorContext(r.Context(), "could not spool a chat upload", "error", err)
			http.Error(w, "could not read the upload", http.StatusBadRequest)
		}
		return
	}
	defer func() { _ = os.Remove(tmp) }()

	var result attachment.Result
	switch mode {
	case UploadAsMedia:
		result, err = h.pipeline.Media(r.Context(), claims.UserID, tmp, size)
	case UploadAsVoice:
		result, err = h.pipeline.Voice(r.Context(), claims.UserID, tmp, size)
	default:
		result, err = h.pipeline.File(r.Context(), claims.UserID, tmp, sanitizeFilename(decodeFilename(r.Header.Get("X-Filename"))), size)
	}
	if err != nil {
		h.fail(r.Context(), w, mode, err)
		return
	}

	upload, err := h.recorder.RecordUpload(r.Context(), claims.UserID, result.Kind, result.Payload)
	if err != nil {
		h.log.ErrorContext(r.Context(), "could not record a chat upload", "error", err)
		http.Error(w, "could not record the upload", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(attachmentUploadResponse{
		UploadID: upload.ID.String(),
		Kind:     string(upload.Kind),
		Payload:  upload.Payload,
	})
}

func (h *AttachmentHandler) fail(ctx context.Context, w http.ResponseWriter, mode UploadMode, err error) {
	switch {
	case errors.Is(err, media.ErrTooLarge):
		http.Error(w, "the file is too large", http.StatusRequestEntityTooLarge)
	case errors.Is(err, media.ErrUnsupported):
		h.log.InfoContext(ctx, "refused an unsupported chat upload", "as", mode, "error", err)
		http.Error(w, "this file cannot be sent as "+string(mode), http.StatusUnsupportedMediaType)
	case errors.Is(err, context.DeadlineExceeded):
		h.log.WarnContext(ctx, "chat upload processing timed out", "as", mode)
		http.Error(w, "processing took too long", http.StatusGatewayTimeout)
	case errors.Is(err, context.Canceled):
		http.Error(w, "the upload was cancelled", http.StatusBadRequest)
	default:
		h.log.ErrorContext(ctx, "could not store a chat upload", "as", mode, "error", err)
		http.Error(w, "attachment storage is not available", http.StatusServiceUnavailable)
	}
}

func uploadMode(raw string) (UploadMode, int64, bool) {
	switch UploadMode(raw) {
	case UploadAsMedia:
		return UploadAsMedia, attachment.MaxUploadBytes, true
	case UploadAsVoice:
		return UploadAsVoice, attachment.MaxVoiceBytes, true
	case UploadAsFile, "":
		return UploadAsFile, attachment.MaxUploadBytes, true
	default:
		return "", 0, false
	}
}

var errEmptyUpload = errors.New("server: empty upload")

func spool(w http.ResponseWriter, r *http.Request, limit int64) (string, int64, error) {
	body := http.MaxBytesReader(w, r.Body, limit)

	f, err := os.CreateTemp("", "chat-upload-*")
	if err != nil {
		return "", 0, err
	}

	size, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil && size == 0 {
		copyErr = errEmptyUpload
	}
	if copyErr != nil {
		_ = os.Remove(f.Name())
		return "", 0, copyErr
	}

	return f.Name(), size, nil
}

func (h *AttachmentHandler) authenticate(headers http.Header) (authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, err
	}
	return h.verifier.Verify(raw)
}

func decodeFilename(raw string) string {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return raw
	}
	return decoded
}

func sanitizeFilename(raw string) string {
	name := path.Base(strings.ReplaceAll(raw, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)

	if name == "" || name == "." || name == "/" || name == ".." {
		return defaultFilename
	}
	if len(name) > maxFilenameLength {
		name = strings.ToValidUTF8(name[:maxFilenameLength], "")
	}
	return name
}
