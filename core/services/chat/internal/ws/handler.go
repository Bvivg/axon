package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/authn"
	sharedws "github.com/bvivg/axon/core/shared/pkg/ws"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
	"github.com/bvivg/axon/core/services/chat/internal/pubsub"
	"github.com/bvivg/axon/core/services/chat/internal/service"
)

const Path = "/ws"

type Rooms interface {
	Send(ctx context.Context, in service.SendInput) (domain.Message, bool, error)
	ListMessages(ctx context.Context, roomID, userID uuid.UUID, page domain.Page) (service.History, error)
}

type Handler struct {
	svc      Rooms
	verifier *authn.Verifier
	bus      pubsub.Bus
	log      *slog.Logger
	origins  []string
	now      func() time.Time
}

type Config struct {
	Service  Rooms
	Verifier *authn.Verifier
	Bus      pubsub.Bus
	Logger   *slog.Logger

	Origins []string

	Now func() time.Time
}

func New(cfg Config) (*Handler, error) {
	switch {
	case cfg.Service == nil:
		return nil, errors.New("ws: service is required")
	case cfg.Verifier == nil:
		return nil, errors.New("ws: token verifier is required")
	case cfg.Bus == nil:
		return nil, errors.New("ws: a message bus is required")
	case cfg.Logger == nil:
		return nil, errors.New("ws: logger is required")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Handler{
		svc:      cfg.Service,
		verifier: cfg.Verifier,
		bus:      cfg.Bus,
		log:      cfg.Logger,
		origins:  cfg.Origins,
		now:      now,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, err := h.authenticate(r.Header)
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

		h.log.WarnContext(r.Context(), "websocket upgrade failed", "error", err)
		return
	}

	s := &session{
		h:      h,
		conn:   conn,
		user:   claims.UserID,
		expiry: claims.ExpiresAt,
		subs:   make(map[uuid.UUID]func()),
	}
	s.run(r.Context())
}

func (h *Handler) authenticate(headers http.Header) (authn.Claims, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, err
	}
	return h.verifier.Verify(raw)
}

type session struct {
	h      *Handler
	conn   *sharedws.Conn
	user   uuid.UUID
	expiry time.Time

	subs map[uuid.UUID]func()
}

func (s *session) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer func() {
		for _, stop := range s.subs {
			stop()
		}
		_ = s.conn.CloseNow()
	}()

	if timer := s.expiryTimer(cancel); timer != nil {
		defer timer.Stop()
	}

	s.h.log.InfoContext(ctx, "chat socket opened", "user_id", s.user)

	for {
		payload, err := s.conn.Read(ctx)
		if err != nil {
			s.h.log.InfoContext(ctx, "chat socket closed", "user_id", s.user, "error", err)
			return
		}

		frame, err := decode(payload)
		if err != nil {
			_ = s.conn.Close(CloseProtocol, sharedws.TruncateReason(err.Error()))
			return
		}

		if !s.handle(ctx, frame) {
			return
		}
	}
}

func (s *session) expiryTimer(cancel context.CancelFunc) *time.Timer {
	if s.expiry.IsZero() {
		return nil
	}

	remaining := s.expiry.Sub(s.h.now())
	if remaining <= 0 {
		remaining = time.Nanosecond
	}

	return time.AfterFunc(remaining, func() {
		_ = s.conn.Close(CloseTokenExpired, "access token expired")
		cancel()
	})
}

func (s *session) handle(ctx context.Context, frame Inbound) bool {
	switch frame.Type {
	case TypeSubscribe:
		return s.subscribe(ctx, frame)
	case TypeUnsubscribe:
		return s.unsubscribe(ctx, frame)
	case TypeSend:
		return s.send(ctx, frame)
	default:
		_ = s.conn.Close(CloseProtocol, "unknown frame type "+sharedws.TruncateReason(frame.Type))
		return false
	}
}

func (s *session) subscribe(ctx context.Context, frame Inbound) bool {
	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", "")
	}

	if _, already := s.subs[roomID]; already {

		return true
	}

	live, stop, err := s.h.bus.Subscribe(ctx, roomID)
	if err != nil {
		return s.internal(ctx, "subscribe to room", err, "")
	}

	missed, current, err := s.catchUp(ctx, roomID, frame.Since)
	if err != nil {
		stop()
		return s.report(ctx, err, "")
	}

	if err := s.write(ctx, subscribedFrame(roomID.String(), current)); err != nil {
		stop()
		return false
	}

	delivered := frame.Since
	for _, m := range missed {
		if err := s.write(ctx, messageFrame(m, s.user.String())); err != nil {
			stop()
			return false
		}
		delivered = m.Seq
	}

	s.subs[roomID] = stop
	go s.deliver(ctx, roomID, live, delivered)

	return true
}

func (s *session) catchUp(
	ctx context.Context,
	roomID uuid.UUID,
	since int64,
) ([]domain.Message, int64, error) {
	if since > 0 {
		history, err := s.h.svc.ListMessages(ctx, roomID, s.user, domain.Page{AfterSeq: since})
		if err != nil {
			return nil, 0, err
		}

		current := since
		if n := len(history.Messages); n > 0 {
			current = history.Messages[n-1].Seq
		}
		return history.Messages, current, nil
	}

	latest, err := s.h.svc.ListMessages(ctx, roomID, s.user, domain.Page{Limit: 1})
	if err != nil {
		return nil, 0, err
	}

	var current int64
	if n := len(latest.Messages); n > 0 {
		current = latest.Messages[n-1].Seq
	}
	return nil, current, nil
}

func (s *session) deliver(ctx context.Context, roomID uuid.UUID, live <-chan domain.Message, delivered int64) {
	for m := range live {
		if m.Seq <= delivered {
			continue
		}
		delivered = m.Seq

		if err := s.write(ctx, messageFrame(m, s.user.String())); err != nil {

			s.h.log.DebugContext(ctx, "stopped delivering to a chat socket",
				"user_id", s.user, "room_id", roomID, "error", err)
			_ = s.conn.CloseNow()
			return
		}
	}
}

func (s *session) unsubscribe(ctx context.Context, frame Inbound) bool {
	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", "")
	}

	if stop, ok := s.subs[roomID]; ok {
		stop()
		delete(s.subs, roomID)
	}
	return true
}

func (s *session) send(ctx context.Context, frame Inbound) bool {
	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", frame.ClientID)
	}

	message, duplicate, err := s.h.svc.Send(ctx, service.SendInput{
		RoomID:   roomID,
		AuthorID: s.user,
		Body:     frame.Body,
		ClientID: frame.ClientID,
	})
	if err != nil {
		return s.report(ctx, err, frame.ClientID)
	}

	if err := s.write(ctx, ackFrame(message)); err != nil {
		return false
	}

	if !duplicate {
		if err := s.h.bus.Publish(ctx, message); err != nil {
			s.h.log.ErrorContext(ctx, "could not fan a message out",
				"room_id", roomID, "message_id", message.ID, "error", err)
		}
	}

	return true
}

func (s *session) report(ctx context.Context, err error, clientID string) bool {
	switch {
	case errors.Is(err, domain.ErrNotAMember), errors.Is(err, domain.ErrRoomNotFound):

		return s.refuse(ctx, ErrorNotAMember, "no such room", clientID)
	default:
		if v, ok := domain.AsValidationError(err); ok {
			return s.refuse(ctx, ErrorInvalid, v.Field+" "+v.Reason, clientID)
		}
		return s.internal(ctx, "handle frame", err, clientID)
	}
}

func (s *session) internal(ctx context.Context, what string, err error, clientID string) bool {
	s.h.log.ErrorContext(ctx, "chat socket: "+what, "user_id", s.user, "error", err)
	return s.refuse(ctx, ErrorInternal, "something went wrong", clientID)
}

func (s *session) refuse(ctx context.Context, code, reason, clientID string) bool {
	return s.write(ctx, errorFrame(code, reason, clientID)) == nil
}

func (s *session) write(ctx context.Context, out Outbound) error {
	payload, err := encode(out)
	if err != nil {
		s.h.log.ErrorContext(ctx, "could not encode a chat frame", "type", out.Type, "error", err)
		return err
	}
	return s.conn.Write(ctx, payload)
}
