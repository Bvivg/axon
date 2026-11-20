package notify

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func TestCallPushNeverOutlivesRingDeadline(t *testing.T) {
	now := time.Now().UTC()
	call := calls.Call{ID: uuid.New(), State: calls.Ringing, Deadline: now.Add(2800 * time.Millisecond)}
	raw, ttl := callPushPayload(call, now)
	if ttl != 2 {
		t.Fatalf("TTL=%d", ttl)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["call_id"] != call.ID.String() || payload["type"] != "call" || payload["url"] != "/calls" {
		t.Fatalf("bad payload %v", payload)
	}
	if _, ttl = callPushPayload(call, now.Add(3*time.Second)); ttl != 0 {
		t.Fatal("expired ring was pushed")
	}
	call.State = calls.Ended
	if _, ttl = callPushPayload(call, now); ttl != 0 {
		t.Fatal("ended call was pushed")
	}
}
