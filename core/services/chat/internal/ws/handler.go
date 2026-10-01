package ws

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
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
	SendDirect(ctx context.Context, in service.SendDirectInput) (domain.Message, domain.Room, bool, error)
	ListMessages(ctx context.Context, roomID, userID uuid.UUID, page domain.Page) (service.History, error)
	HistoryStart(ctx context.Context, roomID, userID uuid.UUID) (int64, error)
	EditMessage(ctx context.Context, in service.EditInput) (domain.Message, error)
	DeleteMessage(ctx context.Context, messageID, userID uuid.UUID) (domain.Message, error)
}

type Names interface {
	DisplayName(ctx context.Context, accessToken string) string
}

type RevocationSubscriber interface {
	Subscribe(ctx context.Context, userID, familyID uuid.UUID) (<-chan struct{}, func(), error)
}

type PresenceWatcher interface {
	SubscribeChanged(ctx context.Context, userID string) (<-chan struct{}, func(), error)
}

const maxPresenceWatches = 64

type Handler struct {
	svc        Rooms
	verifier   *authn.Verifier
	bus        pubsub.Bus
	signals    pubsub.SignalBus
	users      pubsub.UserSignalBus
	names      Names
	revocation RevocationSubscriber
	presence   PresenceWatcher
	log        *slog.Logger
	origins    []string
	now        func() time.Time
}

type Config struct {
	Service     Rooms
	Verifier    *authn.Verifier
	Bus         pubsub.Bus
	Signals     pubsub.SignalBus
	UserSignals pubsub.UserSignalBus
	Names       Names
	Revocation  RevocationSubscriber
	Presence    PresenceWatcher
	Logger      *slog.Logger

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
	case cfg.Signals == nil:
		return nil, errors.New("ws: a room signal bus is required")
	case cfg.UserSignals == nil:
		return nil, errors.New("ws: a user signal bus is required")
	case cfg.Names == nil:
		return nil, errors.New("ws: a display name resolver is required")
	case cfg.Logger == nil:
		return nil, errors.New("ws: logger is required")
	}

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Handler{
		svc:        cfg.Service,
		verifier:   cfg.Verifier,
		bus:        cfg.Bus,
		signals:    cfg.Signals,
		users:      cfg.UserSignals,
		names:      cfg.Names,
		revocation: cfg.Revocation,
		presence:   cfg.Presence,
		log:        cfg.Logger,
		origins:    cfg.Origins,
		now:        now,
	}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	claims, token, err := h.authenticate(r.Header)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	userSignals, stopUserSignals, err := h.users.SubscribeUserSignals(r.Context(), claims.UserID)
	if err != nil {
		h.log.ErrorContext(r.Context(), "could not listen for the user's room changes", "user_id", claims.UserID, "error", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	defer stopUserSignals()

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
		h:        h,
		conn:     conn,
		user:     claims.UserID,
		familyID: claims.FamilyID,
		token:    token,
		expiry:   claims.ExpiresAt,
		subs:     make(map[uuid.UUID]func()),
		watches:  make(map[uuid.UUID]func()),
	}
	s.run(r.Context(), userSignals)
}

func (h *Handler) authenticate(headers http.Header) (authn.Claims, string, error) {
	raw, err := authn.BearerToken(headers)
	if err != nil {
		return authn.Claims{}, "", err
	}
	claims, err := h.verifier.Verify(raw)
	if err != nil {
		return authn.Claims{}, "", err
	}
	return claims, raw, nil
}

type session struct {
	h        *Handler
	conn     *sharedws.Conn
	user     uuid.UUID
	familyID uuid.UUID
	token    string
	expiry   time.Time

	mu   sync.Mutex
	subs map[uuid.UUID]func()

	watches map[uuid.UUID]func()
}

func (s *session) run(ctx context.Context, userSignals <-chan pubsub.Signal) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	defer func() {
		s.mu.Lock()
		for _, stop := range s.subs {
			stop()
		}
		s.mu.Unlock()
		for _, stop := range s.watches {
			stop()
		}
		_ = s.conn.CloseNow()
	}()

	if timer := s.expiryTimer(cancel); timer != nil {
		defer timer.Stop()
	}

	if stop := s.watchRevocation(ctx, cancel); stop != nil {
		defer stop()
	}

	go s.deliverUserSignals(ctx, userSignals)

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

func (s *session) watchRevocation(ctx context.Context, cancel context.CancelFunc) func() {
	if s.h.revocation == nil {
		return nil
	}

	signal, stop, err := s.h.revocation.Subscribe(ctx, s.user, s.familyID)
	if err != nil {
		s.h.log.WarnContext(ctx, "could not watch for session revocation", "user_id", s.user, "error", err)
		return nil
	}

	go func() {
		select {
		case <-signal:
			_ = s.conn.Close(CloseSessionRevoked, "session revoked")
			cancel()
		case <-ctx.Done():
		}
	}()

	return stop
}

func (s *session) handle(ctx context.Context, frame Inbound) bool {
	switch frame.Type {
	case TypeSubscribe:
		return s.subscribe(ctx, frame)
	case TypeUnsubscribe:
		return s.unsubscribe(ctx, frame)
	case TypeSend:
		return s.send(ctx, frame)
	case TypeTyping:
		return s.typing(ctx, frame)
	case TypeWatchPresence:
		return s.watchPresence(ctx, frame)
	case TypeEdit:
		return s.edit(ctx, frame)
	case TypeDelete:
		return s.delete(ctx, frame)
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

	if s.subscribed(roomID) {
		return true
	}

	live, stop, err := s.h.bus.Subscribe(ctx, roomID)
	if err != nil {
		return s.internal(ctx, "subscribe to room", err, "")
	}

	liveSignals, stopSignals, err := s.h.signals.SubscribeSignals(ctx, roomID)
	if err != nil {
		stop()
		return s.internal(ctx, "subscribe to room signals", err, "")
	}

	s.mu.Lock()
	s.subs[roomID] = func() {
		stop()
		stopSignals()
	}
	s.mu.Unlock()

	missed, current, err := s.catchUp(ctx, roomID, frame.Since)
	if err != nil {
		s.drop(roomID)
		return s.report(ctx, err, "")
	}

	start, err := s.h.svc.HistoryStart(ctx, roomID, s.user)
	if err != nil {
		s.drop(roomID)
		return s.report(ctx, err, "")
	}

	if err := s.write(ctx, subscribedFrame(roomID.String(), current)); err != nil {
		s.drop(roomID)
		return false
	}

	delivered := frame.Since
	for _, m := range missed {
		if err := s.write(ctx, messageFrame(m, s.user.String())); err != nil {
			s.drop(roomID)
			return false
		}
		delivered = m.Seq
	}

	go s.deliver(ctx, roomID, live, delivered, start)
	go s.deliverSignals(ctx, roomID, liveSignals)

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

func (s *session) subscribed(roomID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.subs[roomID]
	return ok
}

func (s *session) drop(roomID uuid.UUID) {
	s.mu.Lock()
	stop, ok := s.subs[roomID]
	delete(s.subs, roomID)
	s.mu.Unlock()

	if ok {
		stop()
	}
}

func (s *session) deliver(ctx context.Context, roomID uuid.UUID, live <-chan pubsub.Delivery, delivered, start int64) {
	for d := range live {
		var out Outbound
		switch {
		case d.Update && d.Message.Seq <= start:
			continue
		case d.Update:
			out = messageUpdatedFrame(d.Message)
		case d.Message.Seq <= delivered:
			continue
		default:
			delivered = d.Message.Seq
			out = messageFrame(d.Message, s.user.String())
		}

		if err := s.write(ctx, out); err != nil {

			s.h.log.DebugContext(ctx, "stopped delivering to a chat socket",
				"user_id", s.user, "room_id", roomID, "error", err)
			_ = s.conn.CloseNow()
			return
		}
	}
}

func (s *session) deliverSignals(ctx context.Context, roomID uuid.UUID, live <-chan pubsub.Signal) {
	for sig := range live {
		if sig.UserID == s.user {
			continue
		}

		var out Outbound
		switch sig.Kind {
		case pubsub.SignalRead:
			out = readFrame(roomID.String(), sig.UserID.String(), sig.Seq)
		case pubsub.SignalTyping:
			out = typingFrame(roomID.String(), sig.UserID.String())
		default:
			continue
		}

		if err := s.write(ctx, out); err != nil {
			s.h.log.DebugContext(ctx, "stopped delivering room signals to a chat socket",
				"user_id", s.user, "room_id", roomID, "error", err)
			_ = s.conn.CloseNow()
			return
		}
	}
}

func (s *session) deliverUserSignals(ctx context.Context, live <-chan pubsub.Signal) {
	for sig := range live {
		var out Outbound
		switch sig.Kind {
		case pubsub.SignalRoomAdded:
			out = roomAddedFrame(sig.RoomID.String())
		case pubsub.SignalRoomRemoved:
			s.drop(sig.RoomID)
			out = roomRemovedFrame(sig.RoomID.String())
		default:
			continue
		}
		if err := s.write(ctx, out); err != nil {
			s.h.log.DebugContext(ctx, "stopped delivering room changes to a chat socket",
				"user_id", s.user, "error", err)
			_ = s.conn.CloseNow()
			return
		}
	}
}

func (s *session) typing(ctx context.Context, frame Inbound) bool {
	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", "")
	}

	if !s.subscribed(roomID) {
		return s.refuse(ctx, ErrorNotAMember, "no such room", "")
	}

	if err := s.h.signals.PublishSignal(ctx, pubsub.Signal{
		Kind:   pubsub.SignalTyping,
		RoomID: roomID,
		UserID: s.user,
	}); err != nil {
		s.h.log.WarnContext(ctx, "could not fan a typing signal out", "room_id", roomID, "error", err)
	}
	return true
}

func (s *session) watchPresence(ctx context.Context, frame Inbound) bool {
	userID, err := uuid.Parse(frame.ToUserID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "to_user_id is not a valid id", "")
	}

	if s.h.presence == nil {
		return true
	}
	if _, already := s.watches[userID]; already {
		return true
	}
	if len(s.watches) >= maxPresenceWatches {
		return s.refuse(ctx, ErrorInvalid, "too many presence watches", "")
	}

	changed, stop, err := s.h.presence.SubscribeChanged(ctx, userID.String())
	if err != nil {
		return s.internal(ctx, "watch presence", err, "")
	}
	s.watches[userID] = stop

	go func() {
		for range changed {
			if err := s.write(ctx, presenceFrame(userID.String())); err != nil {
				s.h.log.DebugContext(ctx, "stopped delivering presence to a chat socket",
					"user_id", s.user, "watched_user_id", userID, "error", err)
				_ = s.conn.CloseNow()
				return
			}
		}
	}()

	return true
}

func (s *session) unsubscribe(ctx context.Context, frame Inbound) bool {
	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", "")
	}

	s.drop(roomID)
	return true
}

func (s *session) edit(ctx context.Context, frame Inbound) bool {
	messageID, err := uuid.Parse(frame.MessageID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "message_id is not a valid id", frame.ClientID)
	}

	edited, err := s.h.svc.EditMessage(ctx, service.EditInput{MessageID: messageID, UserID: s.user, Body: frame.Body})
	if err != nil {
		return s.report(ctx, err, frame.ClientID)
	}

	return s.publishUpdate(ctx, edited)
}

func (s *session) delete(ctx context.Context, frame Inbound) bool {
	messageID, err := uuid.Parse(frame.MessageID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "message_id is not a valid id", frame.ClientID)
	}

	deleted, err := s.h.svc.DeleteMessage(ctx, messageID, s.user)
	if err != nil {
		return s.report(ctx, err, frame.ClientID)
	}

	return s.publishUpdate(ctx, deleted)
}

func (s *session) publishUpdate(ctx context.Context, m domain.Message) bool {
	if err := s.h.bus.PublishUpdate(ctx, m); err != nil {
		s.h.log.ErrorContext(ctx, "could not fan a message change out",
			"room_id", m.RoomID, "message_id", m.ID, "error", err)
		return s.write(ctx, messageUpdatedFrame(m)) == nil
	}
	return true
}

func (s *session) send(ctx context.Context, frame Inbound) bool {
	if (frame.RoomID == "") == (frame.ToUserID == "") {
		return s.refuse(ctx, ErrorInvalid, "exactly one of room_id or to_user_id is required", frame.ClientID)
	}

	if len(frame.Payload) > 0 {
		return s.report(ctx, domain.ErrClientPayload, frame.ClientID)
	}

	if frame.ToUserID != "" {
		return s.sendDirect(ctx, frame)
	}

	roomID, err := uuid.Parse(frame.RoomID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "room_id is not a valid id", frame.ClientID)
	}

	message, duplicate, err := s.h.svc.Send(ctx, service.SendInput{
		RoomID:          roomID,
		AuthorID:        s.user,
		Body:            frame.Body,
		ClientID:        frame.ClientID,
		Kind:            frame.Kind,
		UploadID:        frame.UploadID,
		ReplyToID:       frame.ReplyToID,
		ForwardedFromID: frame.ForwardedFromID,
	})
	if err != nil {
		return s.report(ctx, err, frame.ClientID)
	}

	return s.ackAndPublish(ctx, message, duplicate)
}

func (s *session) sendDirect(ctx context.Context, frame Inbound) bool {
	toUserID, err := uuid.Parse(frame.ToUserID)
	if err != nil {
		return s.refuse(ctx, ErrorInvalid, "to_user_id is not a valid id", frame.ClientID)
	}

	displayName := s.h.names.DisplayName(ctx, s.token)

	message, room, duplicate, err := s.h.svc.SendDirect(ctx, service.SendDirectInput{
		FromUserID:      s.user,
		FromDisplayName: displayName,
		ToUserID:        toUserID,
		Body:            frame.Body,
		ClientID:        frame.ClientID,
		Kind:            frame.Kind,
		UploadID:        frame.UploadID,
		ReplyToID:       frame.ReplyToID,
		ForwardedFromID: frame.ForwardedFromID,
	})
	if err != nil {
		return s.report(ctx, err, frame.ClientID)
	}

	if !s.ackAndPublish(ctx, message, duplicate) {
		return false
	}

	if !duplicate {
		for _, userID := range []uuid.UUID{toUserID, s.user} {
			if err := s.h.users.PublishUserSignal(ctx, userID, pubsub.Signal{
				Kind:   pubsub.SignalRoomAdded,
				RoomID: room.ID,
				UserID: s.user,
			}); err != nil {
				s.h.log.WarnContext(ctx, "could not announce a direct room",
					"room_id", room.ID, "user_id", userID, "error", err)
			}
		}
	}

	return true
}

func (s *session) ackAndPublish(ctx context.Context, message domain.Message, duplicate bool) bool {
	if err := s.write(ctx, ackFrame(message)); err != nil {
		return false
	}

	if !duplicate {
		if err := s.h.bus.Publish(ctx, message); err != nil {
			s.h.log.ErrorContext(ctx, "could not fan a message out",
				"room_id", message.RoomID, "message_id", message.ID, "error", err)
		}
		for _, userID := range message.Revealed {
			if err := s.h.users.PublishUserSignal(ctx, userID, pubsub.Signal{
				Kind:   pubsub.SignalRoomAdded,
				RoomID: message.RoomID,
				UserID: s.user,
			}); err != nil {
				s.h.log.WarnContext(ctx, "could not bring a hidden room back",
					"room_id", message.RoomID, "user_id", userID, "error", err)
			}
		}
	}

	return true
}

func (s *session) report(ctx context.Context, err error, clientID string) bool {
	switch {
	case errors.Is(err, domain.ErrNotAMember), errors.Is(err, domain.ErrRoomNotFound):

		return s.refuse(ctx, ErrorNotAMember, "no such room", clientID)
	case errors.Is(err, domain.ErrNotMessageAuthor):
		return s.refuse(ctx, ErrorForbidden, err.Error(), clientID)
	case errors.Is(err, domain.ErrMessageNotFound),
		errors.Is(err, domain.ErrMessageNotEditable),
		errors.Is(err, domain.ErrMessageDeleted):
		return s.refuse(ctx, ErrorInvalid, err.Error(), clientID)
	case errors.Is(err, domain.ErrInvalidReplyTarget),
		errors.Is(err, domain.ErrInvalidForwardTarget),
		errors.Is(err, domain.ErrSystemKindNotSendable),
		errors.Is(err, domain.ErrCannotMessageSelf),
		errors.Is(err, domain.ErrUploadNotFound),
		errors.Is(err, domain.ErrUploadRequired),
		errors.Is(err, domain.ErrUploadKindMismatch),
		errors.Is(err, domain.ErrClientPayload):
		return s.refuse(ctx, ErrorInvalid, err.Error(), clientID)
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
