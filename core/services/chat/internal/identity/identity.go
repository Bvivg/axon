// Package identity reads the caller's own profile from auth.
//
// It exists for one thing: the display name snapshot taken when somebody joins
// a room. Chat keeps no accounts and cannot read auth's schema, so the name has
// to be asked for — once, on the way in, rather than on every rendered message.
//
// The call carries the caller's own access token and asks for the caller's own
// profile. Nothing here can read anybody else's: GetMe takes no user id, so a
// bug in this package cannot turn into a way to enumerate accounts.
package identity

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"connectrpc.com/connect"

	authv1 "github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/auth/v1/authv1connect"
	"github.com/bvivg/axon/core/shared/pkg/authn"
	"github.com/bvivg/axon/core/shared/pkg/middleware"
)

// Resolver looks up display names at auth.
type Resolver struct {
	client  authv1connect.AuthServiceClient
	timeout time.Duration
	log     *slog.Logger
}

// Config configures a Resolver.
type Config struct {
	// HTTPClient reaches auth. It has to speak h2c, because there is no TLS on
	// the internal network for HTTP/2 to be negotiated with.
	HTTPClient connect.HTTPClient

	// BaseURL is auth's Connect listener.
	BaseURL string

	// Timeout bounds one lookup. A name is a nicety; joining a room must not
	// wait on it.
	Timeout time.Duration

	Logger *slog.Logger
}

// New returns a Resolver.
func New(cfg Config) (*Resolver, error) {
	switch {
	case cfg.HTTPClient == nil:
		return nil, errors.New("identity: http client is required")
	case cfg.BaseURL == "":
		return nil, errors.New("identity: auth service URL is required")
	case cfg.Logger == nil:
		return nil, errors.New("identity: logger is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	return &Resolver{
		client: authv1connect.NewAuthServiceClient(cfg.HTTPClient, cfg.BaseURL,
			connect.WithInterceptors(middleware.NewCorrelationInterceptor()),
		),
		timeout: timeout,
		log:     cfg.Logger,
	}, nil
}

// DisplayName returns what auth calls the holder of this token, or an empty
// string if it cannot say.
//
// It never returns an error, and that is the design rather than laziness: the
// name is decoration on a membership row. Auth being slow, down, or unwilling
// costs the room a name it can fill in the next time that person joins — it
// must not cost them the room.
func (r *Resolver) DisplayName(ctx context.Context, accessToken string) string {
	if accessToken == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	req := connect.NewRequest(&authv1.GetMeRequest{})
	req.Header().Set(authn.Header, "Bearer "+accessToken)

	resp, err := r.client.GetMe(ctx, req)
	if err != nil {
		r.log.WarnContext(ctx, "could not read the caller's profile", "error", err)
		return ""
	}

	return resp.Msg.GetUser().GetDisplayName()
}

// BearerFromHeader lifts the raw access token back out of a request's headers.
//
// The token has already been verified by the time this is called; what it is
// wanted for is forwarding, not for deciding anything.
func BearerFromHeader(h http.Header) string {
	raw, err := authn.BearerToken(h)
	if err != nil {
		return ""
	}
	return raw
}
