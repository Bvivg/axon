package attachment

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/andybalholm/brotli"
)

const (
	brotliProbeBytes   = 1 << 20
	brotliMinSavingPct = 10
	brotliQuality      = 9
	brotliWindowBits   = 24
)

func compressIfWorthIt(path string) (string, string, error) {
	worth, err := brotliPays(path)
	if err != nil || !worth {
		return path, "", err
	}

	in, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", "", fmt.Errorf("attachment: open for compression: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".br-*")
	if err != nil {
		return "", "", fmt.Errorf("attachment: create compressed file: %w", err)
	}
	w := brotli.NewWriterOptions(out, brotli.WriterOptions{Quality: brotliQuality, LGWin: brotliWindowBits})
	_, copyErr := io.Copy(w, in)
	closeErr := errors.Join(w.Close(), out.Close())
	if err := errors.Join(copyErr, closeErr); err != nil {
		_ = os.Remove(out.Name())
		return "", "", fmt.Errorf("attachment: compress: %w", err)
	}
	return out.Name(), "br", nil
}

func brotliPays(path string) (bool, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return false, fmt.Errorf("attachment: open for probing: %w", err)
	}
	defer func() { _ = f.Close() }()

	probe, err := io.ReadAll(io.LimitReader(f, brotliProbeBytes))
	if err != nil {
		return false, fmt.Errorf("attachment: read probe: %w", err)
	}
	if len(probe) == 0 {
		return false, nil
	}

	var packed bytes.Buffer
	w := brotli.NewWriterOptions(&packed, brotli.WriterOptions{Quality: brotliQuality, LGWin: brotliWindowBits})
	if _, err := w.Write(probe); err != nil {
		return false, fmt.Errorf("attachment: probe compression: %w", err)
	}
	if err := w.Close(); err != nil {
		return false, fmt.Errorf("attachment: probe compression: %w", err)
	}
	return packed.Len()*100 <= len(probe)*(100-brotliMinSavingPct), nil
}
