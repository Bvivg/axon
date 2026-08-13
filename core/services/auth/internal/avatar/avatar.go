package avatar

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Size string

const (
	SizeSmall    Size = "small"
	SizeMedium   Size = "medium"
	SizeLarge    Size = "large"
	SizeOriginal Size = "original"
)

const MaxUploadBytes = 5 << 20

var (
	ErrInvalidImage  = errors.New("avatar: not a decodable image")
	ErrImageTooLarge = errors.New("avatar: image exceeds the upload limit")
)

type URLs struct {
	Small    string
	Medium   string
	Large    string
	Original string
}

type URLBuilder struct {
	publicURL string
	bucket    string
}

func NewURLBuilder(publicURL, bucket string) URLBuilder {
	return URLBuilder{publicURL: strings.TrimRight(publicURL, "/"), bucket: bucket}
}

func (b URLBuilder) URLs(userID uuid.UUID) URLs {
	return URLs{
		Small:    b.urlFor(userID, SizeSmall),
		Medium:   b.urlFor(userID, SizeMedium),
		Large:    b.urlFor(userID, SizeLarge),
		Original: b.urlFor(userID, SizeOriginal),
	}
}

func (b URLBuilder) urlFor(userID uuid.UUID, size Size) string {
	return b.publicURL + "/" + b.bucket + "/" + objectKey(userID, size)
}

func objectKey(userID uuid.UUID, size Size) string {
	return userID.String() + "/" + string(size) + ".jpg"
}

type Pipeline struct {
	store  *Store
	urls   URLBuilder
	client *http.Client
}

func NewPipeline(store *Store, urls URLBuilder) *Pipeline {
	return &Pipeline{
		store:  store,
		urls:   urls,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *Pipeline) Process(ctx context.Context, userID uuid.UUID, raw []byte) (URLs, error) {
	if len(raw) > MaxUploadBytes {
		return URLs{}, ErrImageTooLarge
	}

	sizes, err := derive(raw)
	if err != nil {
		return URLs{}, err
	}

	for size, data := range sizes {
		if err := p.store.put(ctx, userID, size, data); err != nil {
			return URLs{}, err
		}
	}

	return p.urls.URLs(userID), nil
}

func (p *Pipeline) ProcessFromURL(ctx context.Context, userID uuid.UUID, sourceURL string) (URLs, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return URLs{}, fmt.Errorf("avatar: build request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return URLs{}, fmt.Errorf("avatar: fetch %s: %w", sourceURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return URLs{}, fmt.Errorf("avatar: fetch %s: status %d", sourceURL, resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxUploadBytes+1))
	if err != nil {
		return URLs{}, fmt.Errorf("avatar: read %s: %w", sourceURL, err)
	}

	return p.Process(ctx, userID, raw)
}
