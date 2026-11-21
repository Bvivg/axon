package avatar

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
)

func pngHeaderOnly(t *testing.T, width, height uint32) []byte {
	t.Helper()

	chunk := func(kind string, data []byte) []byte {
		var out bytes.Buffer
		_ = binary.Write(&out, binary.BigEndian, uint32(len(data)))
		out.WriteString(kind)
		out.Write(data)
		_ = binary.Write(&out, binary.BigEndian, crc32.ChecksumIEEE(append([]byte(kind), data...)))
		return out.Bytes()
	}

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], width)
	binary.BigEndian.PutUint32(ihdr[4:], height)
	ihdr[8], ihdr[9] = 8, 6

	var idat bytes.Buffer
	w := zlib.NewWriter(&idat)
	_, _ = w.Write(make([]byte, 1+4*int(width)))
	_ = w.Close()

	var out bytes.Buffer
	out.WriteString("\x89PNG\r\n\x1a\n")
	out.Write(chunk("IHDR", ihdr))
	out.Write(chunk("IDAT", idat.Bytes()))
	out.Write(chunk("IEND", nil))
	return out.Bytes()
}

func stripe(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewGray(image.Rect(0, 0, width, height))
	for x := range width {
		img.SetGray(x, 0, color.Gray{Y: uint8(x)})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func TestDeriveRefusesAnImageWiderThanTheLimit(t *testing.T) {
	_, err := derive(stripe(t, 9000, 1), DefaultLimits())
	if !errors.Is(err, ErrImageDimensionsTooLarge) {
		t.Fatalf("err = %v, want ErrImageDimensionsTooLarge", err)
	}
}

func TestDeriveRefusesTooManyPixelsWithinTheSideLimit(t *testing.T) {
	_, err := derive(stripe(t, 600, 500), Limits{MaxPixels: 200_000, MaxSide: 8192})
	if !errors.Is(err, ErrImageDimensionsTooLarge) {
		t.Fatalf("err = %v, want ErrImageDimensionsTooLarge", err)
	}
}

func TestDeriveRefusesAPixelBombWithoutDecodingIt(t *testing.T) {
	for _, side := range []uint32{20000, 8000} {
		bomb := pngHeaderOnly(t, side, side)

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)

		_, err := derive(bomb, DefaultLimits())

		runtime.ReadMemStats(&after)
		if !errors.Is(err, ErrImageDimensionsTooLarge) {
			t.Fatalf("%dx%d: err = %v, want ErrImageDimensionsTooLarge", side, side, err)
		}
		if grown := after.TotalAlloc - before.TotalAlloc; grown > 16<<20 {
			t.Errorf("%dx%d: refusing the bomb allocated %d MiB", side, side, grown>>20)
		}
	}
}

func TestProcessFailsFastWhenEverySlotIsBusy(t *testing.T) {
	pipeline := NewPipeline(nil, NewURLBuilder("https://cdn.example.com", "avatars"), nil,
		Limits{MaxConcurrent: 1, QueueTimeout: 20 * time.Millisecond})

	release, err := pipeline.gate.Acquire(t.Context())
	if err != nil {
		t.Fatalf("hold the only slot: %v", err)
	}
	defer release()

	start := time.Now()
	_, err = pipeline.Process(t.Context(), uuid.New(), stripe(t, 64, 64))
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy", err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Errorf("a saturated pipeline held the request for %v", waited)
	}
}

func TestProcessFromURLIsHeldToTheSameLimits(t *testing.T) {
	bomb := pngHeaderOnly(t, 20000, 20000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(bomb)
	}))
	defer server.Close()

	pipeline := NewPipeline(nil, NewURLBuilder("https://cdn.example.com", "avatars"), server.Client(), DefaultLimits())

	_, err := pipeline.ProcessFromURL(t.Context(), uuid.New(), server.URL+"/avatar.png")
	if !errors.Is(err, ErrImageDimensionsTooLarge) {
		t.Fatalf("err = %v, want ErrImageDimensionsTooLarge", err)
	}
}
