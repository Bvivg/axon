package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type UploadMode string

const (
	UploadModeMedia UploadMode = "media"
	UploadModeFile  UploadMode = "file"
	UploadModeVoice UploadMode = "voice"
)

type UploadStatus string

const (
	UploadUploading  UploadStatus = "uploading"
	UploadProcessing UploadStatus = "processing"
	UploadReady      UploadStatus = "ready"
	UploadFailed     UploadStatus = "failed"
)

var (
	ErrSessionNotFound = errors.New("chat: no such upload")
	ErrSessionState    = errors.New("chat: the upload is not in a state for this")
	ErrUploadInUse     = errors.New("chat: the upload is already part of a message")
	ErrPartsMissing    = errors.New("chat: some parts of the upload are missing")
)

type UploadSession struct {
	ID         uuid.UUID
	UploaderID uuid.UUID
	Mode       UploadMode
	Filename   string

	SizeBytes int64
	PartSize  int64
	PartCount int

	StagingKey     string
	MultipartID    string
	ExpectedSHA256 []byte

	Status   UploadStatus
	Failure  string
	UploadID *uuid.UUID

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s UploadSession) PartBytes(number int) int64 {
	if number < s.PartCount {
		return s.PartSize
	}
	return s.SizeBytes - int64(s.PartCount-1)*s.PartSize
}

type StoredFile struct {
	ID         uuid.UUID
	Kind       MessageKind
	SHA256     []byte
	Payload    json.RawMessage
	ObjectKeys []string
	SizeBytes  int64
}

func (f StoredFile) PayloadFor(filename string) (json.RawMessage, error) {
	if f.Kind != MessageKindAttachment {
		return f.Payload, nil
	}
	var payload AttachmentPayload
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		return nil, err
	}
	payload.Filename = filename
	return json.Marshal(payload)
}
