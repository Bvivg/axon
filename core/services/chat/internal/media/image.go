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

	imageQuality     = 85
	thumbnailQuality = 75
)

var (
	ErrUnsupported       = errors.New("media: unsupported format")
	ErrTooLarge          = errors.New("media: too large")
	ErrAnimationTooLarge = errors.New("media: the animation has too many frames or pixels")
)

type ImageLimits struct {
	MaxPixels          int64
	MaxFrames          int
	MaxAnimationPixels int64
	MaxAnimationScreen int64
}

func DefaultImageLimits() ImageLimits {
	return ImageLimits{
		MaxPixels:          50_000_000,
		MaxFrames:          1000,
		MaxAnimationPixels: 300_000_000,
		MaxAnimationScreen: 16_000_000,
	}
}

func (l ImageLimits) withDefaults() ImageLimits {
	d := DefaultImageLimits()
	if l.MaxPixels <= 0 {
		l.MaxPixels = d.MaxPixels
	}
	if l.MaxFrames <= 0 {
		l.MaxFrames = d.MaxFrames
	}
	if l.MaxAnimationPixels <= 0 {
		l.MaxAnimationPixels = d.MaxAnimationPixels
	}
	if l.MaxAnimationScreen <= 0 {
		l.MaxAnimationScreen = d.MaxAnimationScreen
	}
	return l
}

func (l ImageLimits) checkAnimation(s gifShape) error {
	if !s.animated() {
		return nil
	}
	switch {
	case int64(s.width)*int64(s.height) > l.MaxAnimationScreen:
		return fmt.Errorf("%w: a %dx%d screen", ErrAnimationTooLarge, s.width, s.height)
	case s.frames > l.MaxFrames:
		return fmt.Errorf("%w: more than %d frames", ErrAnimationTooLarge, l.MaxFrames)
	case s.pixels > l.MaxAnimationPixels:
		return fmt.Errorf("%w: more than %d decoded pixels", ErrAnimationTooLarge, l.MaxAnimationPixels)
	}
	return nil
}

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

func ProcessImage(raw []byte, limits ImageLimits) (Image, error) {
	limits = limits.withDefaults()
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
	if int64(cfg.Width)*int64(cfg.Height) > limits.MaxPixels {
		return Image{}, ErrTooLarge
	}

	if mime == "image/gif" {
		shape, err := scanGIF(raw, limits)
		if err != nil {
			return Image{}, err
		}
		if shape.animated() {
			return processAnimatedGIF(raw)
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

func processAnimatedGIF(raw []byte) (Image, error) {
	g, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return Image{}, fmt.Errorf("%w: %w", ErrUnsupported, err)
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
		return Image{}, fmt.Errorf("media: encode gif: %w", err)
	}

	thumb, thumbMime, err := encode(fitThumbnail(g.Image[0]), false, thumbnailQuality)
	if err != nil {
		return Image{}, err
	}

	return Image{
		Data:          buf.Bytes(),
		Mime:          "image/gif",
		Width:         g.Config.Width,
		Height:        g.Config.Height,
		Thumbnail:     thumb,
		ThumbnailMime: thumbMime,
	}, nil
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
