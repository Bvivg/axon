package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const (
	MaxImageSide  = 2560
	ThumbnailSide = 480

	maxImagePixels = 50_000_000

	imageQuality     = 85
	thumbnailQuality = 75
)

var (
	ErrUnsupported = errors.New("media: unsupported format")
	ErrTooLarge    = errors.New("media: too large")
)

type Image struct {
	Data   []byte
	Mime   string
	Width  int
	Height int

	Thumbnail     []byte
	ThumbnailMime string
}

func IsImageMime(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}

func Sniff(head []byte) string {
	return http.DetectContentType(head)
}

func ProcessImage(raw []byte) (Image, error) {
	mime := Sniff(raw)
	if !IsImageMime(mime) {
		return Image{}, ErrUnsupported
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return Image{}, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Image{}, ErrUnsupported
	}
	if cfg.Width*cfg.Height > maxImagePixels {
		return Image{}, ErrTooLarge
	}

	if mime == "image/gif" {
		animated, ok, err := processAnimatedGIF(raw)
		if err != nil {
			return Image{}, err
		}
		if ok {
			return animated, nil
		}
	}

	src, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return Image{}, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}

	main := src
	if b := main.Bounds(); b.Dx() > MaxImageSide || b.Dy() > MaxImageSide {
		main = imaging.Fit(main, MaxImageSide, MaxImageSide, imaging.Lanczos)
	}

	opaque := isOpaque(src)

	data, outMime, err := encode(main, opaque, imageQuality)
	if err != nil {
		return Image{}, err
	}

	thumb, thumbMime, err := encode(fitThumbnail(src), opaque, thumbnailQuality)
	if err != nil {
		return Image{}, err
	}

	b := main.Bounds()
	return Image{
		Data:          data,
		Mime:          outMime,
		Width:         b.Dx(),
		Height:        b.Dy(),
		Thumbnail:     thumb,
		ThumbnailMime: thumbMime,
	}, nil
}

func processAnimatedGIF(raw []byte) (Image, bool, error) {
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return Image{}, false, fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	if len(g.Image) < 2 {
		return Image{}, false, nil
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image:           g.Image,
		Delay:           g.Delay,
		LoopCount:       g.LoopCount,
		Disposal:        g.Disposal,
		Config:          g.Config,
		BackgroundIndex: g.BackgroundIndex,
	}); err != nil {
		return Image{}, false, fmt.Errorf("media: encode gif: %w", err)
	}

	thumb, thumbMime, err := encode(fitThumbnail(g.Image[0]), false, thumbnailQuality)
	if err != nil {
		return Image{}, false, err
	}

	return Image{
		Data:          buf.Bytes(),
		Mime:          "image/gif",
		Width:         g.Config.Width,
		Height:        g.Config.Height,
		Thumbnail:     thumb,
		ThumbnailMime: thumbMime,
	}, true, nil
}

func fitThumbnail(img image.Image) image.Image {
	if b := img.Bounds(); b.Dx() <= ThumbnailSide && b.Dy() <= ThumbnailSide {
		return img
	}
	return imaging.Fit(img, ThumbnailSide, ThumbnailSide, imaging.Lanczos)
}

func encode(img image.Image, opaque bool, quality int) ([]byte, string, error) {
	var buf bytes.Buffer
	if opaque {
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", fmt.Errorf("media: encode jpeg: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil
	}

	encoder := png.Encoder{CompressionLevel: png.BestCompression}
	if err := encoder.Encode(&buf, img); err != nil {
		return nil, "", fmt.Errorf("media: encode png: %w", err)
	}
	return buf.Bytes(), "image/png", nil
}

func isOpaque(img image.Image) bool {
	if o, ok := img.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return true
}
