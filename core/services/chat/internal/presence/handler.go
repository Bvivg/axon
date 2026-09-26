package presence

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	sharedws "github.com/bvivg/axon/core/shared/pkg/ws"
)

const Path = "/ws/presence"

const Subprotocol = "axon.presence.v1"

const (
	TTL             = 30 * time.Second
	RefreshInterval = 10 * time.Second

	expiryMargin = 5 * time.Second
)

const CloseTokenExpired sharedws.StatusCode = 4402

type Tracker interface {
	Touch(ctx context.Context, sessionID, connID string, ttl time.Duration) error
	Clear(ctx context.Context, sessionID, connID string) error
	PublishChanged(ctx context.Context, userID string) error
}

type LastSeenRecorder interface {
	UpdateLastSeen(ctx context.Context, accessToken string)
}

type Handler struct {
	verifier *authn.Verifier
	tracker  Tracker
	lastSeen LastSeenRecorder
	log      *slog.Logger
	origins  []string
}

type Config struct {
	Verifier *authn.Verifier
	Tracker  Tracker
	LastSeen LastSeenRecorder
	Logger   *slog.Logger

	Origins []string
}

func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Verifier == nil:
		return nil, errors.New("presence: token verifier is required")
	case cfg.Tracker == nil:
		return nil, errors.New("presence: a tracker is required")
	case cfg.LastSeen == nil:
		return nil, errors.New("presence: a last-seen recorder is required")
	case cfg.Logger == nil:
		return nil, errors.New("presence: logger is required")
	}

	return &Handler{
		verifier: cfg.Verifier,
		tracker:  cfg.Tracker,
		lastSeen: cfg.LastSeen,
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
	userID := claims.UserID.String()
	connID := uuid.NewString()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	if err := h.tracker.Touch(ctx, sessionID, connID, TTL); err != nil {
		h.log.ErrorContext(ctx, "presence: initial touch failed", "session_id", sessionID, "error", err)
	}
	if err := h.tracker.PublishChanged(ctx, userID); err != nil {
		h.log.WarnContext(ctx, "presence: publish change failed", "user_id", userID, "error", err)
	}

	go h.refresh(ctx, sessionID, connID)

	var rotating atomic.Bool
	if timer := expiryTimer(claims.ExpiresAt, func() {
		rotating.Store(true)
		_ = conn.Close(CloseTokenExpired, "access token expired")
		cancel()
	}); timer != nil {
		defer timer.Stop()
	}

	h.log.InfoContext(ctx, "presence socket opened", "user_id", claims.UserID, "session_id", sessionID)

	for {
		if _, err := conn.Read(ctx); err != nil {
			break
		}
	}
	cancel()

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()

	h.lastSeen.UpdateLastSeen(closeCtx, raw)

	if rotating.Load() {
		h.log.InfoContext(r.Context(), "presence socket rotating to a fresh token",
			"user_id", claims.UserID, "session_id", sessionID)
		return
	}

	h.log.InfoContext(r.Context(), "presence socket closed", "user_id", claims.UserID, "session_id", sessionID)

	if err := h.tracker.Clear(closeCtx, sessionID, connID); err != nil {
		h.log.ErrorContext(closeCtx, "presence: clear failed", "session_id", sessionID, "error", err)
	}
	if err := h.tracker.PublishChanged(closeCtx, userID); err != nil {
		h.log.WarnContext(closeCtx, "presence: publish change failed", "user_id", userID, "error", err)
	}
}

func expiryTimer(expiresAt time.Time, onExpiry func()) *time.Timer {
	if expiresAt.IsZero() {
		return nil
	}

	remaining := time.Until(expiresAt) - expiryMargin
	if remaining <= 0 {
		remaining = time.Nanosecond
	}
	return time.AfterFunc(remaining, onExpiry)
}

func (h *Handler) refresh(ctx context.Context, sessionID, connID string) {
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := h.tracker.Touch(ctx, sessionID, connID, TTL); err != nil {
				h.log.ErrorContext(ctx, "presence: refresh touch failed", "session_id", sessionID, "error", err)
			}
		}
	}
}
