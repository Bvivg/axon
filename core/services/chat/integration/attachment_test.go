//go:build integration

package integration

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
)

type fetched struct {
	status      int
	contentType string
	disposition string
	body        []byte
}

func fetchAttachment(t *testing.T, url string) fetched {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", url, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body of %s: %v", url, err)
	}
	return fetched{
		status:      resp.StatusCode,
		contentType: resp.Header.Get("Content-Type"),
		disposition: resp.Header.Get("Content-Disposition"),
		body:        data,
	}
}

func TestStorePutMakesTheObjectPubliclyFetchable(t *testing.T) {
	userID := uuid.New()
	key := attachment.NewObjectKey(userID, "recording.webm")
	content := []byte("pretend this is an audio recording")

	if err := attachmentStore.Put(t.Context(), attachment.Object{Key: key, ContentType: "audio/webm", Data: content}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	url := attachmentURLs.URL(key)
	got := fetchAttachment(t, url)

	if got.status != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, got.status)
	}
	if !bytes.Equal(got.body, content) {
		t.Errorf("downloaded content = %q, want %q", got.body, content)
	}
	if got.contentType != "audio/webm" {
		t.Errorf("Content-Type = %q, want audio/webm", got.contentType)
	}
}

func TestNewObjectKeyIsCollisionResistantAndIgnoresPathTraversal(t *testing.T) {
	userID := uuid.New()

	first := attachment.NewObjectKey(userID, "invoice.pdf")
	second := attachment.NewObjectKey(userID, "invoice.pdf")
	if first == second {
		t.Fatalf("two uploads of the same filename produced the same key: %s", first)
	}

	traversal := attachment.NewObjectKey(userID, "../../etc/passwd")
	if bytes.Contains([]byte(traversal), []byte("..")) {
		t.Errorf("object key leaked path traversal from the filename: %s", traversal)
	}
}

func TestStorePutRejectsUnknownBucket(t *testing.T) {
	badStore, err := attachment.NewStore(attachment.StoreConfig{
		Endpoint:  attachmentPublicURL[len("http://"):],
		AccessKey: minioUser,
		SecretKey: minioPassword,
		Bucket:    "does-not-exist",
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	key := attachment.NewObjectKey(uuid.New(), "file.bin")
	if err := badStore.Put(t.Context(), attachment.Object{Key: key, ContentType: "application/octet-stream", Data: []byte("x")}); err == nil {
		t.Fatal("Put into a nonexistent bucket was accepted")
	}
}

func TestAFileIsServedAsADownloadUnderItsOwnName(t *testing.T) {
	key := attachment.NewObjectKey(uuid.New(), "page.html")
	content := []byte("<script>alert(document.domain)</script>")

	if err := attachmentStore.Put(t.Context(), attachment.Object{
		Key:         key,
		ContentType: "text/html; charset=utf-8",
		Disposition: `attachment; filename="page.html"`,
		Data:        content,
	}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got := fetchAttachment(t, attachmentURLs.URL(key))
	if got.disposition != `attachment; filename="page.html"` {
		t.Errorf("Content-Disposition = %q, want the download header", got.disposition)
	}
}
