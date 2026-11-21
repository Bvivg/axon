package media_test

import (
	"bytes"
	"compress/lzw"
	"encoding/binary"
	"errors"
	"image/gif"
	"runtime"
	"testing"

	"github.com/bvivg/axon/core/services/chat/internal/media"
)

type frame struct{ w, h int }

func lzwZeros(t *testing.T, pixels int) []byte {
	t.Helper()
	var compressed bytes.Buffer
	w := lzw.NewWriter(&compressed, lzw.LSB, 2)
	row := make([]byte, 64<<10)
	for left := pixels; left > 0; left -= len(row) {
		if _, err := w.Write(row[:min(left, len(row))]); err != nil {
			t.Fatalf("lzw: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("lzw: %v", err)
	}

	var blocks bytes.Buffer
	data := compressed.Bytes()
	for len(data) > 0 {
		n := min(len(data), 255)
		blocks.WriteByte(byte(n))
		blocks.Write(data[:n])
		data = data[n:]
	}
	blocks.WriteByte(0)
	return blocks.Bytes()
}

func gifHeader(screenW, screenH int) *bytes.Buffer {
	le := binary.LittleEndian
	var buf bytes.Buffer
	buf.WriteString("GIF89a")
	buf.Write(le.AppendUint16(le.AppendUint16(nil, uint16(screenW)), uint16(screenH)))
	buf.Write([]byte{0x80, 0, 0})
	buf.Write([]byte{0, 0, 0, 0xff, 0xff, 0xff})
	buf.Write([]byte{0x21, 0xff, 0x0b})
	buf.WriteString("NETSCAPE2.0")
	buf.Write([]byte{0x03, 0x01, 0x00, 0x00, 0x00})
	return &buf
}

func writeFrames(t *testing.T, buf *bytes.Buffer, frames ...frame) {
	t.Helper()
	le := binary.LittleEndian
	data := map[frame][]byte{}
	for _, f := range frames {
		if _, ok := data[f]; !ok {
			data[f] = lzwZeros(t, f.w*f.h)
		}
		buf.Write([]byte{0x21, 0xf9, 0x04, 0x00, 0x0a, 0x00, 0x00, 0x00})
		buf.WriteByte(0x2c)
		buf.Write(le.AppendUint16(le.AppendUint16(le.AppendUint16(le.AppendUint16(nil, 0), 0), uint16(f.w)), uint16(f.h)))
		buf.WriteByte(0)
		buf.WriteByte(2)
		buf.Write(data[f])
	}
}

func repeat(f frame, n int) []frame {
	out := make([]frame, n)
	for i := range out {
		out[i] = f
	}
	return out
}

func animation(t *testing.T, screenW, screenH int, frames ...frame) []byte {
	t.Helper()
	buf := gifHeader(screenW, screenH)
	writeFrames(t, buf, frames...)
	buf.WriteByte(0x3b)
	return buf.Bytes()
}

func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestAGIFBombIsRefusedBeforeItIsDecoded(t *testing.T) {
	bomb := animation(t, 7000, 7000, repeat(frame{7000, 7000}, 10)...)

	var err error
	allocated := allocatedBy(func() { _, err = media.ProcessImage(bomb, media.ImageLimits{}) })

	if !errors.Is(err, media.ErrAnimationTooLarge) {
		t.Errorf("ProcessImage = %v, want ErrAnimationTooLarge", err)
	}
	if allocated > 32<<20 {
		t.Errorf("refusing a %d KB bomb allocated %d MB", len(bomb)>>10, allocated>>20)
	}
}

func TestAnimationsOverTheLimitsAreRefused(t *testing.T) {
	tests := map[string]struct {
		raw    []byte
		limits media.ImageLimits
	}{
		"a huge screen with tiny frames": {
			raw: animation(t, 7000, 7000, frame{16, 16}, frame{16, 16}),
		},
		"more frames than allowed": {
			raw: animation(t, 16, 16, repeat(frame{16, 16}, 1001)...),
		},
		"more decoded pixels than allowed": {
			raw: animation(t, 4000, 4000, repeat(frame{4000, 4000}, 20)...),
		},
		"a configured frame limit": {
			raw:    animation(t, 8, 8, repeat(frame{8, 8}, 4)...),
			limits: media.ImageLimits{MaxFrames: 3},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			var err error
			allocated := allocatedBy(func() { _, err = media.ProcessImage(tt.raw, tt.limits) })
			if !errors.Is(err, media.ErrAnimationTooLarge) {
				t.Fatalf("ProcessImage = %v, want ErrAnimationTooLarge", err)
			}
			if allocated > 32<<20 {
				t.Errorf("refusing it allocated %d MB", allocated>>20)
			}
		})
	}
}

func TestRealAnimationsAndStickersPass(t *testing.T) {
	deltas := append([]frame{{1280, 720}}, repeat(frame{200, 100}, 299)...)

	tests := map[string]struct {
		raw    []byte
		frames int
	}{
		"a sticker":                    {raw: animation(t, 512, 512, repeat(frame{512, 512}, 30)...), frames: 30},
		"a screen recording of deltas": {raw: animation(t, 1280, 720, deltas...), frames: 300},
		"exactly the frame limit":      {raw: animation(t, 8, 8, repeat(frame{8, 8}, 1000)...), frames: 1000},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := media.ProcessImage(tt.raw, media.ImageLimits{})
			if err != nil {
				t.Fatalf("ProcessImage: %v", err)
			}
			out, err := gif.DecodeAll(bytes.NewReader(got.Data))
			if err != nil {
				t.Fatalf("decode output: %v", err)
			}
			if len(out.Image) != tt.frames {
				t.Errorf("frames = %d, want %d", len(out.Image), tt.frames)
			}
		})
	}
}

func TestFramesBehindAPlainTextExtensionAreStillCounted(t *testing.T) {
	buf := gifHeader(8, 8)
	buf.Write([]byte{0x21, 0x01, 0x00, 0x3b})
	buf.Write(make([]byte, 11))
	buf.WriteByte(0)
	writeFrames(t, buf, repeat(frame{8, 8}, 3)...)
	buf.WriteByte(0x3b)

	if g, err := gif.DecodeAll(bytes.NewReader(buf.Bytes())); err != nil || len(g.Image) != 3 {
		t.Fatalf("the reference decoder sees %v frames, err %v; want 3", len(g.Image), err)
	}
	if _, err := media.ProcessImage(buf.Bytes(), media.ImageLimits{MaxFrames: 2}); !errors.Is(err, media.ErrAnimationTooLarge) {
		t.Fatalf("ProcessImage = %v, want ErrAnimationTooLarge", err)
	}
}

func TestBrokenGIFsAreRefusedAsUnsupported(t *testing.T) {
	whole := animation(t, 8, 8, frame{8, 8}, frame{8, 8})
	outside := animation(t, 8, 8, frame{8, 8}, frame{9, 8})

	tests := map[string][]byte{
		"cut short":               whole[:len(whole)-5],
		"no trailer":              whole[:len(whole)-1],
		"a frame past the screen": outside,
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := media.ProcessImage(raw, media.ImageLimits{}); !errors.Is(err, media.ErrUnsupported) {
				t.Fatalf("ProcessImage = %v, want ErrUnsupported", err)
			}
		})
	}
}

func TestAStillGIFOverThePixelLimitIsRefused(t *testing.T) {
	still := animation(t, 8000, 8000, frame{16, 16})

	if _, err := media.ProcessImage(still, media.ImageLimits{}); !errors.Is(err, media.ErrTooLarge) {
		t.Fatalf("ProcessImage = %v, want ErrTooLarge", err)
	}
}
