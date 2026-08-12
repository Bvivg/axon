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

// Path is where the socket lives on the chat service's public listener.
const Path = "/ws"

// Rooms is the part of the chat service this socket needs.
//
// Two methods rather than the whole service, and an interface rather than the
// concrete type, for the usual reason: what is worth testing here is the
// protocol — frames in, frames out, who is told what — and a test of that
// should not need a database underneath it.
type Rooms interface {
	Send(ctx context.Context, in service.SendInput) (domain.Message, bool, error)
	ListMessages(ctx context.Context, roomID, userID uuid.UUID, page domain.Page) (service.History, error)
}

// Handler upgrades a request and runs one conversation on it.
type Handler struct {
	svc      Rooms
	verifier *authn.Verifier
	bus      pubsub.Bus
	log      *slog.Logger
	origins  []string
	now      func() time.Time
}

// Config configures a Handler.
type Config struct {
	Service  Rooms
	Verifier *authn.Verifier
	Bus      pubsub.Bus
	Logger   *slog.Logger

	// Origins is the browser origin allow-list for the upgrade.
	//
	// Empty means same-origin only. In the deployed shape nothing browser-side
	// reaches this directly — the gateway terminates the socket and dials here
	// with no Origin header at all — so this stays empty outside development.
	Origins []string

	// Now overrides the clock. Tests set it; production leaves it nil.
	Now func() time.Time
}

// New returns a Handler.
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

// ServeHTTP authenticates the request and upgrades it.
//
// The token is verified before the upgrade, so a caller without one gets a 401
// it can read rather than a socket that opens and immediately closes. It is
// verified here rather than taken on trust from the gateway for the reason
// rules/security.md gives: this service owns the rooms, so it decides who is
// asking.
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
		// Accept has already written a response; there is nothing to say to a
		// client that is no longer listening.
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

// session is one open socket.
type session struct {
	h      *Handler
	conn   *sharedws.Conn
	user   uuid.UUID
	expiry time.Time

	// subs holds one stop function per subscribed room. Only the read loop
	// touches it, so it needs no lock: frames are handled one at a time, and
	// nothing else subscribes on a client's behalf.
	subs map[uuid.UUID]func()
}

// run reads frames until the client goes away, its token runs out, or it says
// something the protocol does not define.
func (s *session) run(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer func() {
		for _, stop := range s.subs {
			stop()
		}
		_ = s.conn.CloseNow()
	}()

	// A socket outlives the token that opened it, and it must not outlive it by
	// much: the whole point of a fifteen-minute access token is that it stops
	// being usable. The client reconnects with a fresh one and catches up from
	// the position it already has, which costs it one round trip.
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

// expiryTimer closes the socket when the token behind it expires.
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

// handle processes one frame, reporting whether the connection continues.
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

// subscribe starts delivery for a room, catching the client up first when it
// says where it left off.
func (s *session) subscribe(ctx context.Context, frame Inbound) bool {
	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", "")
	}

	if _, already := s.subs[roomID]; already {
		// Subscribing twice is not an error, and it must not open a second
		// delivery: the client would then see everything in the room twice.
		return true
	}

	// Registered before anything is read, so a message written while the
	// catch-up is being fetched waits in the channel instead of vanishing
	// between the two.
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

// catchUp reads what the client missed and reports where the room has got to.
//
// A client that names no position gets no history on the socket: the room may
// hold years of it, and paging through it is what ListMessages is for. What the
// socket owes a reconnecting client is the gap since it was last here.
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

	// No position given: the membership check still has to happen, and the
	// cheapest read that performs it also reports where the room is.
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

// deliver forwards the room's messages to this client until the subscription
// ends.
//
// Anything at or before delivered is skipped: the catch-up already sent it, and
// the same message arriving twice is worse than it arriving late.
func (s *session) deliver(ctx context.Context, roomID uuid.UUID, live <-chan domain.Message, delivered int64) {
	for m := range live {
		if m.Seq <= delivered {
			continue
		}
		delivered = m.Seq

		if err := s.write(ctx, messageFrame(m, s.user.String())); err != nil {
			// The socket is gone or too slow to keep up. Either way this
			// conversation is over; the read loop notices and tears the rest
			// down.
			s.h.log.DebugContext(ctx, "stopped delivering to a chat socket",
				"user_id", s.user, "room_id", roomID, "error", err)
			_ = s.conn.CloseNow()
			return
		}
	}
}

// unsubscribe stops delivery for a room.
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

// send writes a message and hands it to the bus.
//
// The order is the one the whole design rests on: durable first, acknowledged
// second, delivered third. A client that has its ack knows the message survives
// this instance; one that does not can resend the same client id and get the
// first copy back rather than writing a second.
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

	// A resend has already been delivered once. Publishing it again would put a
	// second copy in front of everybody in the room.
	if !duplicate {
		if err := s.h.bus.Publish(ctx, message); err != nil {
			s.h.log.ErrorContext(ctx, "could not fan a message out",
				"room_id", roomID, "message_id", message.ID, "error", err)
		}
	}

	return true
}

// report turns an error from below into an error frame.
func (s *session) report(ctx context.Context, err error, clientID string) bool {
	switch {
	case errors.Is(err, domain.ErrNotAMember), errors.Is(err, domain.ErrRoomNotFound):
		// One answer for both, as everywhere else: telling them apart turns a
		// room id into a probe for whether that room exists.
		return s.refuse(ctx, ErrorNotAMember, "no such room", clientID)
	default:
		if v, ok := domain.AsValidationError(err); ok {
			return s.refuse(ctx, ErrorInvalid, v.Field+" "+v.Reason, clientID)
		}
		return s.internal(ctx, "handle frame", err, clientID)
	}
}

// internal logs a failure the client can do nothing about and says so.
func (s *session) internal(ctx context.Context, what string, err error, clientID string) bool {
	s.h.log.ErrorContext(ctx, "chat socket: "+what, "user_id", s.user, "error", err)
	return s.refuse(ctx, ErrorInternal, "something went wrong", clientID)
}

// refuse sends an error frame. A refusal does not end the connection: one bad
// room id is not a reason to drop a conversation in every other room.
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
