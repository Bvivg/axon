//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	chatv1 "github.com/bvivg/axon/core/shared/gen/go/axon/chat/v1"
)

type uploaded struct {
	UploadID string          `json:"upload_id"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
}

func (u *user) upload(t *testing.T, as, filename string, body []byte) (uploaded, int) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, gatewayURL+"/api/attachment?as="+as, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("build upload: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Filename", url.PathEscape(filename))

	client := &http.Client{Timeout: 2 * time.Minute, Transport: forwardedFor{addr: "10.200.0.1"}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("upload %s: %v", filename, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return uploaded{}, resp.StatusCode
	}

	var out uploaded
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decode upload response %q: %v", raw, err)
	}
	return out, resp.StatusCode
}

func fetch(t *testing.T, rawURL string) ([]byte, http.Header) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("build fetch %s: %v", rawURL, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch %s: status %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", rawURL, err)
	}
	return body, resp.Header
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func photoWithLocation(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 64, 32))
	for y := range 32 {
		for x := range 64 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: 80, B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode photo: %v", err)
	}
	plain := buf.Bytes()

	le := binary.LittleEndian
	tiff := []byte{'I', 'I', 0x2A, 0x00}
	tiff = le.AppendUint32(tiff, 8)
	tiff = le.AppendUint16(tiff, 1)
	tiff = le.AppendUint16(tiff, 0x010E)
	tiff = le.AppendUint16(tiff, 2)
	secret := "home-at-55.7558N"
	tiff = le.AppendUint32(tiff, uint32(len(secret)+1))
	tiff = le.AppendUint32(tiff, 8+2+12+4)
	tiff = le.AppendUint32(tiff, 0)
	tiff = append(tiff, secret...)
	tiff = append(tiff, 0)

	body := append([]byte("Exif\x00\x00"), tiff...)
	segment := []byte{0xFF, 0xE1}
	segment = binary.BigEndian.AppendUint16(segment, uint16(len(body)+2))
	segment = append(segment, body...)

	out := append([]byte{}, plain[:2]...)
	out = append(out, segment...)
	return append(out, plain[2:]...)
}

func (s *socket) sendUpload(roomID, uploadID, caption string) outbound {
	s.t.Helper()

	s.send(inbound{Type: typeSend, RoomID: roomID, ClientID: uuid.NewString(), UploadID: uploadID, Body: caption})
	for {
		out := s.read()
		switch out.Type {
		case typeMessage:
			return out
		case typeError:
			s.t.Fatalf("sending upload %s was refused: %s", uploadID, out.Reason)
		}
	}
}

func TestAPhotoArrivesAsAnImageWithoutItsMetadata(t *testing.T) {
	ada := newUser(t)
	room := ada.createRoom(t, "photos-"+uuid.NewString()[:8])

	original := photoWithLocation(t)
	if !bytes.Contains(original, []byte("home-at-55.7558N")) {
		t.Fatal("the fixture lost its marker")
	}

	up, status := ada.upload(t, "media", "IMG_0001.jpg", original)
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}
	if up.Kind != "image" {
		t.Fatalf("kind = %q, want image", up.Kind)
	}

	var payload struct {
		URL          string `json:"url"`
		ThumbnailURL string `json:"thumbnail_url"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
	}
	if err := json.Unmarshal(up.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Width != 64 || payload.Height != 32 || payload.ThumbnailURL == "" {
		t.Errorf("payload = %+v, want a 64x32 photo with a thumbnail", payload)
	}

	stored, _ := fetch(t, payload.URL)
	if bytes.Contains(stored, []byte("home-at-55.7558N")) || bytes.Contains(stored, []byte("Exif")) {
		t.Error("the stored photo still carries its EXIF")
	}

	s := ada.connect(t)
	s.subscribe(room.GetId(), 0)
	frame := s.sendUpload(room.GetId(), up.UploadID, "the view")
	if frame.Message.Kind != "image" || frame.Message.Body != "the view" {
		t.Errorf("message = %+v, want an image with its caption", frame.Message)
	}

	history, err := ada.chat.ListMessages(context.Background(), connect.NewRequest(&chatv1.ListMessagesRequest{
		RoomId: room.GetId(),
		Kinds:  []chatv1.MessageKind{chatv1.MessageKind_MESSAGE_KIND_IMAGE, chatv1.MessageKind_MESSAGE_KIND_VIDEO},
	}))
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(history.Msg.GetMessages()) != 1 || history.Msg.GetMessages()[0].GetImage().GetWidth() != 64 {
		t.Fatalf("media history = %v, want the one photo", history.Msg.GetMessages())
	}
}

func TestAVideoIsReencodedAndLosesItsLocation(t *testing.T) {
	ada := newUser(t)

	up, status := ada.upload(t, "media", "clip.mov", fixture(t, "clip.mov"))
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}
	if up.Kind != "video" {
		t.Fatalf("kind = %q, want video", up.Kind)
	}

	var payload struct {
		URL        string `json:"url"`
		PosterURL  string `json:"poster_url"`
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		DurationMS int64  `json:"duration_ms"`
		Mime       string `json:"mime"`
	}
	if err := json.Unmarshal(up.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.Width != 320 || payload.Height != 180 || payload.Mime != "video/mp4" {
		t.Errorf("payload = %+v, want a 320x180 mp4", payload)
	}
	if payload.DurationMS < 900 || payload.DurationMS > 1200 {
		t.Errorf("duration = %dms, want about a second", payload.DurationMS)
	}

	video, headers := fetch(t, payload.URL)
	if ct := headers.Get("Content-Type"); ct != "video/mp4" {
		t.Errorf("content type = %q, want video/mp4", ct)
	}
	for _, leak := range []string{"55.7558", "holiday-secret"} {
		if bytes.Contains(video, []byte(leak)) {
			t.Errorf("the video still carries %q", leak)
		}
	}

	poster, _ := fetch(t, payload.PosterURL)
	if _, _, err := image.DecodeConfig(bytes.NewReader(poster)); err != nil {
		t.Errorf("the poster is not an image: %v", err)
	}
}

func TestAFileIsKeptByteForByte(t *testing.T) {
	ada := newUser(t)

	original := make([]byte, 96<<10)
	if _, err := rand.Read(original); err != nil {
		t.Fatalf("random bytes: %v", err)
	}

	up, status := ada.upload(t, "file", "отчёт за год.bin", original)
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}

	var payload struct {
		URL       string `json:"url"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := json.Unmarshal(up.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if up.Kind != "attachment" || payload.Filename != "отчёт за год.bin" || payload.SizeBytes != int64(len(original)) {
		t.Errorf("upload = %s %+v, want the file as sent", up.Kind, payload)
	}

	stored, headers := fetch(t, payload.URL)
	if !bytes.Equal(stored, original) {
		t.Error("the stored file differs from what was sent")
	}
	if cd := headers.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("content disposition = %q, want attachment", cd)
	}
}

func TestAVoiceMessageBecomesAAC(t *testing.T) {
	ada := newUser(t)

	up, status := ada.upload(t, "voice", "voice.webm", fixture(t, "voice.webm"))
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}

	var payload struct {
		URL        string `json:"url"`
		DurationMS int64  `json:"duration_ms"`
		Mime       string `json:"mime"`
	}
	if err := json.Unmarshal(up.Payload, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if up.Kind != "voice" || payload.Mime != "audio/mp4" {
		t.Errorf("upload = %s %+v, want an audio/mp4 voice message", up.Kind, payload)
	}
	if payload.DurationMS < 900 || payload.DurationMS > 1200 {
		t.Errorf("duration = %dms, want about a second", payload.DurationMS)
	}
}

func TestWhatCannotBeSentIsRefused(t *testing.T) {
	ada := newUser(t)
	grace := newUser(t)
	room := ada.createRoom(t, "guarded-"+uuid.NewString()[:8])
	grace.joinRoom(t, room.GetId())

	if _, status := ada.upload(t, "media", "notes.txt", []byte("just some words, not a picture")); status != http.StatusUnsupportedMediaType {
		t.Errorf("a text file as media: status = %d, want 415", status)
	}
	if _, status := ada.upload(t, "sticker", "x.bin", []byte("x")); status != http.StatusBadRequest {
		t.Errorf("an unknown mode: status = %d, want 400", status)
	}

	up, status := ada.upload(t, "media", "IMG_0002.jpg", photoWithLocation(t))
	if status != http.StatusOK {
		t.Fatalf("upload status = %d, want 200", status)
	}

	s := grace.connect(t)
	s.subscribe(room.GetId(), 0)

	s.send(inbound{Type: typeSend, RoomID: room.GetId(), ClientID: uuid.NewString(), UploadID: up.UploadID})
	if out := s.expect(typeError); !strings.Contains(out.Reason, "upload") {
		t.Errorf("someone else's upload: reason = %q", out.Reason)
	}

	s.send(inbound{
		Type: typeSend, RoomID: room.GetId(), ClientID: uuid.NewString(), Kind: "image",
		Payload: json.RawMessage(`{"url":"https://tracker.example/pixel.gif","width":1,"height":1,"size_bytes":1}`),
	})
	if out := s.expect(typeError); !strings.Contains(out.Reason, "payload") {
		t.Errorf("a client payload: reason = %q", out.Reason)
	}
}
