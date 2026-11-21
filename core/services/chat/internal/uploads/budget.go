package uploads

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type Usage interface {
	ChargeUpload(ctx context.Context, user uuid.UUID, day time.Time, size, limit int64) (bool, error)
	RefundUpload(ctx context.Context, user uuid.UUID, day time.Time, size int64) error
}

type Budget struct {
	usage Usage
	daily int64
	now   func() time.Time
}

func NewBudget(usage Usage, daily int64) *Budget {
	return &Budget{usage: usage, daily: daily, now: time.Now}
}

func (b *Budget) Charge(ctx context.Context, user uuid.UUID, size int64) (time.Time, error) {
	day := dayOf(b.now())
	if b.daily <= 0 {
		return day, nil
	}
	if size > b.daily {
		return day, domain.ErrUploadQuota
	}
	charged, err := b.usage.ChargeUpload(ctx, user, day, size, b.daily)
	if err != nil {
		return day, err
	}
	if !charged {
		return day, domain.ErrUploadQuota
	}
	return day, nil
}

func (b *Budget) Refund(ctx context.Context, user uuid.UUID, chargedAt time.Time, size int64) error {
	if b.daily <= 0 {
		return nil
	}
	return b.usage.RefundUpload(ctx, user, dayOf(chargedAt), size)
}

func dayOf(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
