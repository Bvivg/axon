package repository

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
)

func TestCallFingerprintPreservesR1Replay(t *testing.T) {
	cmd := calls.Command{Action: "start", ID: uuid.New(), RoomID: uuid.MustParse("22222222-2222-4222-8222-222222222222"), RequestID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), Endpoint: calls.EndpointRef{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Epoch: 99, Proof: "rotated"}, Mode: "audio", EnableSFU: true, SFUCapable: true}
	got, err := callFingerprint(cmd)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", got) != "ca68b6b82d3e56500a282afcf61056ef67e2aa4a7bc787feeca509ce457ab90f" {
		t.Fatalf("R1 replay fingerprint changed: %x", got)
	}
}
