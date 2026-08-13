package avatar

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func syntheticImage(t *testing.T, width, height int, encodeJPEGSource bool) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 200, A: 255})
		}
	}

	var buf bytes.Buffer
	var err error
	if encodeJPEGSource {
		err = jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90})
	} else {
		err = png.Encode(&buf, img)
	}
	if err != nil {
		t.Fatalf("encode fixture: %v", err)
	}
	return buf.Bytes()
}

func TestDeriveProducesEverySize(t *testing.T) {
	raw := syntheticImage(t, 800, 600, true)

	out, err := derive(raw)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	for size, want := range map[Size]int{
		SizeSmall:  48,
		SizeMedium: 128,
		SizeLarge:  512,
	} {
		data, ok := out[size]
		if !ok {
			t.Fatalf("missing size %q", size)
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode %q: %v", size, err)
		}
		if b := img.Bounds(); b.Dx() != want || b.Dy() != want {
			t.Errorf("%q = %dx%d, want %dx%d", size, b.Dx(), b.Dy(), want, want)
		}
	}

	original, ok := out[SizeOriginal]
	if !ok {
		t.Fatal("missing original")
	}
	img, err := jpeg.Decode(bytes.NewReader(original))
	if err != nil {
		t.Fatalf("decode original: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 800 || b.Dy() != 600 {
		t.Errorf("original = %dx%d, want 800x600 (below the cap, so unchanged)", b.Dx(), b.Dy())
	}
}

func TestDeriveCapsTheOriginal(t *testing.T) {
	raw := syntheticImage(t, 3000, 1500, true)

	out, err := derive(raw)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}

	img, err := jpeg.Decode(bytes.NewReader(out[SizeOriginal]))
	if err != nil {
		t.Fatalf("decode original: %v", err)
	}
	b := img.Bounds()
	if b.Dx() > originalMaxPixels || b.Dy() > originalMaxPixels {
		t.Errorf("original = %dx%d, exceeds the %dpx cap", b.Dx(), b.Dy(), originalMaxPixels)
	}
	if b.Dx() != originalMaxPixels {
		t.Errorf("original width = %d, want %d (the longer side hits the cap)", b.Dx(), originalMaxPixels)
	}
}

func TestDeriveAcceptsPNG(t *testing.T) {
	raw := syntheticImage(t, 200, 200, false)

	if _, err := derive(raw); err != nil {
		t.Fatalf("derive: %v", err)
	}
}

func TestDeriveRejectsNonImages(t *testing.T) {
	_, err := derive([]byte("this is not an image, just some bytes pretending to be one"))
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}

func TestDeriveRejectsEmptyInput(t *testing.T) {
	_, err := derive(nil)
	if !errors.Is(err, ErrInvalidImage) {
		t.Fatalf("err = %v, want ErrInvalidImage", err)
	}
}
