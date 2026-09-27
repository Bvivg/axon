package attachment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/media"
)

const (
	MaxImageBytes = 25 << 20
	MaxVoiceBytes = 10 << 20

	sniffBytes = 512

	posterOffset = 500 * time.Millisecond
)

type Result struct {
	Kind    domain.MessageKind
	Payload json.RawMessage
}

type Pipeline struct {
	store *Store
	urls  URLBuilder
	tools *media.Tools
}

func NewPipeline(store *Store, urls URLBuilder, tools *media.Tools) *Pipeline {
	return &Pipeline{store: store, urls: urls, tools: tools}
}

func (p *Pipeline) Media(ctx context.Context, userID uuid.UUID, path string, size int64) (Result, error) {
	head, err := readHead(path)
	if err != nil {
		return Result{}, err
	}

	if media.IsImageMime(media.Sniff(head)) {
		return p.image(ctx, userID, path, size)
	}
	return p.video(ctx, userID, path)
}

func (p *Pipeline) image(ctx context.Context, userID uuid.UUID, path string, size int64) (Result, error) {
	if size > MaxImageBytes {
		return Result{}, media.ErrTooLarge
	}

	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Result{}, fmt.Errorf("attachment: read image: %w", err)
	}

	img, err := media.ProcessImage(raw)
	if err != nil {
		return Result{}, err
	}

	base := newObjectBase(userID)
	mainKey := base + extensionFor(img.Mime)
	thumbKey := base + ".thumb" + extensionFor(img.ThumbnailMime)

	if err := p.store.Put(ctx, Object{Key: mainKey, ContentType: img.Mime, Data: img.Data}); err != nil {
		return Result{}, err
	}
	if err := p.store.Put(ctx, Object{Key: thumbKey, ContentType: img.ThumbnailMime, Data: img.Thumbnail}); err != nil {
		return Result{}, err
	}

	return resultOf(domain.MessageKindImage, domain.ImagePayload{
		URL:          p.urls.URL(mainKey),
		ThumbnailURL: p.urls.URL(thumbKey),
		Width:        img.Width,
		Height:       img.Height,
		SizeBytes:    int64(len(img.Data)),
		Mime:         img.Mime,
	})
}

func (p *Pipeline) video(ctx context.Context, userID uuid.UUID, path string) (Result, error) {
	probe, err := p.tools.Probe(ctx, path, media.VideoDemuxers)
	if err != nil {
		return Result{}, err
	}
	if !probe.HasVideo || probe.StillImage {
		return Result{}, media.ErrUnsupported
	}

	out := path + ".mp4"
	poster := path + ".jpg"
	defer removeAll(out, poster)

	if err := p.tools.TranscodeVideo(ctx, path, out); err != nil {
		return Result{}, err
	}

	encoded, err := p.tools.Probe(ctx, out, media.MP4Demuxer)
	if err != nil {
		return Result{}, err
	}
	if encoded.Width <= 0 || encoded.Height <= 0 {
		return Result{}, media.ErrUnsupported
	}

	at := posterOffset
	if encoded.Duration < 2*posterOffset {
		at = encoded.Duration / 2
	}
	if err := p.tools.Poster(ctx, out, poster, at); err != nil {
		return Result{}, err
	}

	info, err := os.Stat(out)
	if err != nil {
		return Result{}, fmt.Errorf("attachment: stat video: %w", err)
	}

	base := newObjectBase(userID)
	videoKey := base + ".mp4"
	posterKey := base + ".poster.jpg"

	if err := p.store.Put(ctx, Object{Key: videoKey, ContentType: "video/mp4", Path: out}); err != nil {
		return Result{}, err
	}
	if err := p.store.Put(ctx, Object{Key: posterKey, ContentType: "image/jpeg", Path: poster}); err != nil {
		return Result{}, err
	}

	return resultOf(domain.MessageKindVideo, domain.VideoPayload{
		URL:        p.urls.URL(videoKey),
		PosterURL:  p.urls.URL(posterKey),
		Width:      encoded.Width,
		Height:     encoded.Height,
		DurationMS: encoded.Duration.Milliseconds(),
		SizeBytes:  info.Size(),
		Mime:       "video/mp4",
	})
}

func (p *Pipeline) Voice(ctx context.Context, userID uuid.UUID, path string, size int64) (Result, error) {
	if size > MaxVoiceBytes {
		return Result{}, media.ErrTooLarge
	}

	probe, err := p.tools.Probe(ctx, path, media.VoiceDemuxers)
	if err != nil {
		return Result{}, err
	}
	if !probe.HasAudio {
		return Result{}, media.ErrUnsupported
	}

	out := path + ".m4a"
	defer removeAll(out)

	if err := p.tools.TranscodeVoice(ctx, path, out); err != nil {
		return Result{}, err
	}

	encoded, err := p.tools.Probe(ctx, out, media.MP4Demuxer)
	if err != nil {
		return Result{}, err
	}
	switch {
	case encoded.Duration <= 0:
		return Result{}, media.ErrUnsupported
	case encoded.Duration > media.MaxVoiceDuration:
		return Result{}, media.ErrTooLarge
	}

	info, err := os.Stat(out)
	if err != nil {
		return Result{}, fmt.Errorf("attachment: stat voice: %w", err)
	}

	key := newObjectBase(userID) + ".m4a"
	if err := p.store.Put(ctx, Object{Key: key, ContentType: "audio/mp4", Path: out}); err != nil {
		return Result{}, err
	}

	return resultOf(domain.MessageKindVoice, domain.VoicePayload{
		DurationMS: max(encoded.Duration.Milliseconds(), 1),
		URL:        p.urls.URL(key),
		Mime:       "audio/mp4",
		SizeBytes:  info.Size(),
	})
}

func (p *Pipeline) File(ctx context.Context, userID uuid.UUID, path, filename string, size int64) (Result, error) {
	head, err := readHead(path)
	if err != nil {
		return Result{}, err
	}
	contentType := media.Sniff(head)

	key := NewObjectKey(userID, filename)
	if err := p.store.Put(ctx, Object{
		Key:         key,
		ContentType: contentType,
		Disposition: mime.FormatMediaType("attachment", map[string]string{"filename": filename}),
		Path:        path,
	}); err != nil {
		return Result{}, err
	}

	return resultOf(domain.MessageKindAttachment, domain.AttachmentPayload{
		URL:       p.urls.URL(key),
		Filename:  filename,
		Mime:      contentType,
		SizeBytes: size,
	})
}

func resultOf(kind domain.MessageKind, payload any) (Result, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("attachment: marshal %s payload: %w", kind, err)
	}
	return Result{Kind: kind, Payload: raw}, nil
}

func readHead(path string) ([]byte, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("attachment: open upload: %w", err)
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, sniffBytes)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("attachment: read upload: %w", err)
	}
	return head[:n], nil
}

func extensionFor(contentType string) string {
	switch contentType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	default:
		return ".jpg"
	}
}

func removeAll(paths ...string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
