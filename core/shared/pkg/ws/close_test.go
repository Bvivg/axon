package ws_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/bvivg/axon/core/shared/pkg/ws"
)

func TestTruncateReasonLeavesShortReasonsAlone(t *testing.T) {
	reason := "session closed by the server"

	if got := ws.TruncateReason(reason); got != reason {
		t.Fatalf("TruncateReason changed a short reason: %q", got)
	}
}

func TestTruncateReasonAtTheLimit(t *testing.T) {
	reason := strings.Repeat("a", ws.MaxCloseReason)

	if got := ws.TruncateReason(reason); got != reason {
		t.Fatalf("a reason of exactly the limit was truncated to %d bytes", len(got))
	}
}

func TestTruncateReasonCutsOverlongReasons(t *testing.T) {
	got := ws.TruncateReason(strings.Repeat("a", 500))

	if len(got) != ws.MaxCloseReason {
		t.Fatalf("truncated to %d bytes, want %d", len(got), ws.MaxCloseReason)
	}
}

// Cutting mid-rune would produce invalid UTF-8, which a close frame may not
// carry — the truncation would break the frame it was supposed to save.
func TestTruncateReasonCutsOnRuneBoundaries(t *testing.T) {
	// Two bytes per rune, so a byte-count cut lands mid-rune half the time.
	got := ws.TruncateReason(strings.Repeat("ю", 100))

	if len(got) > ws.MaxCloseReason {
		t.Fatalf("truncated to %d bytes, want at most %d", len(got), ws.MaxCloseReason)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncation produced invalid UTF-8: %q", got)
	}
}

func TestCloseCodeFor(t *testing.T) {
	cases := map[string]struct {
		err  error
		want ws.StatusCode
	}{
		"no error":          {nil, ws.StatusNormalClosure},
		"read limit":        {fmt.Errorf("ws: read: %w", websocket.ErrMessageTooBig), ws.StatusMessageTooBig},
		"context cancelled": {fmt.Errorf("ws: read: %w", context.Canceled), ws.StatusGoingAway},
		"deadline":          {fmt.Errorf("ws: read: %w", context.DeadlineExceeded), ws.StatusGoingAway},
		"anything else":     {errors.New("handler blew up"), ws.StatusInternalError},
		"peer closed": {
			fmt.Errorf("ws: read: %w", websocket.CloseError{
				Code:   websocket.StatusPolicyViolation,
				Reason: "not your session",
			}),
			ws.StatusPolicyViolation,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, reason := ws.CloseCodeFor(tc.err)

			if got != tc.want {
				t.Fatalf("code = %v, want %v", got, tc.want)
			}
			if len(reason) > ws.MaxCloseReason {
				t.Fatalf("reason is %d bytes, which will not fit in a close frame", len(reason))
			}
		})
	}
}
