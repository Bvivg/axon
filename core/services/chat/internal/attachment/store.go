package attachment

import (
	"bytes"
	"context"
	"fmt"

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
	return &Store{client: client, bucket: cfg.Bucket}, nil
}

type Object struct {
	Key         string
	ContentType string
	Disposition string

	Data []byte

	Path string
}

func (s *Store) Put(ctx context.Context, o Object) error {
	opts := minio.PutObjectOptions{
		ContentType:        o.ContentType,
		ContentDisposition: o.Disposition,
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
