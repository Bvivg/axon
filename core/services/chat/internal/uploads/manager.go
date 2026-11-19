package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/media"
)

const (
	MaxParts = 10_000

	minPartSize = 5 << 20

	staleClaim      = 10 * time.Minute
	recoverInterval = time.Minute
	recoverBatch    = 50
	queueDepth      = 256
	waitPoll        = 400 * time.Millisecond
)

var (
	ErrTooLarge     = errors.New("uploads: the file is too large")
	ErrPartSize     = errors.New("uploads: the part has the wrong size")
	ErrBadPart      = errors.New("uploads: no such part number")
	ErrEmpty        = errors.New("uploads: the file is empty")
	ErrBadHash      = errors.New("uploads: invalid source hash")
	ErrHashMismatch = errors.New("uploads: the file does not match its source hash")
)

type Sessions interface {
	CreateSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, error)
	ReuseSession(ctx context.Context, s domain.UploadSession) (domain.UploadSession, bool, error)
	SessionByID(ctx context.Context, id uuid.UUID) (domain.UploadSession, error)
	MarkSessionProcessing(ctx context.Context, id uuid.UUID) error
	ClaimSession(ctx context.Context, id uuid.UUID, staleAfter time.Duration) (domain.UploadSession, bool, error)
	ClaimableSessions(ctx context.Context, staleAfter time.Duration, limit int) ([]uuid.UUID, error)
	FinishSession(ctx context.Context, id, uploadID uuid.UUID) error
	FailSession(ctx context.Context, id uuid.UUID, reason string) error
	DeleteSession(ctx context.Context, id uuid.UUID) error

	DeleteDraftUpload(ctx context.Context, uploadID uuid.UUID) error
	CreateUpload(ctx context.Context, u domain.Upload) (domain.Upload, error)
	UploadByID(ctx context.Context, id uuid.UUID) (domain.Upload, error)

	FileBySource(ctx context.Context, mode domain.UploadMode, source []byte) (domain.StoredFile, bool, error)
	SaveFile(ctx context.Context, file domain.StoredFile, mode domain.UploadMode, source []byte, sourceSize int64) (domain.StoredFile, bool, error)
}

type Objects interface {
	StartMultipart(ctx context.Context, key string) (string, error)
	PutPart(ctx context.Context, key, uploadID string, number int, data io.Reader, size int64) error
	ListParts(ctx context.Context, key, uploadID string) ([]attachment.Part, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []attachment.Part) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
	Download(ctx context.Context, key, path string) error
	Remove(ctx context.Context, keys []string) map[string]error
}

type Processor interface {
	Media(ctx context.Context, userID uuid.UUID, path string, size int64) (attachment.Result, error)
	Voice(ctx context.Context, userID uuid.UUID, path string, size int64) (attachment.Result, error)
	File(ctx context.Context, userID uuid.UUID, path, filename string, size int64) (attachment.Result, error)
}

type Config struct {
	Sessions  Sessions
	Objects   Objects
	Processor Processor
	KeysOf    func(payload json.RawMessage) []string
	Logger    *slog.Logger

	MaxBytes      int64
	MaxVoiceBytes int64
	PartSize      int64
	Workers       int
	TempDir       string
}

type Manager struct {
	sessions  Sessions
	objects   Objects
	processor Processor
	keysOf    func(payload json.RawMessage) []string
	log       *slog.Logger

	maxBytes      int64
	maxVoiceBytes int64
	partSize      int64
	workers       int
	tempDir       string

	queue chan uuid.UUID
	newID func() uuid.UUID
}

func New(cfg Config) (*Manager, error) {
	switch {
	case cfg.Sessions == nil:
		return nil, errors.New("uploads: a session store is required")
	case cfg.Objects == nil:
		return nil, errors.New("uploads: an object store is required")
	case cfg.Processor == nil:
		return nil, errors.New("uploads: a media processor is required")
	case cfg.KeysOf == nil:
		return nil, errors.New("uploads: an object key resolver is required")
	case cfg.Logger == nil:
		return nil, errors.New("uploads: logger is required")
	case cfg.MaxBytes <= 0 || cfg.MaxVoiceBytes <= 0:
		return nil, errors.New("uploads: size limits must be positive")
	case cfg.PartSize < minPartSize:
		return nil, fmt.Errorf("uploads: parts must be at least %d bytes", minPartSize)
	}

	return &Manager{
		sessions:      cfg.Sessions,
		objects:       cfg.Objects,
		processor:     cfg.Processor,
		keysOf:        cfg.KeysOf,
		log:           cfg.Logger,
		maxBytes:      cfg.MaxBytes,
		maxVoiceBytes: cfg.MaxVoiceBytes,
		partSize:      cfg.PartSize,
		workers:       max(cfg.Workers, 1),
		tempDir:       cfg.TempDir,
		queue:         make(chan uuid.UUID, queueDepth),
		newID:         uuid.New,
	}, nil
}

func (m *Manager) limitFor(mode domain.UploadMode) int64 {
	if mode == domain.UploadModeVoice {
		return m.maxVoiceBytes
	}
	return m.maxBytes
}

func (m *Manager) Start(
	ctx context.Context,
	uploader uuid.UUID,
	mode domain.UploadMode,
	filename string,
	size int64,
	source []byte,
) (domain.UploadSession, error) {
	switch {
	case size <= 0:
		return domain.UploadSession{}, ErrEmpty
	case size > m.limitFor(mode):
		return domain.UploadSession{}, ErrTooLarge
	case len(source) != 0 && len(source) != sha256.Size:
		return domain.UploadSession{}, ErrBadHash
	}

	parts := int((size + m.partSize - 1) / m.partSize)
	if parts > MaxParts {
		return domain.UploadSession{}, ErrTooLarge
	}

	id := m.newID()
	session := domain.UploadSession{
		ID: id, UploaderID: uploader, Mode: mode, Filename: filename, SizeBytes: size,
		PartSize: m.partSize, PartCount: parts, ExpectedSHA256: bytes.Clone(source),
	}
	if len(source) > 0 {
		ready, found, err := m.sessions.ReuseSession(ctx, session)
		if err != nil || found {
			return ready, err
		}
	}
	key := "staging/" + id.String()
	multipart, err := m.objects.StartMultipart(ctx, key)
	if err != nil {
		return domain.UploadSession{}, err
	}

	session.StagingKey, session.MultipartID = key, multipart
	stored, err := m.sessions.CreateSession(ctx, session)
	if err != nil {
		_ = m.objects.AbortMultipart(context.WithoutCancel(ctx), key, multipart)
	}
	return stored, err
}

func (m *Manager) owned(ctx context.Context, uploader, id uuid.UUID) (domain.UploadSession, error) {
	s, err := m.sessions.SessionByID(ctx, id)
	if err != nil {
		return domain.UploadSession{}, err
	}
	if s.UploaderID != uploader {
		return domain.UploadSession{}, domain.ErrSessionNotFound
	}
	return s, nil
}

func (m *Manager) PutPart(ctx context.Context, uploader, id uuid.UUID, number int, body io.Reader, size int64) error {
	s, err := m.owned(ctx, uploader, id)
	if err != nil {
		return err
	}
	switch {
	case s.Status != domain.UploadUploading:
		return domain.ErrSessionState
	case number < 1 || number > s.PartCount:
		return ErrBadPart
	case size != s.PartBytes(number):
		return ErrPartSize
	}
	return m.objects.PutPart(ctx, s.StagingKey, s.MultipartID, number, io.LimitReader(body, size), size)
}

type View struct {
	Session domain.UploadSession
	Parts   []int
	Upload  *domain.Upload
}

func (m *Manager) Status(ctx context.Context, uploader, id uuid.UUID) (View, error) {
	s, err := m.owned(ctx, uploader, id)
	if err != nil {
		return View{}, err
	}
	return m.view(ctx, s)
}

func (m *Manager) view(ctx context.Context, s domain.UploadSession) (View, error) {
	v := View{Session: s}
	switch s.Status {
	case domain.UploadUploading:
		parts, err := m.objects.ListParts(ctx, s.StagingKey, s.MultipartID)
		if err != nil {
			return View{}, err
		}
		for _, part := range parts {
			if part.Size == s.PartBytes(part.Number) {
				v.Parts = append(v.Parts, part.Number)
			}
		}
		slices.Sort(v.Parts)
	case domain.UploadReady:
		if s.UploadID != nil {
			upload, err := m.sessions.UploadByID(ctx, *s.UploadID)
			if err != nil {
				return View{}, err
			}
			v.Upload = &upload
		}
	}
	return v, nil
}

func (m *Manager) Wait(ctx context.Context, uploader, id uuid.UUID, timeout time.Duration) (View, error) {
	deadline := time.Now().Add(timeout)
	for {
		s, err := m.owned(ctx, uploader, id)
		if err != nil {
			return View{}, err
		}
		if s.Status != domain.UploadProcessing || time.Now().After(deadline) {
			return m.view(ctx, s)
		}
		select {
		case <-ctx.Done():
			return m.view(context.WithoutCancel(ctx), s)
		case <-time.After(waitPoll):
		}
	}
}

func (m *Manager) Complete(ctx context.Context, uploader, id uuid.UUID) (domain.UploadSession, error) {
	s, err := m.owned(ctx, uploader, id)
	if err != nil {
		return domain.UploadSession{}, err
	}
	switch s.Status {
	case domain.UploadProcessing, domain.UploadReady:
		return s, nil
	case domain.UploadFailed:
		return domain.UploadSession{}, domain.ErrSessionState
	}

	parts, err := m.objects.ListParts(ctx, s.StagingKey, s.MultipartID)
	if err != nil {
		return domain.UploadSession{}, err
	}
	byNumber := make(map[int]attachment.Part, len(parts))
	for _, part := range parts {
		byNumber[part.Number] = part
	}
	ordered := make([]attachment.Part, 0, s.PartCount)
	for number := 1; number <= s.PartCount; number++ {
		part, ok := byNumber[number]
		if !ok || part.Size != s.PartBytes(number) {
			return domain.UploadSession{}, domain.ErrPartsMissing
		}
		ordered = append(ordered, part)
	}

	if err := m.objects.CompleteMultipart(ctx, s.StagingKey, s.MultipartID, ordered); err != nil {
		if errors.Is(err, attachment.ErrNoSuchUpload) {
			return m.owned(ctx, uploader, id)
		}
		return domain.UploadSession{}, err
	}
	if err := m.sessions.MarkSessionProcessing(ctx, s.ID); err != nil {
		if errors.Is(err, domain.ErrSessionState) {
			return m.owned(ctx, uploader, id)
		}
		return domain.UploadSession{}, err
	}
	m.enqueue(s.ID)

	s.Status = domain.UploadProcessing
	return s, nil
}

func (m *Manager) Delete(ctx context.Context, uploader, id uuid.UUID) error {
	s, err := m.owned(ctx, uploader, id)
	if err != nil {
		if errors.Is(err, domain.ErrSessionNotFound) {
			return nil
		}
		return err
	}

	switch s.Status {
	case domain.UploadUploading:
		if err := m.objects.AbortMultipart(ctx, s.StagingKey, s.MultipartID); err != nil {
			return err
		}
	case domain.UploadFailed:
		_ = m.objects.AbortMultipart(ctx, s.StagingKey, s.MultipartID)
		m.removeStaging(ctx, s)
	case domain.UploadReady:
		if s.UploadID != nil {
			if err := m.dropDraft(ctx, *s.UploadID); err != nil {
				return err
			}
		}
	}
	return m.sessions.DeleteSession(ctx, s.ID)
}

func (m *Manager) dropDraft(ctx context.Context, uploadID uuid.UUID) error {
	upload, err := m.sessions.UploadByID(ctx, uploadID)
	if err != nil {
		if errors.Is(err, domain.ErrUploadNotFound) {
			return nil
		}
		return err
	}
	if err := m.sessions.DeleteDraftUpload(ctx, uploadID); err != nil {
		return err
	}
	if upload.FileID == nil {
		for key, err := range m.objects.Remove(context.WithoutCancel(ctx), m.keysOf(upload.Payload)) {
			m.log.WarnContext(ctx, "could not remove a withdrawn draft, the sweeper will not see it", "key", key, "error", err)
		}
	}
	return nil
}

func (m *Manager) enqueue(id uuid.UUID) {
	select {
	case m.queue <- id:
	default:
		m.log.Warn("upload queue is full, the recovery loop will pick it up", "upload_session_id", id)
	}
}

func (m *Manager) Run(ctx context.Context) {
	for range m.workers {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case id := <-m.queue:
					m.process(ctx, id)
				}
			}
		}()
	}

	ticker := time.NewTicker(recoverInterval)
	defer ticker.Stop()
	for {
		ids, err := m.sessions.ClaimableSessions(ctx, staleClaim, recoverBatch)
		if err != nil && ctx.Err() == nil {
			m.log.ErrorContext(ctx, "could not look for unfinished uploads", "error", err)
		}
		for _, id := range ids {
			m.enqueue(id)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (m *Manager) process(ctx context.Context, id uuid.UUID) {
	s, claimed, err := m.sessions.ClaimSession(ctx, id, staleClaim)
	if err != nil {
		m.log.ErrorContext(ctx, "could not claim an upload", "upload_session_id", id, "error", err)
		return
	}
	if !claimed {
		return
	}

	log := m.log.With("upload_session_id", s.ID, "user_id", s.UploaderID, "mode", s.Mode)
	upload, err := m.produce(ctx, s)
	if err != nil {
		reason := failureOf(err)
		log.WarnContext(ctx, "upload processing failed", "reason", reason, "error", err)
		if err := m.sessions.FailSession(context.WithoutCancel(ctx), s.ID, reason); err != nil {
			log.ErrorContext(ctx, "could not mark an upload failed", "error", err)
		}
		m.removeStaging(ctx, s)
		return
	}

	if err := m.sessions.FinishSession(ctx, s.ID, upload.ID); err != nil {
		log.InfoContext(ctx, "upload was withdrawn while it was processed", "error", err)
		if err := m.dropDraft(context.WithoutCancel(ctx), upload.ID); err != nil {
			log.WarnContext(ctx, "could not drop a withdrawn upload, the sweeper will", "error", err)
		}
	}
	m.removeStaging(ctx, s)
	log.InfoContext(ctx, "upload processed", "upload_id", upload.ID, "kind", upload.Kind)
}

func (m *Manager) produce(ctx context.Context, s domain.UploadSession) (domain.Upload, error) {
	tmp, err := os.CreateTemp(m.tempDir, "chat-upload-*")
	if err != nil {
		return domain.Upload{}, fmt.Errorf("uploads: temp file: %w", err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(path) }()

	if err := m.objects.Download(ctx, s.StagingKey, path); err != nil {
		return domain.Upload{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return domain.Upload{}, fmt.Errorf("uploads: stat download: %w", err)
	}
	if info.Size() != s.SizeBytes {
		return domain.Upload{}, fmt.Errorf("uploads: assembled %d bytes, expected %d: %w", info.Size(), s.SizeBytes, domain.ErrPartsMissing)
	}

	source, err := hashOf(path)
	if err != nil {
		return domain.Upload{}, err
	}
	if len(s.ExpectedSHA256) > 0 && !bytes.Equal(s.ExpectedSHA256, source) {
		return domain.Upload{}, ErrHashMismatch
	}

	file, found, err := m.sessions.FileBySource(ctx, s.Mode, source)
	if err != nil {
		return domain.Upload{}, err
	}
	if !found {
		file, err = m.processFresh(ctx, s, path, source)
		if err != nil {
			return domain.Upload{}, err
		}
	}

	payload, err := file.PayloadFor(s.Filename)
	if err != nil {
		return domain.Upload{}, err
	}
	return m.sessions.CreateUpload(ctx, domain.Upload{
		ID: m.newID(), UploaderID: s.UploaderID, Kind: file.Kind, Payload: payload, FileID: &file.ID,
	})
}

func (m *Manager) processFresh(ctx context.Context, s domain.UploadSession, path string, source []byte) (domain.StoredFile, error) {
	var (
		result attachment.Result
		err    error
	)
	switch s.Mode {
	case domain.UploadModeFile:
		result, err = m.processor.File(ctx, s.UploaderID, path, s.Filename, s.SizeBytes)
	case domain.UploadModeVoice:
		result, err = m.processor.Voice(ctx, s.UploaderID, path, s.SizeBytes)
	default:
		result, err = m.processor.Media(ctx, s.UploaderID, path, s.SizeBytes)
	}
	if err != nil {
		return domain.StoredFile{}, err
	}

	saved, created, err := m.sessions.SaveFile(ctx, domain.StoredFile{
		ID:         m.newID(),
		Kind:       result.Kind,
		SHA256:     result.SHA256,
		Payload:    result.Payload,
		ObjectKeys: result.Keys,
		SizeBytes:  result.Size,
	}, s.Mode, source, s.SizeBytes)
	if err != nil {
		m.objects.Remove(context.WithoutCancel(ctx), result.Keys)
		return domain.StoredFile{}, err
	}
	if !created {
		for key, err := range m.objects.Remove(context.WithoutCancel(ctx), result.Keys) {
			m.log.WarnContext(ctx, "could not drop a duplicate of a stored file", "key", key, "error", err)
		}
	}
	return saved, nil
}

func (m *Manager) removeStaging(ctx context.Context, s domain.UploadSession) {
	for key, err := range m.objects.Remove(context.WithoutCancel(ctx), []string{s.StagingKey}) {
		m.log.WarnContext(ctx, "could not remove a staged upload, the sweeper will", "key", key, "error", err)
	}
}

func hashOf(path string) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("uploads: open for hashing: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, fmt.Errorf("uploads: hash: %w", err)
	}
	return h.Sum(nil), nil
}

func failureOf(err error) string {
	switch {
	case errors.Is(err, ErrHashMismatch):
		return "hash_mismatch"
	case errors.Is(err, media.ErrTooLarge):
		return "too_large"
	case errors.Is(err, media.ErrUnsupported):
		return "unsupported"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	default:
		return "failed"
	}
}
