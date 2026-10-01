package attachment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const immutableCache = "public, max-age=31536000, immutable"

type StoreConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type Store struct {
	client *minio.Client
	core   minio.Core
	bucket string
}

func NewStore(cfg StoreConfig) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("attachment: minio client: %w", err)
	}
	return &Store{client: client, core: minio.Core{Client: client}, bucket: cfg.Bucket}, nil
}

type Object struct {
	Key         string
	ContentType string
	Disposition string
	Encoding    string

	Data []byte

	Path string
}

func (s *Store) Put(ctx context.Context, o Object) error {
	opts := minio.PutObjectOptions{
		ContentType:        o.ContentType,
		ContentDisposition: o.Disposition,
		ContentEncoding:    o.Encoding,
		CacheControl:       immutableCache,
	}

	var err error
	if o.Path != "" {
		_, err = s.client.FPutObject(ctx, s.bucket, o.Key, o.Path, opts)
	} else {
		_, err = s.client.PutObject(ctx, s.bucket, o.Key, bytes.NewReader(o.Data), int64(len(o.Data)), opts)
	}
	if err != nil {
		return fmt.Errorf("attachment: upload %s: %w", o.Key, err)
	}
	return nil
}

func (s *Store) Remove(ctx context.Context, keys []string) map[string]error {
	failed := map[string]error{}
	if len(keys) == 0 {
		return failed
	}

	objects := make(chan minio.ObjectInfo, len(keys))
	for _, key := range keys {
		objects <- minio.ObjectInfo{Key: key}
	}
	close(objects)

	for failure := range s.client.RemoveObjects(ctx, s.bucket, objects, minio.RemoveObjectsOptions{}) {
		failed[failure.ObjectName] = fmt.Errorf("attachment: remove %s: %w", failure.ObjectName, failure.Err)
	}
	return failed
}

type Part struct {
	Number int
	Size   int64
	ETag   string
}

var ErrNoSuchUpload = errors.New("attachment: the multipart upload is gone")

func (s *Store) StartMultipart(ctx context.Context, key string) (string, error) {
	id, err := s.core.NewMultipartUpload(ctx, s.bucket, key, minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return "", fmt.Errorf("attachment: start multipart %s: %w", key, err)
	}
	return id, nil
}

func (s *Store) PutPart(ctx context.Context, key, uploadID string, number int, data io.Reader, size int64) error {
	if _, err := s.core.PutObjectPart(ctx, s.bucket, key, uploadID, number, data, size, minio.PutObjectPartOptions{}); err != nil {
		return wrapMultipart(fmt.Sprintf("put part %d of %s", number, key), err)
	}
	return nil
}

func (s *Store) ListParts(ctx context.Context, key, uploadID string) ([]Part, error) {
	var parts []Part
	marker := 0
	for {
		result, err := s.core.ListObjectParts(ctx, s.bucket, key, uploadID, marker, 1000)
		if err != nil {
			return nil, wrapMultipart("list parts of "+key, err)
		}
		for _, part := range result.ObjectParts {
			parts = append(parts, Part{Number: part.PartNumber, Size: part.Size, ETag: part.ETag})
		}
		if !result.IsTruncated {
			return parts, nil
		}
		marker = result.NextPartNumberMarker
	}
}

func (s *Store) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	complete := make([]minio.CompletePart, len(parts))
	for i, part := range parts {
		complete[i] = minio.CompletePart{PartNumber: part.Number, ETag: part.ETag}
	}
	if _, err := s.core.CompleteMultipartUpload(ctx, s.bucket, key, uploadID, complete, minio.PutObjectOptions{}); err != nil {
		return wrapMultipart("complete "+key, err)
	}
	return nil
}

func (s *Store) AbortMultipart(ctx context.Context, key, uploadID string) error {
	if err := s.core.AbortMultipartUpload(ctx, s.bucket, key, uploadID); err != nil {
		if errors.Is(wrapMultipart("", err), ErrNoSuchUpload) {
			return nil
		}
		return fmt.Errorf("attachment: abort %s: %w", key, err)
	}
	return nil
}

func (s *Store) Download(ctx context.Context, key, path string) error {
	if err := s.client.FGetObject(ctx, s.bucket, key, path, minio.GetObjectOptions{}); err != nil {
		return fmt.Errorf("attachment: download %s: %w", key, err)
	}
	return nil
}

func wrapMultipart(what string, err error) error {
	if minio.ToErrorResponse(err).Code == "NoSuchUpload" {
		return ErrNoSuchUpload
	}
	return fmt.Errorf("attachment: %s: %w", what, err)
}
