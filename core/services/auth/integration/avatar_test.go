//go:build integration

package integration

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/auth/internal/avatar"
)

func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func fetch(t *testing.T, url string) []byte {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", url, err)
	}
	return data
}

func TestPipelineProcessStoresEveryPublicSize(t *testing.T) {
	ctx := t.Context()
	userID := uuid.New()

	urls, err := avatarPipeline.Process(ctx, userID, testPNG(t, 800, 600))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	for size, want := range map[string]struct {
		url           string
		width, height int
	}{
		"small":  {urls.Small, 48, 48},
		"medium": {urls.Medium, 128, 128},
		"large":  {urls.Large, 512, 512},
	} {
		data := fetch(t, want.url)
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: decode: %v", size, err)
		}
		if b := img.Bounds(); b.Dx() != want.width || b.Dy() != want.height {
			t.Errorf("%s = %dx%d, want %dx%d", size, b.Dx(), b.Dy(), want.width, want.height)
		}
	}

	original := fetch(t, urls.Original)
	img, err := jpeg.Decode(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("original: decode: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 600 {
		t.Errorf("original = %dx%d, want 800x600", b.Dx(), b.Dy())
	}
}

func TestPipelineProcessRejectsGarbage(t *testing.T) {
	_, err := avatarPipeline.Process(t.Context(), uuid.New(), []byte("not an image"))
	if err == nil {
		t.Fatal("garbage input was accepted as an image")
	}
}

func TestPipelineProcessFromURLDownloadsAndStores(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(testPNG(t, 300, 300))
	}))
	defer source.Close()

	urls, err := avatarPipeline.ProcessFromURL(t.Context(), uuid.New(), source.URL)
	if err != nil {
		t.Fatalf("ProcessFromURL: %v", err)
	}

	data := fetch(t, urls.Medium)
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode medium: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 128 || b.Dy() != 128 {
		t.Errorf("medium = %dx%d, want 128x128", b.Dx(), b.Dy())
	}
}

func TestPipelineProcessFromURLRejectsOversizedDownloads(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(make([]byte, avatar.MaxUploadBytes+1024))
	}))
	defer source.Close()

	_, err := avatarPipeline.ProcessFromURL(t.Context(), uuid.New(), source.URL)
	if err == nil {
		t.Fatal("an oversized download was accepted")
	}
}
