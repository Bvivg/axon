package avatar

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type StoreConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type Store struct {
	client *minio.Client
	bucket string
}

func NewStore(cfg StoreConfig) (*Store, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("avatar: minio client: %w", err)
	}
	return &Store{client: client, bucket: cfg.Bucket}, nil
}

func (s *Store) put(ctx context.Context, userID uuid.UUID, size Size, data []byte) error {
	key := objectKey(userID, size)

	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "image/jpeg"})
	if err != nil {
		return fmt.Errorf("avatar: upload %s: %w", key, err)
	}
	return nil
}
