package avatar

import (
	"bytes"
	"fmt"
	"image"

	"github.com/disintegration/imaging"
)

const originalMaxPixels = 2048

var sizePixels = map[Size]int{
	SizeSmall:  48,
	SizeMedium: 128,
	SizeLarge:  512,
}

func derive(raw []byte) (map[Size][]byte, error) {
	src, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}

	out := make(map[Size][]byte, len(sizePixels)+1)

	for size, px := range sizePixels {
		encoded, err := encodeJPEG(imaging.Fill(src, px, px, imaging.Center, imaging.Lanczos))
		if err != nil {
			return nil, err
		}
		out[size] = encoded
	}

	original := src
	if b := original.Bounds(); b.Dx() > originalMaxPixels || b.Dy() > originalMaxPixels {
		original = imaging.Fit(original, originalMaxPixels, originalMaxPixels, imaging.Lanczos)
	}
	encoded, err := encodeJPEG(original)
	if err != nil {
		return nil, err
	}
	out[SizeOriginal] = encoded

	return out, nil
}

func encodeJPEG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, img, imaging.JPEG, imaging.JPEGQuality(90)); err != nil {
		return nil, fmt.Errorf("avatar: encode: %w", err)
	}
	return buf.Bytes(), nil
}
