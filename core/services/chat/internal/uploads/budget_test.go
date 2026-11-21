package uploads

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type fakeUsage struct {
	mu   sync.Mutex
	used map[string]int64
}

func newFakeUsage() *fakeUsage { return &fakeUsage{used: map[string]int64{}} }

func usageKey(user uuid.UUID, day time.Time) string {
	return user.String() + "/" + day.Format(time.DateOnly)
}

func (u *fakeUsage) ChargeUpload(_ context.Context, user uuid.UUID, day time.Time, size, limit int64) (bool, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	key := usageKey(user, day)
	if u.used[key]+size > limit {
		return false, nil
	}
	u.used[key] += size
	return true, nil
}

func (u *fakeUsage) RefundUpload(_ context.Context, user uuid.UUID, day time.Time, size int64) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	key := usageKey(user, day)
	u.used[key] = max(u.used[key]-size, 0)
	return nil
}

func (u *fakeUsage) total() int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	var sum int64
	for _, n := range u.used {
		sum += n
	}
	return sum
}

func (o *fakeObjects) multipartCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.parts)
}

func TestADailyBudgetStopsOneUserBeforeAnythingIsStored(t *testing.T) {
	h := newHarnessWithBudget(t, 3*testPart)
	alice, bob := uuid.New(), uuid.New()

	if _, err := h.m.Start(t.Context(), alice, domain.UploadModeMedia, "a.mp4", 2*testPart, nil); err != nil {
		t.Fatalf("Start within the budget: %v", err)
	}
	started := h.objects.multipartCount()

	if _, err := h.m.Start(t.Context(), alice, domain.UploadModeMedia, "b.mp4", 2*testPart, nil); !errors.Is(err, domain.ErrUploadQuota) {
		t.Fatalf("Start over the budget: err = %v, want ErrUploadQuota", err)
	}
	if h.objects.multipartCount() != started {
		t.Error("a refused upload still opened a multipart upload in storage")
	}

	if _, err := h.m.Start(t.Context(), bob, domain.UploadModeMedia, "c.mp4", 2*testPart, nil); err != nil {
		t.Fatalf("someone else's budget was spent: %v", err)
	}
}

func TestTheBudgetIsRenewedTheNextDay(t *testing.T) {
	h := newHarnessWithBudget(t, 2*testPart)
	user := uuid.New()
	today := time.Date(2026, 11, 21, 23, 59, 0, 0, time.UTC)
	h.budget.now = func() time.Time { return today }

	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "a.mp4", 2*testPart, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "b.mp4", 1, nil); !errors.Is(err, domain.ErrUploadQuota) {
		t.Fatalf("Start on a spent day: err = %v, want ErrUploadQuota", err)
	}

	today = today.Add(2 * time.Minute)
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "c.mp4", 2*testPart, nil); err != nil {
		t.Fatalf("Start the next day: %v", err)
	}
}

func TestAFileThatIsAlreadyStoredCostsNothing(t *testing.T) {
	raw := content(1000, 21)
	h := newHarnessWithBudget(t, int64(len(raw))+testPart)
	user := uuid.New()
	sum := sha256.Sum256(raw)

	first, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "one.txt", int64(len(raw)), sum[:])
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := h.m.PutPart(t.Context(), user, first.ID, 1, bytes.NewReader(raw), int64(len(raw))); err != nil {
		t.Fatalf("PutPart: %v", err)
	}
	h.finish(t, user, first)
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "fill.mp4", testPart, nil); err != nil {
		t.Fatalf("Start to spend the rest: %v", err)
	}

	again, err := h.m.Start(t.Context(), user, domain.UploadModeFile, "two.txt", int64(len(raw)), sum[:])
	if err != nil || again.Status != domain.UploadReady {
		t.Fatalf("reusing a stored file on a spent budget = %+v, %v; want it ready", again, err)
	}
}

func TestWithdrawingBeforeCompletionGivesTheBytesBack(t *testing.T) {
	h := newHarnessWithBudget(t, 2*testPart)
	user := uuid.New()

	s, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "a.mp4", 2*testPart, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := h.m.Delete(t.Context(), user, s.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if used := h.usage.total(); used != 0 {
		t.Fatalf("used = %d after withdrawing, want 0", used)
	}
	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "b.mp4", 2*testPart, nil); err != nil {
		t.Fatalf("Start after withdrawing: %v", err)
	}
}

func TestAStartThatFailsInStorageGivesTheBytesBack(t *testing.T) {
	h := newHarnessWithBudget(t, 2*testPart)
	user := uuid.New()
	h.objects.startErr = errors.New("storage is down")

	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "a.mp4", 2*testPart, nil); err == nil {
		t.Fatal("Start succeeded with storage down")
	}
	if used := h.usage.total(); used != 0 {
		t.Fatalf("used = %d after a failed start, want 0", used)
	}
}

func TestACompletedUploadKeepsItsCost(t *testing.T) {
	h := newHarnessWithBudget(t, 1<<20)
	user := uuid.New()

	view := h.finish(t, user, h.upload(t, user, content(100, 22)))
	if err := h.m.Delete(t.Context(), user, view.Session.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if used := h.usage.total(); used != 100 {
		t.Fatalf("used = %d, want the 100 bytes that were processed", used)
	}
}

func TestWithdrawingTwiceGivesTheBytesBackOnce(t *testing.T) {
	h := newHarnessWithBudget(t, 4*testPart)
	user := uuid.New()

	if _, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "a.mp4", testPart, nil); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s, err := h.m.Start(t.Context(), user, domain.UploadModeMedia, "b.mp4", 2*testPart, nil)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := h.m.Delete(t.Context(), user, s.ID); err != nil {
				t.Errorf("Delete: %v", err)
			}
		})
	}
	wg.Wait()
	if used := h.usage.total(); used != testPart {
		t.Fatalf("used = %d, want %d: a withdrawal was refunded more than once", used, testPart)
	}
}
