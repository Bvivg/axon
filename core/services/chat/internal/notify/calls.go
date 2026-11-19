package notify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

type CallPushJob struct {
	Event    calls.Event
	Deadline time.Time
	Receipts map[string]bool
}

type CallPushStore interface {
	DueCallPush(context.Context, time.Time) ([]CallPushJob, error)
	RecordCallPush(context.Context, uuid.UUID, string, bool) error
	GetCall(context.Context, uuid.UUID, uuid.UUID) (calls.Call, error)
	PushSubscriptions(context.Context, []uuid.UUID) ([]domain.PushSubscription, error)
	DropPushEndpoint(context.Context, string) error
}

type CallNotifier struct {
	Store  CallPushStore
	Sender WebPush
	Logger *slog.Logger
}

func (n CallNotifier) Run(ctx context.Context) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := n.tick(ctx); err != nil {
				n.Logger.WarnContext(ctx, "call push deferred", "error", err)
			}
		}
	}
}

func (n CallNotifier) tick(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	jobs, err := n.Store.DueCallPush(ctx, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, job := range jobs {
		current, err := n.Store.GetCall(ctx, job.Event.UserID, job.Event.Call.ID)
		if err != nil {
			return err
		}
		deadline, eligible := current.InvitationDeadline(job.Event.UserID, time.Now().UTC())
		original, _ := job.Event.Call.InvitationDeadline(job.Event.UserID, time.Now().UTC())
		if !eligible || !deadline.Equal(original) {
			continue
		}
		current.State = calls.Ringing
		current.Deadline = deadline
		subs, err := n.Store.PushSubscriptions(ctx, []uuid.UUID{job.Event.UserID})
		if err != nil {
			return err
		}
		for _, sub := range subs {
			hash := sha256.Sum256([]byte(sub.Endpoint))
			key := hex.EncodeToString(hash[:])
			if job.Receipts[key] {
				continue
			}
			payload, ttl := callPushPayload(current, time.Now().UTC())
			if ttl < 1 {
				return nil
			}
			status, err := n.Sender.SendTTL(ctx, sub, payload, topicOf(current.ID), ttl)
			if err != nil {
				return err
			}
			if status == http.StatusNotFound || status == http.StatusGone {
				if err = n.Store.DropPushEndpoint(ctx, sub.Endpoint); err != nil {
					return err
				}
			} else if status < 200 || status >= 300 {
				continue
			}
			if err = n.Store.RecordCallPush(ctx, job.Event.ID, key, false); err != nil {
				return err
			}
			job.Receipts[key] = true
		}
		complete := true
		for _, sub := range subs {
			hash := sha256.Sum256([]byte(sub.Endpoint))
			complete = complete && job.Receipts[hex.EncodeToString(hash[:])]
		}
		if complete {
			if err = n.Store.RecordCallPush(ctx, job.Event.ID, "", true); err != nil {
				return err
			}
		}
	}
	return nil
}

func callPushPayload(call calls.Call, now time.Time) ([]byte, int) {
	ttl := int(call.Deadline.Sub(now) / time.Second)
	if call.State != calls.Ringing || ttl < 1 {
		return nil, 0
	}
	payload, _ := json.Marshal(struct {
		Type     string    `json:"type"`
		CallID   string    `json:"call_id"`
		Deadline time.Time `json:"deadline"`
		Title    string    `json:"title"`
		Body     string    `json:"body"`
		URL      string    `json:"url"`
		Tag      string    `json:"tag"`
	}{"call", call.ID.String(), call.Deadline, "Incoming call", "Open Axon to answer.", "/calls", "call-" + call.ID.String()})
	return payload, ttl
}
