//go:build integration

package integration

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTheUploadBudgetIsChargedAtomically(t *testing.T) {
	r := newRepo(t)
	user := uuid.New()
	day := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)

	var (
		charged atomic.Int32
		wg      sync.WaitGroup
	)
	for range 10 {
		wg.Go(func() {
			ok, err := r.ChargeUpload(t.Context(), user, day, 30, 100)
			if err != nil {
				t.Errorf("ChargeUpload: %v", err)
			}
			if ok {
				charged.Add(1)
			}
		})
	}
	wg.Wait()
	if n := charged.Load(); n != 3 {
		t.Fatalf("%d charges of 30 fit into 100, want 3", n)
	}
}

func TestTheUploadBudgetRenewsDailyAndTakesRefunds(t *testing.T) {
	r := newRepo(t)
	user, other := uuid.New(), uuid.New()
	day := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)
	next := day.AddDate(0, 0, 1)

	charge := func(user uuid.UUID, day time.Time, size int64, want bool) {
		t.Helper()
		ok, err := r.ChargeUpload(t.Context(), user, day, size, 100)
		if err != nil {
			t.Fatalf("ChargeUpload: %v", err)
		}
		if ok != want {
			t.Fatalf("ChargeUpload(%d on %s) = %v, want %v", size, day.Format(time.DateOnly), ok, want)
		}
	}

	charge(user, day, 101, false)
	charge(user, day, 100, true)
	charge(user, day, 1, false)
	charge(other, day, 100, true)

	if err := r.RefundUpload(t.Context(), user, next, 50); err != nil {
		t.Fatalf("RefundUpload: %v", err)
	}
	charge(user, day, 1, false)

	if err := r.RefundUpload(t.Context(), user, day, 40); err != nil {
		t.Fatalf("RefundUpload: %v", err)
	}
	charge(user, day, 40, true)
	charge(user, day, 1, false)

	charge(user, next, 100, true)
	charge(user, day, 1, false)
	if err := r.RefundUpload(t.Context(), user, next, 1000); err != nil {
		t.Fatalf("RefundUpload: %v", err)
	}
	charge(user, next, 100, true)
}
