//go:build e2e

package e2e

import (
	"bytes"
	"compress/lzw"
	"encoding/binary"
	"net/http"
	"testing"
	"time"
)

func gifBomb(t *testing.T, side, frames int) []byte {
	t.Helper()
	le := binary.LittleEndian

	var compressed bytes.Buffer
	w := lzw.NewWriter(&compressed, lzw.LSB, 2)
	row := make([]byte, side)
	for range side {
		if _, err := w.Write(row); err != nil {
			t.Fatalf("lzw: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("lzw: %v", err)
	}
	var data bytes.Buffer
	for rest := compressed.Bytes(); len(rest) > 0; {
		n := min(len(rest), 255)
		data.WriteByte(byte(n))
		data.Write(rest[:n])
		rest = rest[n:]
	}
	data.WriteByte(0)

	var buf bytes.Buffer
	buf.WriteString("GIF89a")
	buf.Write(le.AppendUint16(le.AppendUint16(nil, uint16(side)), uint16(side)))
	buf.Write([]byte{0x80, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff})
	for range frames {
		buf.Write([]byte{0x2c, 0, 0, 0, 0})
		buf.Write(le.AppendUint16(le.AppendUint16(nil, uint16(side)), uint16(side)))
		buf.Write([]byte{0, 2})
		buf.Write(data.Bytes())
	}
	buf.WriteByte(0x3b)
	return buf.Bytes()
}

func TestAGIFBombIsRefusedQuickly(t *testing.T) {
	ada := newUser(t)
	bomb := gifBomb(t, 7000, 30)

	started := time.Now()
	if _, status := ada.upload(t, "media", "bomb.gif", bomb); status != http.StatusBadRequest {
		t.Fatalf("a %d KB GIF that decodes to %d MB: status = %d, want 400", len(bomb)>>10, 30*7000*7000>>20, status)
	}
	if took := time.Since(started); took > 10*time.Second {
		t.Errorf("refusing the bomb took %s", took)
	}

	if _, status := ada.upload(t, "media", "IMG_0003.jpg", photoWithLocation(t)); status != http.StatusOK {
		t.Fatalf("chat did not survive the bomb: photo upload status = %d", status)
	}
}
