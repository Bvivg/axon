package media

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	gifColorTableFlag = 0x80
	gifColorTableBits = 0x07

	gifExtension       = 0x21
	gifImageDescriptor = 0x2c
	gifTrailer         = 0x3b

	gifPlainText      = 0x01
	gifGraphicControl = 0xf9
	gifComment        = 0xfe
	gifApplication    = 0xff
)

var errGIFStructure = errors.New("malformed gif block stream")

type gifShape struct {
	width  int
	height int
	frames int
	pixels int64
}

func (s gifShape) animated() bool { return s.frames > 1 }

type gifScanner struct {
	raw []byte
	pos int
}

func (r *gifScanner) take(n int) ([]byte, bool) {
	if n < 0 || len(r.raw)-r.pos < n {
		return nil, false
	}
	out := r.raw[r.pos : r.pos+n]
	r.pos += n
	return out, true
}

func (r *gifScanner) byte() (byte, bool) {
	b, ok := r.take(1)
	if !ok {
		return 0, false
	}
	return b[0], true
}

func (r *gifScanner) colorTable(fields byte) bool {
	if fields&gifColorTableFlag == 0 {
		return true
	}
	_, ok := r.take(3 << (1 + uint(fields&gifColorTableBits)))
	return ok
}

func (r *gifScanner) subBlocks() bool {
	for {
		n, ok := r.byte()
		if !ok {
			return false
		}
		if n == 0 {
			return true
		}
		if _, ok := r.take(int(n)); !ok {
			return false
		}
	}
}

func (r *gifScanner) extension() bool {
	label, ok := r.byte()
	if !ok {
		return false
	}
	switch label {
	case gifPlainText:
		if _, ok := r.take(13); !ok {
			return false
		}
	case gifGraphicControl:
		block, ok := r.take(6)
		return ok && block[0] == 4 && block[5] == 0
	case gifApplication:
		size, ok := r.byte()
		if !ok {
			return false
		}
		if _, ok := r.take(int(size)); !ok {
			return false
		}
	case gifComment:
	default:
		return false
	}
	return r.subBlocks()
}

func scanGIF(raw []byte, limits ImageLimits) (gifShape, error) {
	r := gifScanner{raw: raw}
	header, ok := r.take(13)
	if !ok {
		return gifShape{}, fmt.Errorf("%w: %w", ErrUnsupported, errGIFStructure)
	}
	if v := string(header[:6]); v != "GIF87a" && v != "GIF89a" {
		return gifShape{}, fmt.Errorf("%w: %w", ErrUnsupported, errGIFStructure)
	}
	shape := gifShape{
		width:  int(binary.LittleEndian.Uint16(header[6:8])),
		height: int(binary.LittleEndian.Uint16(header[8:10])),
	}
	if !r.colorTable(header[10]) {
		return gifShape{}, fmt.Errorf("%w: %w", ErrUnsupported, errGIFStructure)
	}

	for {
		block, ok := r.byte()
		if !ok {
			return gifShape{}, fmt.Errorf("%w: %w", ErrUnsupported, errGIFStructure)
		}
		switch block {
		case gifExtension:
			ok = r.extension()
		case gifImageDescriptor:
			ok = r.frame(&shape)
			if ok {
				if err := limits.checkAnimation(shape); err != nil {
					return gifShape{}, err
				}
			}
		case gifTrailer:
			if shape.frames == 0 {
				return gifShape{}, fmt.Errorf("%w: %w", ErrUnsupported, errGIFStructure)
			}
			return shape, nil
		default:
			ok = false
		}
		if !ok {
			return gifShape{}, fmt.Errorf("%w: %w", ErrUnsupported, errGIFStructure)
		}
	}
}

func (r *gifScanner) frame(shape *gifShape) bool {
	d, ok := r.take(9)
	if !ok {
		return false
	}
	le := binary.LittleEndian
	left, top := int(le.Uint16(d[0:2])), int(le.Uint16(d[2:4]))
	width, height := int(le.Uint16(d[4:6])), int(le.Uint16(d[6:8]))
	if left+width > shape.width || top+height > shape.height {
		return false
	}
	if !r.colorTable(d[8]) {
		return false
	}
	if _, ok := r.byte(); !ok {
		return false
	}
	if !r.subBlocks() {
		return false
	}
	shape.frames++
	shape.pixels += int64(width) * int64(height)
	return true
}
