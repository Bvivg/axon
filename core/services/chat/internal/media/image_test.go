package media_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/color/palette"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/bvivg/axon/core/services/chat/internal/media"
)

func solid(w, h int, left, right color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, left)
			} else {
				img.Set(x, y, right)
			}
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func exifSegment(orientation uint16) []byte {
	le := binary.LittleEndian
	tiff := make([]byte, 0, 64)
	tiff = append(tiff, 'I', 'I', 0x2A, 0x00)
	tiff = le.AppendUint32(tiff, 8)

	const ifd0Entries = 2
	gpsOffset := uint32(8 + 2 + ifd0Entries*12 + 4)

	tiff = le.AppendUint16(tiff, ifd0Entries)
	tiff = le.AppendUint16(tiff, 0x0112)
	tiff = le.AppendUint16(tiff, 3)
	tiff = le.AppendUint32(tiff, 1)
	tiff = le.AppendUint16(tiff, orientation)
	tiff = le.AppendUint16(tiff, 0)
	tiff = le.AppendUint16(tiff, 0x8825)
	tiff = le.AppendUint16(tiff, 4)
	tiff = le.AppendUint32(tiff, 1)
	tiff = le.AppendUint32(tiff, gpsOffset)
	tiff = le.AppendUint32(tiff, 0)

	tiff = le.AppendUint16(tiff, 1)
	tiff = le.AppendUint16(tiff, 0x0001)
	tiff = le.AppendUint16(tiff, 2)
	tiff = le.AppendUint32(tiff, 2)
	tiff = append(tiff, 'N', 0, 0, 0)
	tiff = le.AppendUint32(tiff, 0)

	body := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(body)+2))
	return append(segment, body...)
}

func withEXIF(jpegBytes []byte, orientation uint16) []byte {
	out := append([]byte{}, jpegBytes[:2]...)
	out = append(out, exifSegment(orientation)...)
	return append(out, jpegBytes[2:]...)
}

func TestPhotosAreTurnedUprightAndLoseTheirMetadata(t *testing.T) {
	raw := withEXIF(encodeJPEG(t, solid(40, 20, color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255})), 6)

	got, err := media.ProcessImage(raw)
	if err != nil {
		t.Fatalf("ProcessImage: %v", err)
	}

	if got.Width != 20 || got.Height != 40 {
		t.Errorf("size = %dx%d, want 20x40 after the EXIF rotation", got.Width, got.Height)
	}
	if got.Mime != "image/jpeg" {
		t.Errorf("mime = %q, want image/jpeg", got.Mime)
	}
	for _, blob := range [][]byte{got.Data, got.Thumbnail} {
		if bytes.Contains(blob, []byte("Exif")) || bytes.Contains(blob, []byte{0xFF, 0xE1}) {
			t.Error("the output still carries an EXIF segment")
		}
	}

	decoded, err := jpeg.Decode(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	r, _, b, _ := decoded.At(10, 5).RGBA()
	if r < b {
		t.Errorf("the top of the rotated photo is blue, want the red half there")
	}
}

func TestLargePhotosAreScaledDownWithAThumbnail(t *testing.T) {
	raw := encodeJPEG(t, solid(3000, 1000, color.White, color.Black))

	got, err := media.ProcessImage(raw)
	if err != nil {
		t.Fatalf("ProcessImage: %v", err)
	}
	if got.Width != media.MaxImageSide || got.Height != 853 {
		t.Errorf("size = %dx%d, want %dx853", got.Width, got.Height, media.MaxImageSide)
	}

	thumb, _, err := image.DecodeConfig(bytes.NewReader(got.Thumbnail))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	if thumb.Width != media.ThumbnailSide || thumb.Height != 160 {
		t.Errorf("thumbnail = %dx%d, want %dx160", thumb.Width, thumb.Height, media.ThumbnailSide)
	}
}

func TestTransparentImagesStayPNG(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	img.Set(1, 1, color.NRGBA{R: 255, A: 128})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	got, err := media.ProcessImage(buf.Bytes())
	if err != nil {
		t.Fatalf("ProcessImage: %v", err)
	}
	if got.Mime != "image/png" || got.ThumbnailMime != "image/png" {
		t.Errorf("mime = %q / %q, want image/png for both", got.Mime, got.ThumbnailMime)
	}
}

func TestAnimatedGIFsKeepTheirFrames(t *testing.T) {
	frames := &gif.GIF{}
	for i := range 3 {
		frame := image.NewPaletted(image.Rect(0, 0, 10, 10), palette.Plan9)
		frame.SetColorIndex(i, i, uint8(i+1))
		frames.Image = append(frames.Image, frame)
		frames.Delay = append(frames.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, frames); err != nil {
		t.Fatalf("encode gif: %v", err)
	}

	got, err := media.ProcessImage(buf.Bytes())
	if err != nil {
		t.Fatalf("ProcessImage: %v", err)
	}
	if got.Mime != "image/gif" {
		t.Fatalf("mime = %q, want image/gif", got.Mime)
	}
	out, err := gif.DecodeAll(bytes.NewReader(got.Data))
	if err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if len(out.Image) != 3 {
		t.Errorf("frames = %d, want 3", len(out.Image))
	}
}

func TestWhatIsNotAPhotoIsRefused(t *testing.T) {
	_, err := media.ProcessImage([]byte("<html><script>alert(1)</script></html>"))
	if !errors.Is(err, media.ErrUnsupported) {
		t.Fatalf("ProcessImage = %v, want ErrUnsupported", err)
	}
}

func TestAPhotoThatWouldDecodeToGigabytesIsRefused(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte("\x89PNG\r\n\x1a\n"))

	ihdr := binary.BigEndian.AppendUint32(nil, 30000)
	ihdr = binary.BigEndian.AppendUint32(ihdr, 30000)
	ihdr = append(ihdr, 8, 2, 0, 0, 0)

	chunk := append([]byte("IHDR"), ihdr...)
	buf.Write(binary.BigEndian.AppendUint32(nil, uint32(len(ihdr))))
	buf.Write(chunk)
	buf.Write(binary.BigEndian.AppendUint32(nil, crc32.ChecksumIEEE(chunk)))

	_, err := media.ProcessImage(buf.Bytes())
	if !errors.Is(err, media.ErrTooLarge) {
		t.Fatalf("ProcessImage = %v, want ErrTooLarge", err)
	}
}
