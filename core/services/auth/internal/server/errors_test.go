package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"connectrpc.com/connect"

	"github.com/bvivg/axon/core/shared/pkg/logger"

	"github.com/bvivg/axon/core/services/auth/internal/domain"
)

func TestSecurityRefusalsReachTheClientWithExplicitCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		err        error
		code       connect.Code
		retryAfter string
	}{
		"fake provider outside its domain": {err: fmt.Errorf("oauth: fake: %w", domain.ErrOauthEmailNotAllowed), code: connect.CodeInvalidArgument},
		"link to an unverified account":    {err: domain.ErrOauthAccountExists, code: connect.CodeFailedPrecondition},
	} {
		t.Run(name, func(t *testing.T) {
			got := translateError(context.Background(), logger.Discard(), tc.err)

			var connectErr *connect.Error
			if !errors.As(got, &connectErr) {
				t.Fatalf("translateError = %v, want a connect error", got)
			}
			if connectErr.Code() != tc.code {
				t.Errorf("code = %v, want %v", connectErr.Code(), tc.code)
			}
			if got := connectErr.Meta().Get("Retry-After"); got != tc.retryAfter {
				t.Errorf("Retry-After = %q, want %q", got, tc.retryAfter)
			}
		})
	}
}
