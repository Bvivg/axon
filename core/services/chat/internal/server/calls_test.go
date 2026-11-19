package server

import (
	"testing"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/services/chat/internal/calls"
	"github.com/bvivg/axon/core/shared/pkg/logger"
)

func TestCallErrorsKeepBusySeparateFromRateLimit(t *testing.T) {
	handler := NewCalls(nil, nil, logger.Discard())
	for _, item := range []struct {
		err  error
		code connect.Code
	}{{calls.ErrBusy, connect.CodeFailedPrecondition}, {calls.ErrStale, connect.CodeFailedPrecondition}, {calls.ErrForbidden, connect.CodePermissionDenied}, {calls.ErrConflict, connect.CodeAlreadyExists}, {calls.ErrNotFound, connect.CodeNotFound}} {
		if got := connect.CodeOf(handler.failure(t.Context(), item.err)); got != item.code {
			t.Fatalf("%v => %v, want %v", item.err, got, item.code)
		}
	}
}
