package rpclimit

import (
	"compress/gzip"
	"errors"
	"io"

	"connectrpc.com/connect"
)

const DefaultReadMaxBytes = 256 << 10

const gzipName = "gzip"

var ErrInflatedTooLarge = errors.New("decompressed message exceeds the read limit")

func Handler(readMaxBytes int) connect.HandlerOption {
	if readMaxBytes <= 0 {
		readMaxBytes = DefaultReadMaxBytes
	}
	budget := int64(readMaxBytes) + 1
	return connect.WithHandlerOptions(
		connect.WithReadMaxBytes(readMaxBytes),
		connect.WithCompression(gzipName,
			func() connect.Decompressor { return NewDecompressor(budget) },
			func() connect.Compressor { return gzip.NewWriter(io.Discard) },
		),
	)
}

type Decompressor struct {
	reader gzip.Reader
	budget int64
	left   int64
}

func NewDecompressor(budget int64) *Decompressor {
	return &Decompressor{budget: budget, left: budget}
}

func (d *Decompressor) Read(p []byte) (int, error) {
	if d.left <= 0 {
		return 0, ErrInflatedTooLarge
	}
	if int64(len(p)) > d.left {
		p = p[:d.left]
	}
	n, err := d.reader.Read(p)
	d.left -= int64(n)
	return n, err
}

func (d *Decompressor) Reset(r io.Reader) error {
	d.left = d.budget
	return d.reader.Reset(r)
}

func (d *Decompressor) Close() error {
	return d.reader.Close()
}
