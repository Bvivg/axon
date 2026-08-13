package presence

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	sharedws "github.com/bvivg/axon/core/shared/pkg/ws"
)

const Path = "/ws/presence"

const Subprotocol = "axon.presence.v1"

const (
	TTL             = 30 * time.Second
	RefreshInterval = 10 * time.Second
)

type Tracker interface {
	Touch(ctx context.Context, sessionID string, ttl time.Duration) error
	Clear(ctx context.Context, sessionID string) error
}

type Handler struct {
	verifier *authn.Verifier
	tracker  Tracker
	log      *slog.Logger
	origins  []string
}

type Config struct {
	Verifier *authn.Verifier
	Tracker  Tracker
	Logger   *slog.Logger

	Origins []string
}

func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("presence: token verifier is required")
	case cfg.Tracker == nil:
		return nil, errors.New("presence: a tracker is required")
	case cfg.Logger == nil:
		return nil, errors.New("presence: logger is required")
	}

	return &Handler{
		verifier: cfg.Verifier,
		tracker:  cfg.Tracker,
		log:      cfg.Logger,
		origins:  cfg.Origins,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := authn.BearerToken(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	claims, err := h.verifier.Verify(raw)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := sharedws.Accept(w, r, sharedws.AcceptOptions{
		Subprotocols:   []string{Subprotocol},
		OriginPatterns: h.origins,
		Options:        sharedws.Options{Logger: h.log},
	})
	if err != nil {
		h.log.WarnContext(r.Context(), "presence socket upgrade failed", "error", err)
		return
	}
	defer func() { _ = conn.CloseNow() }()

	sessionID := claims.FamilyID.String()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := h.tracker.Touch(ctx, sessionID, TTL); err != nil {
		h.log.ErrorContext(ctx, "presence: initial touch failed", "session_id", sessionID, "error", err)
	}

	go h.refresh(ctx, sessionID)

	h.log.InfoContext(ctx, "presence socket opened", "user_id", claims.UserID, "session_id", sessionID)

	for {
		if _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	cancel()

	h.log.InfoContext(r.Context(), "presence socket closed", "user_id", claims.UserID, "session_id", sessionID)

	clearCtx, clearCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer clearCancel()
	if err := h.tracker.Clear(clearCtx, sessionID); err != nil {
		h.log.ErrorContext(clearCtx, "presence: clear failed", "session_id", sessionID, "error", err)
	}
}

func (h *Handler) refresh(ctx context.Context, sessionID string) {
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.tracker.Touch(ctx, sessionID, TTL); err != nil {
				h.log.ErrorContext(ctx, "presence: refresh touch failed", "session_id", sessionID, "error", err)
			}
		}
	}
}
