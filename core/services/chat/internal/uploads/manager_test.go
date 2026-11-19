package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/attachment"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/media"
)

const testPart = minPartSize

type fakeSessions struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]domain.UploadSession
	uploads  map[uuid.UUID]domain.Upload
	files    map[uuid.UUID]domain.StoredFile
	sources  map[string]uuid.UUID
	inUse    map[uuid.UUID]bool
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{
		sessions: map[uuid.UUID]domain.UploadSession{},
		uploads:  map[uuid.UUID]domain.Upload{},
		files:    map[uuid.UUID]domain.StoredFile{},
		sources:  map[string]uuid.UUID{},
		inUse:    map[uuid.UUID]bool{},
	}
}

func (f *fakeSessions) CreateSession(_ context.Context, s domain.UploadSession) (domain.UploadSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s.Status = domain.UploadUploading
	f.sessions[s.ID] = s
	return s, nil
}

func (f *fakeSessions) ReuseSession(_ context.Context, s domain.UploadSession) (domain.UploadSession, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.sources[sourceKey(s.Mode, s.ExpectedSHA256)]
	if !ok {
		return domain.UploadSession{}, false, nil
	}
	for _, old := range f.sessions {
		if old.UploadID == nil || old.UploaderID != s.UploaderID || old.SizeBytes != s.SizeBytes {
			continue
		}
		upload := f.uploads[*old.UploadID]
		if upload.FileID == nil || *upload.FileID != id {
			continue
		}
		file := f.files[id]
		payload, err := file.PayloadFor(s.Filename)
		if err != nil {
			return domain.UploadSession{}, false, err
		}
		next := domain.Upload{ID: uuid.New(), UploaderID: s.UploaderID, Kind: file.Kind, FileID: &file.ID, Payload: payload}
		f.uploads[next.ID] = next
		s.Status, s.UploadID = domain.UploadReady, &next.ID
		f.sessions[s.ID] = s
		return s, true, nil
	}
	return domain.UploadSession{}, false, nil
}

func (f *fakeSessions) SessionByID(_ context.Context, id uuid.UUID) (domain.UploadSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return domain.UploadSession{}, domain.ErrSessionNotFound
	}
	return s, nil
}

func (f *fakeSessions) setStatus(id uuid.UUID, from []domain.UploadStatus, to domain.UploadStatus, edit func(*domain.UploadSession)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok {
		return domain.ErrSessionState
	}
	for _, status := range from {
		if s.Status == status {
			s.Status = to
			if edit != nil {
				edit(&s)
			}
			f.sessions[id] = s
			return nil
		}
	}
	return domain.ErrSessionState
}

func (f *fakeSessions) MarkSessionProcessing(_ context.Context, id uuid.UUID) error {
	return f.setStatus(id, []domain.UploadStatus{domain.UploadUploading}, domain.UploadProcessing, nil)
}

func (f *fakeSessions) ClaimSession(_ context.Context, id uuid.UUID, _ time.Duration) (domain.UploadSession, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[id]
	if !ok || s.Status != domain.UploadProcessing {
		return domain.UploadSession{}, false, nil
	}
	return s, true, nil
}

func (f *fakeSessions) ClaimableSessions(context.Context, time.Duration, int) ([]uuid.UUID, error) {
	return nil, nil
}

func (f *fakeSessions) FinishSession(_ context.Context, id, uploadID uuid.UUID) error {
	return f.setStatus(id, []domain.UploadStatus{domain.UploadProcessing}, domain.UploadReady, func(s *domain.UploadSession) {
		s.UploadID = &uploadID
	})
}

func (f *fakeSessions) FailSession(_ context.Context, id uuid.UUID, reason string) error {
	return f.setStatus(id, []domain.UploadStatus{domain.UploadUploading, domain.UploadProcessing}, domain.UploadFailed,
		func(s *domain.UploadSession) { s.Failure = reason })
}

func (f *fakeSessions) DeleteSession(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	return nil
}

func (f *fakeSessions) DeleteDraftUpload(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inUse[id] {
		return domain.ErrUploadInUse
	}
	delete(f.uploads, id)
	return nil
}

func (f *fakeSessions) CreateUpload(_ context.Context, u domain.Upload) (domain.Upload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[u.ID] = u
	return u, nil
}

func (f *fakeSessions) UploadByID(_ context.Context, id uuid.UUID) (domain.Upload, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.uploads[id]
	if !ok {
		return domain.Upload{}, domain.ErrUploadNotFound
	}
	return u, nil
}

func sourceKey(mode domain.UploadMode, sum []byte) string { return string(mode) + string(sum) }

func (f *fakeSessions) FileBySource(_ context.Context, mode domain.UploadMode, source []byte) (domain.StoredFile, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.sources[sourceKey(mode, source)]
	if !ok {
		return domain.StoredFile{}, false, nil
	}
	return f.files[id], true, nil
}

func (f *fakeSessions) SaveFile(_ context.Context, file domain.StoredFile, mode domain.UploadMode, source []byte, _ int64) (domain.StoredFile, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.files {
		if existing.Kind == file.Kind && bytes.Equal(existing.SHA256, file.SHA256) {
			f.sources[sourceKey(mode, source)] = existing.ID
			return existing, false, nil
		}
	}
	f.files[file.ID] = file
	f.sources[sourceKey(mode, source)] = file.ID
	return file, true, nil
}

type fakeObjects struct {
	mu       sync.Mutex
	parts    map[string]map[int][]byte
	objects  map[string][]byte
	aborted  []string
	complete map[string]string
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{parts: map[string]map[int][]byte{}, objects: map[string][]byte{}, complete: map[string]string{}}
}

func (o *fakeObjects) StartMultipart(_ context.Context, key string) (string, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	id := "mp-" + key
	o.parts[id] = map[int][]byte{}
	return id, nil
}

func (o *fakeObjects) PutPart(_ context.Context, _, uploadID string, number int, data io.Reader, _ int64) error {
	raw, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	parts, ok := o.parts[uploadID]
	if !ok {
		return attachment.ErrNoSuchUpload
	}
	parts[number] = raw
	return nil
}

func (o *fakeObjects) ListParts(_ context.Context, _, uploadID string) ([]attachment.Part, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	parts, ok := o.parts[uploadID]
	if !ok {
		return nil, attachment.ErrNoSuchUpload
	}
	var out []attachment.Part
	for n, raw := range parts {
		out = append(out, attachment.Part{Number: n, Size: int64(len(raw)), ETag: "e"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (o *fakeObjects) CompleteMultipart(_ context.Context, key, uploadID string, parts []attachment.Part) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	stored, ok := o.parts[uploadID]
	if !ok {
		return attachment.ErrNoSuchUpload
	}
	var whole []byte
	for _, part := range parts {
		whole = append(whole, stored[part.Number]...)
	}
	o.objects[key] = whole
	delete(o.parts, uploadID)
	return nil
}

func (o *fakeObjects) AbortMultipart(_ context.Context, _, uploadID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.aborted = append(o.aborted, uploadID)
	delete(o.parts, uploadID)
	return nil
}

func (o *fakeObjects) Download(_ context.Context, key, path string) error {
	o.mu.Lock()
	raw, ok := o.objects[key]
	o.mu.Unlock()
	if !ok {
		return errors.New("no such object")
	}
	return os.WriteFile(path, raw, 0o600)
}

func (o *fakeObjects) Remove(_ context.Context, keys []string) map[string]error {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, key := range keys {
		delete(o.objects, key)
	}
	return map[string]error{}
}

func (o *fakeObjects) put(key string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.objects[key] = []byte("stored")
}

func (o *fakeObjects) has(key string) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, ok := o.objects[key]
	return ok
}

type fakeProcessor struct {
	mu      sync.Mutex
	calls   int
	objects *fakeObjects
	fail    error
	digest  func(raw []byte) []byte
}

func (p *fakeProcessor) run(path string) (attachment.Result, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.mu.Unlock()
	if p.fail != nil {
		return attachment.Result{}, p.fail
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return attachment.Result{}, err
	}
	sum := sha256.Sum256(raw)
	digest := sum[:]
	if p.digest != nil {
		digest = p.digest(raw)
	}
	key := "files/" + string(rune('a'+call))
	p.objects.put(key)
	payload, _ := json.Marshal(map[string]string{"url": key})
	return attachment.Result{Kind: domain.MessageKindImage, Payload: payload, Keys: []string{key}, SHA256: digest}, nil
}

func (p *fakeProcessor) Media(_ context.Context, _ uuid.UUID, path string, _ int64) (attachment.Result, error) {
	return p.run(path)
}

func (p *fakeProcessor) Voice(_ context.Context, _ uuid.UUID, path string, _ int64) (attachment.Result, error) {
	return p.run(path)
}

func (p *fakeProcessor) File(_ context.Context, _ uuid.UUID, path, filename string, size int64) (attachment.Result, error) {
	result, err := p.run(path)
	if err != nil {
		return result, err
	}
	result.Kind = domain.MessageKindAttachment
	result.Payload, _ = json.Marshal(domain.AttachmentPayload{URL: keysOf(result.Payload)[0], Filename: filename, SizeBytes: size})
	result.Size = size
	return result, nil
}

func keysOf(payload json.RawMessage) []string {
	var fields struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(payload, &fields); err != nil || fields.URL == "" {
		return nil
	}
	return []string{fields.URL}
}

type harness struct {
	m         *Manager
	sessions  *fakeSessions
	objects   *fakeObjects
	processor *fakeProcessor
}

func newHarness(t *testing.T) harness {
	t.Helper()
	objects := newFakeObjects()
	h := harness{sessions: newFakeSessions(), objects: objects, processor: &fakeProcessor{objects: objects}}
	m, err := New(Config{
		Sessions:      h.sessions,
		Objects:       objects,
		Processor:     h.processor,
		KeysOf:        keysOf,
		Logger:        slog.New(slog.DiscardHandler),
		MaxBytes:      4 * testPart,
		MaxVoiceBytes: testPart,
		PartSize:      testPart,
		TempDir:       t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.m = m
	return h
}

func content(size int, seed byte) []byte {
	raw := make([]byte, size)
	for i := range raw {
		raw[i] = seed + byte(i%7)
	}
	return raw
}

func (h harness) upload(t *testing.T, user uuid.UUID, raw []byte, order ...int) domain.UploadSession {
	t.Helper()
	s, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "photo.jpg", int64(len(raw)), nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(order) == 0 {
		for n := 1; n <= s.PartCount; n++ {
			order = append(order, n)
		}
	}
	for _, n := range order {
		start := int64(n-1) * s.PartSize
		part := raw[start : start+s.PartBytes(n)]
		if err := h.m.PutPart(t.Context(), user, s.ID, n, bytes.NewReader(part), int64(len(part))); err != nil {
			t.Fatalf("PutPart(%d): %v", n, err)
		}
	}
	return s
}

func (h harness) finish(t *testing.T, user uuid.UUID, s domain.UploadSession) View {
	t.Helper()
	if _, err := h.m.Complete(t.Context(), user, s.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	h.m.process(t.Context(), <-h.m.queue)
	view, err := h.m.Status(t.Context(), user, s.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	return view
}

func TestPartsArriveInAnyOrderAndBecomeAnUpload(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()
	raw := content(2*testPart+123, 1)

	s := h.upload(t, user, raw, 3, 1, 2)
	if s.PartCount != 3 {
		t.Fatalf("part count = %d, want 3", s.PartCount)
	}

	view := h.finish(t, user, s)
	if view.Session.Status != domain.UploadReady || view.Upload == nil || view.Upload.FileID == nil {
		t.Fatalf("view = %+v, want a ready upload backed by a file", view)
	}
	if h.objects.has(s.StagingKey) {
		t.Error("the staged object outlived processing")
	}
}

func TestCompleteRefusesMissingOrShortParts(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()
	raw := content(2*testPart, 2)

	s := h.upload(t, user, raw, 1)
	if _, err := h.m.Complete(t.Context(), user, s.ID); !errors.Is(err, domain.ErrPartsMissing) {
		t.Fatalf("Complete with a missing part: err = %v, want ErrPartsMissing", err)
	}

	tests := map[string]struct {
		number int
		size   int64
		want   error
	}{
		"a part shorter than agreed": {number: 2, size: testPart - 1, want: ErrPartSize},
		"a part number past the end": {number: 3, size: testPart, want: ErrBadPart},
		"part number zero":           {number: 0, size: testPart, want: ErrBadPart},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := h.m.PutPart(t.Context(), user, s.ID, tt.number, bytes.NewReader(make([]byte, tt.size)), tt.size)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestLimitsAndOwnership(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()

	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "big", 4*testPart+1, nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a media file over the limit: err = %v, want ErrTooLarge", err)
	}
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeVoice, "long", testPart+1, nil); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a voice note over its own limit: err = %v, want ErrTooLarge", err)
	}
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "empty", 0, nil); !errors.Is(err, ErrEmpty) {
		t.Errorf("an empty file: err = %v, want ErrEmpty", err)
	}

	s := h.upload(t, user, content(100, 3))
	if _, err := h.m.Status(t.Context(), uuid.New(), s.ID); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Errorf("someone else's upload: err = %v, want ErrSessionNotFound", err)
	}
}

func TestTheSameSourceIsProcessedOnceAndTheSameResultStoredOnce(t *testing.T) {
	h := newHarness(t)
	alice, bob := uuid.New(), uuid.New()
	raw := content(1000, 4)

	first := h.finish(t, alice, h.upload(t, alice, raw))
	second := h.finish(t, bob, h.upload(t, bob, raw))
	if h.processor.calls != 1 {
		t.Fatalf("processed %d times, want once for the same bytes", h.processor.calls)
	}
	if *first.Upload.FileID != *second.Upload.FileID || first.Upload.ID == second.Upload.ID {
		t.Fatalf("uploads %+v and %+v, want two uploads of one file", first.Upload, second.Upload)
	}

	h.processor.digest = func([]byte) []byte { return []byte("same-result") }
	third := h.finish(t, alice, h.upload(t, alice, content(1000, 5)))
	fourth := h.finish(t, alice, h.upload(t, alice, content(1000, 6)))
	if *third.Upload.FileID != *fourth.Upload.FileID {
		t.Fatal("two sources with the same result became two files")
	}
	if h.objects.has("files/d") {
		t.Error("the duplicate result's objects were kept")
	}
}

func TestWithdrawingAnUpload(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()

	uploading := h.upload(t, user, content(testPart+1, 7), 1)
	if err := h.m.Delete(t.Context(), user, uploading.ID); err != nil {
		t.Fatalf("Delete while uploading: %v", err)
	}
	if len(h.objects.aborted) != 1 {
		t.Errorf("aborted %v, want the multipart upload aborted", h.objects.aborted)
	}

	ready := h.finish(t, user, h.upload(t, user, content(10, 8)))
	h.sessions.inUse[ready.Upload.ID] = true
	if err := h.m.Delete(t.Context(), user, ready.Session.ID); !errors.Is(err, domain.ErrUploadInUse) {
		t.Fatalf("Delete of a sent upload: err = %v, want ErrUploadInUse", err)
	}
	h.sessions.inUse[ready.Upload.ID] = false
	if err := h.m.Delete(t.Context(), user, ready.Session.ID); err != nil {
		t.Fatalf("Delete of a draft: %v", err)
	}
	if _, err := h.sessions.UploadByID(t.Context(), ready.Upload.ID); !errors.Is(err, domain.ErrUploadNotFound) {
		t.Errorf("the draft upload survived: err = %v", err)
	}

	processing := h.upload(t, user, content(10, 9))
	if _, err := h.m.Complete(t.Context(), user, processing.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	id := <-h.m.queue
	if err := h.m.Delete(t.Context(), user, processing.ID); err != nil {
		t.Fatalf("Delete while processing: %v", err)
	}
	before := len(h.sessions.uploads)
	h.m.process(t.Context(), id)
	if len(h.sessions.uploads) != before {
		t.Error("an upload withdrawn mid-processing left a draft behind")
	}
}

func TestProcessingFailuresAreReportedAndCleanedUp(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()
	h.processor.fail = media.ErrUnsupported

	s := h.upload(t, user, content(10, 10))
	view := h.finish(t, user, s)
	if view.Session.Status != domain.UploadFailed || view.Session.Failure != "unsupported" {
		t.Fatalf("view = %+v, want a failed upload saying it is unsupported", view.Session)
	}
	if h.objects.has(s.StagingKey) {
		t.Error("a failed upload left its staged object behind")
	}
}

func TestCompleteTwiceIsHarmless(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()
	s := h.upload(t, user, content(10, 11))

	if _, err := h.m.Complete(t.Context(), user, s.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	again, err := h.m.Complete(t.Context(), user, s.ID)
	if err != nil || again.Status != domain.UploadProcessing {
		t.Fatalf("second Complete = %+v, %v; want the processing upload", again, err)
	}
}

func TestWithdrawingADocumentDraftKeepsTheSharedFileForTheSweeper(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()

	s, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "notes.txt", 10, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := h.m.PutPart(t.Context(), user, s.ID, 1, bytes.NewReader(content(10, 12)), 10); err != nil {
		t.Fatalf("PutPart: %v", err)
	}
	view := h.finish(t, user, s)
	if view.Upload == nil || view.Upload.FileID == nil {
		t.Fatalf("view = %+v, want a document upload backed by a shared file", view)
	}
	key := keysOf(view.Upload.Payload)[0]
	if !h.objects.has(key) {
		t.Fatalf("the document object %s was never stored", key)
	}

	if err := h.m.Delete(t.Context(), user, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found := h.sessions.uploads[view.Upload.ID]; found {
		t.Error("the withdrawn draft stayed in the database")
	}
	if !h.objects.has(key) {
		t.Error("a shared file was removed before the sweeper checked its references")
	}
}

func TestHashPreflightReusesTheFileBeforeReceivingParts(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()
	raw := content(100, 14)
	sum := sha256.Sum256(raw)
	first, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "one.txt", int64(len(raw)), sum[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.PutPart(t.Context(), user, first.ID, 1, bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	view := h.finish(t, user, first)
	again, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "two.txt", int64(len(raw)), sum[:])
	if err != nil || again.Status != domain.UploadReady || again.MultipartID != "" {
		t.Fatalf("preflight: %+v, %v", again, err)
	}
	reused, err := h.m.Status(t.Context(), user, again.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Upload.ID == view.Upload.ID || *reused.Upload.FileID != *view.Upload.FileID {
		t.Fatal("want a new upload pointing at the same file")
	}
	var payload domain.AttachmentPayload
	if err := json.Unmarshal(reused.Upload.Payload, &payload); err != nil || payload.Filename != "two.txt" {
		t.Fatalf("payload=%+v, %v", payload, err)
	}
	if h.processor.calls != 1 {
		t.Fatalf("processed %d times", h.processor.calls)
	}
	for _, tc := range []struct {
		user uuid.UUID
		mode domain.UploadMode
	}{{uuid.New(), domain.UploadModeFile}, {user, domain.UploadModeMedia}} {
		session, err := h.m.Start(t.Context(), tc.user, tc.mode, "one", int64(len(raw)), sum[:])
		if err != nil || session.Status != domain.UploadUploading {
			t.Fatalf("unexpected reuse: %+v, %v", session, err)
		}
	}
}

func TestHashMismatchDoesNotProcessOrRememberTheClaimedSource(t *testing.T) {
	h := newHarness(t)
	user := uuid.New()
	raw := content(100, 18)
	claim := sha256.Sum256([]byte("different file"))
	session, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "wrong.txt", int64(len(raw)), claim[:])
	if err != nil {
		t.Fatal(err)
	}
	if err := h.m.PutPart(t.Context(), user, session.ID, 1, bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.Complete(t.Context(), user, session.ID); err != nil {
		t.Fatal(err)
	}
	h.m.process(t.Context(), session.ID)
	state, err := h.m.Status(t.Context(), user, session.ID)
	if err != nil || state.Session.Status != domain.UploadFailed || state.Session.Failure != "hash_mismatch" {
		t.Fatalf("state=%+v, %v", state, err)
	}
	if h.processor.calls != 0 || len(h.sessions.sources) != 0 {
		t.Fatal("unverified bytes were processed or cached")
	}
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "bad.txt", 1, []byte("bad")); !errors.Is(err, ErrBadHash) {
		t.Fatalf("invalid hash: %v", err)
	}
}
