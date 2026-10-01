//go:build e2e

package e2e

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/andybalholm/brotli"
)

type uploadSession struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	PartSize  int64     `json:"part_size"`
	PartCount int       `json:"part_count"`
	Parts     []int     `json:"parts"`
	Failure   string    `json:"failure"`
	Upload    *uploaded `json:"upload"`
}

func (u *user) sessionCall(t *testing.T, method, path string, body io.Reader, out any) int {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, gatewayURL+"/api/uploads"+path, body)
	if err != nil {
		t.Fatalf("build %s %s: %v", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/octet-stream")
	}

	client := &http.Client{Timeout: 2 * time.Minute, Transport: forwardedFor{addr: "10.200.0.2"}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode < 300 && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s %s %q: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}

func (u *user) startUpload(t *testing.T, as, name string, size int) uploadSession {
	t.Helper()

	var s uploadSession
	start, _ := json.Marshal(map[string]any{"as": as, "name": name, "size": size})
	if code := u.sessionCall(t, http.MethodPost, "", bytes.NewReader(start), &s); code != http.StatusCreated {
		t.Fatalf("start upload: status %d", code)
	}
	return s
}

func (u *user) putPart(t *testing.T, s uploadSession, body []byte, n int) {
	t.Helper()

	from := int64(n-1) * s.PartSize
	to := min(from+s.PartSize, int64(len(body)))
	if code := u.sessionCall(t, http.MethodPut, fmt.Sprintf("/%s/parts/%d", s.ID, n), bytes.NewReader(body[from:to]), nil); code != http.StatusNoContent {
		t.Fatalf("part %d: status %d", n, code)
	}
}

func (u *user) finishUpload(t *testing.T, s uploadSession) uploaded {
	t.Helper()

	var state uploadSession
	if code := u.sessionCall(t, http.MethodPost, "/"+s.ID+"/complete", nil, &state); code != http.StatusAccepted {
		t.Fatalf("complete: status %d", code)
	}
	for state.Status == "processing" {
		if code := u.sessionCall(t, http.MethodGet, "/"+s.ID+"?wait=1", nil, &state); code != http.StatusOK {
			t.Fatalf("status: %d", code)
		}
	}
	if state.Status != "ready" || state.Upload == nil {
		t.Fatalf("upload ended as %q (%s)", state.Status, state.Failure)
	}
	return *state.Upload
}

func (u *user) uploadInParts(t *testing.T, as, name string, body []byte) uploaded {
	t.Helper()

	s := u.startUpload(t, as, name, len(body))
	for n := 1; n <= s.PartCount; n++ {
		u.putPart(t, s, body, n)
	}
	return u.finishUpload(t, s)
}

func TestABigFileGoesUpInPartsResumesAfterADropAndReachesThePeer(t *testing.T) {
	alice, bob := newUser(t), newUser(t)
	room := alice.createRoom(t, "e2e parts")
	bob.joinRoom(t, room.GetId())

	body := make([]byte, 25<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}

	s := alice.startUpload(t, "file", "dump.bin", len(body))
	if s.PartCount != 3 {
		t.Fatalf("part count = %d, want 25 MB in three parts", s.PartCount)
	}
	alice.putPart(t, s, body, 1)

	var after uploadSession
	if code := alice.sessionCall(t, http.MethodGet, "/"+s.ID, nil, &after); code != http.StatusOK || len(after.Parts) != 1 || after.Parts[0] != 1 {
		t.Fatalf("after the drop: %d %+v, want part 1 kept", code, after)
	}
	if code := alice.sessionCall(t, http.MethodPost, "/"+s.ID+"/complete", nil, nil); code != http.StatusConflict {
		t.Fatalf("completing with parts missing: status %d, want 409", code)
	}

	alice.putPart(t, s, body, 3)
	alice.putPart(t, s, body, 2)
	up := alice.finishUpload(t, s)

	as, bs := alice.connect(t), bob.connect(t)
	as.subscribe(room.GetId(), 0)
	bs.subscribe(room.GetId(), 0)
	as.sendUpload(room.GetId(), up.UploadID, "the dump")
	got := bs.expect(typeMessage).Message

	var payload struct {
		URL       string `json:"url"`
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.Filename != "dump.bin" || payload.SizeBytes != int64(len(body)) {
		t.Fatalf("bob got %+v, want the 25 MB dump", payload)
	}
	served, _ := fetch(t, payload.URL)
	if !bytes.Equal(served, body) {
		t.Fatalf("served %d bytes that differ from the %d sent", len(served), len(body))
	}
}

func TestATextDocumentIsStoredWithBrotli(t *testing.T) {
	alice := newUser(t)
	text := []byte(strings.Repeat("date,amount,comment\n2026-10-01,42,ok\n", 60000))

	up := alice.uploadInParts(t, "file", "report.csv", text)
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(up.Payload, &payload); err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, payload.URL, nil)
	req.Header.Set("Accept-Encoding", "br")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.Header.Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding = %q, want br", resp.Header.Get("Content-Encoding"))
	}
	packed, _ := io.ReadAll(resp.Body)
	unpacked, err := io.ReadAll(brotli.NewReader(bytes.NewReader(packed)))
	if err != nil || !bytes.Equal(unpacked, text) {
		t.Fatalf("the document does not unpack to what was sent: %v", err)
	}
	if len(packed) >= len(text)/2 {
		t.Errorf("stored %d bytes for %d of text", len(packed), len(text))
	}
}

func TestTheSamePhotoTwiceIsStoredOnce(t *testing.T) {
	alice, bob := newUser(t), newUser(t)
	photo := photoWithLocation(t)

	first := alice.uploadInParts(t, "media", "a.jpg", photo)
	second := bob.uploadInParts(t, "media", "b.jpg", photo)

	var a, b struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(first.Payload, &a)
	_ = json.Unmarshal(second.Payload, &b)
	if a.URL == "" || a.URL != b.URL || first.UploadID == second.UploadID {
		t.Fatalf("two uploads of one photo: %q and %q (%s, %s), want two uploads of one stored file", a.URL, b.URL, first.UploadID, second.UploadID)
	}
}

func TestAWithdrawnDraftIsGone(t *testing.T) {
	alice := newUser(t)
	body := make([]byte, 1<<20)

	s := alice.startUpload(t, "file", "draft.bin", len(body))
	alice.putPart(t, s, body, 1)
	if code := alice.sessionCall(t, http.MethodDelete, "/"+s.ID, nil, nil); code != http.StatusNoContent {
		t.Fatalf("withdraw: status %d", code)
	}
	if code := alice.sessionCall(t, http.MethodGet, "/"+s.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("after withdrawing: status %d, want 404", code)
	}

	bob := newUser(t)
	other := alice.startUpload(t, "file", "mine.bin", len(body))
	if code := bob.sessionCall(t, http.MethodGet, "/"+other.ID, nil, nil); code != http.StatusNotFound {
		t.Fatalf("someone else's upload: status %d, want 404", code)
	}
}
