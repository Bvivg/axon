package calls

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	callv1 "github.com/bvivg/axon/core/shared/gen/go/axon/calls/v1"
)

type Families interface {
	ActiveFamily(context.Context, string, uuid.UUID) error
}
type Peers interface {
	CheckCallPeer(context.Context, string, uuid.UUID) error
}
type Publisher interface {
	PublishCall(context.Context, uuid.UUID, uuid.UUID) error
	PublishCallSignal(context.Context, uuid.UUID, uuid.UUID, []byte) error
}
type Config struct {
	MigrationEnabled    bool
	DisableNewMigration bool
	Media               *MediaProvider
	DisableNewSFU       bool
	Store               Store
	Families            Families
	Peers               Peers
	Publisher           Publisher
	Logger              *slog.Logger
	TURNURLs            []string
	TURNSecret          string
	Now                 func() time.Time
}
type Service struct {
	migration           bool
	disableNewMigration bool
	media               *MediaProvider
	disableNewSFU       bool
	store               Store
	families            Families
	peers               Peers
	publisher           Publisher
	log                 *slog.Logger
	urls                []string
	secret              string
	now                 func() time.Time
}

func NewService(cfg Config) (*Service, error) {
	if cfg.Store == nil || cfg.Families == nil || cfg.Publisher == nil || cfg.Logger == nil {
		return nil, ErrInvalid
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{migration: cfg.MigrationEnabled, disableNewMigration: cfg.DisableNewMigration, disableNewSFU: cfg.DisableNewSFU, media: cfg.Media, store: cfg.Store, families: cfg.Families, peers: cfg.Peers, publisher: cfg.Publisher, log: cfg.Logger, urls: cfg.TURNURLs, secret: cfg.TURNSecret, now: cfg.Now}, nil
}
func (s *Service) SFUEnabled() bool { return s.media != nil && !s.disableNewSFU }
func (s *Service) RegisterMediaEndpoint(ctx context.Context, actor Actor, resume *EndpointRef, supported bool) (Endpoint, EndpointRef, error) {
	endpoint, ref, err := s.Register(ctx, actor, resume)
	if err != nil || s.media == nil {
		return endpoint, ref, err
	}
	store, ok := s.store.(GroupStore)
	if !ok {
		return Endpoint{}, EndpointRef{}, ErrUnavailable
	}
	if err := store.SetEndpointSFU(ctx, actor, ref, supported, s.now().UTC()); err != nil {
		return Endpoint{}, EndpointRef{}, err
	}
	endpoint.SupportsSFU = supported
	return endpoint, ref, nil
}
func (s *Service) preflight(ctx context.Context, actor Actor) error {
	if actor.UserID == uuid.Nil || actor.FamilyID == uuid.Nil || actor.Token == "" {
		return ErrForbidden
	}
	if err := s.families.ActiveFamily(ctx, actor.Token, actor.FamilyID); err != nil {
		return fmt.Errorf("calls: active family: %w", err)
	}
	return nil
}
func validRef(ref EndpointRef) bool {
	return ref.ID != uuid.Nil && ref.Epoch > 0 && len(ref.Proof) >= 32 && len(ref.Proof) <= 128
}
func (s *Service) Register(ctx context.Context, actor Actor, resume *EndpointRef) (Endpoint, EndpointRef, error) {
	if err := s.preflight(ctx, actor); err != nil {
		return Endpoint{}, EndpointRef{}, err
	}
	proof := base64.RawURLEncoding.EncodeToString(randBytes())
	if resume != nil {
		if !validRef(*resume) {
			return Endpoint{}, EndpointRef{}, ErrInvalid
		}
		proof = resume.Proof
	}
	endpoint, err := s.store.RegisterEndpoint(ctx, actor, resume, proof, s.now().UTC())
	return endpoint, EndpointRef{ID: endpoint.ID, Epoch: endpoint.Epoch, Proof: proof}, err
}
func randBytes() []byte {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return value
}
func (s *Service) Check(ctx context.Context, actor Actor, ref EndpointRef, renew bool) error {
	if !validRef(ref) {
		return ErrInvalid
	}
	if renew {
		if err := s.preflight(ctx, actor); err != nil {
			return err
		}
	}
	_, err := s.store.Endpoint(ctx, actor, ref, s.now().UTC(), renew)
	return err
}

func (s *Service) Bind(ctx context.Context, actor Actor, ref EndpointRef, connection uuid.UUID) error {
	if err := s.Check(ctx, actor, ref, true); err != nil {
		return err
	}
	return s.store.BindEndpoint(ctx, actor, ref, connection, s.now().UTC())
}
func (s *Service) Execute(ctx context.Context, actor Actor, cmd Command) (Call, error) {
	if cmd.Action == "timeout" || cmd.Action == "resume" || cmd.RequestID == uuid.Nil || !validRef(cmd.Endpoint) {
		return Call{}, ErrInvalid
	}
	if cmd.StartsDirect() || cmd.Action == "start_group" {
		if cmd.Mode != "audio" && cmd.Mode != "video" {
			return Call{}, ErrInvalid
		}
		if cmd.Action == "start_direct" {
			if cmd.RoomID != uuid.Nil || cmd.TargetID == uuid.Nil || cmd.TargetID == actor.UserID {
				return Call{}, ErrInvalid
			}
		} else if cmd.RoomID == uuid.Nil {
			return Call{}, ErrInvalid
		}
		cmd.ID = uuid.New()
	} else if cmd.ID == uuid.Nil {
		return Call{}, ErrInvalid
	}
	if err := s.preflight(ctx, actor); err != nil {
		return Call{}, err
	}
	if cmd.Action == "start_group" || cmd.Action == "invite" || cmd.Action == "remove" || cmd.Action == "leave" {
		if s.media == nil {
			return Call{}, ErrUnavailable
		}
	}
	if (cmd.Action == "start_group" || cmd.Action == "invite") && s.disableNewSFU {
		return Call{}, ErrUnavailable
	}
	if s.media != nil {
		endpoint, err := s.store.Endpoint(ctx, actor, cmd.Endpoint, s.now().UTC(), false)
		if err != nil {
			return Call{}, err
		}
		cmd.EnableSFU = !s.disableNewSFU
		cmd.SFUCapable = endpoint.SupportsSFU
		cmd.MigrationCapable = endpoint.SupportsMigration
		cmd.EnableMigration = s.MigrationEnabled()
		if cmd.Action == "migrate" {
			if !cmd.EnableMigration {
				return Call{}, ErrUnavailable
			}
			if err := s.media.Available(ctx); err != nil {
				return Call{}, err
			}
		}
		if cmd.Action == "start_group" {
			if !cmd.SFUCapable {
				return Call{}, ErrForbidden
			}
			if err := s.media.Available(ctx); err != nil {
				return Call{}, err
			}
		}
	}
	if cmd.Action == "start_direct" {
		if s.peers == nil {
			return Call{}, ErrUnavailable
		}
		if s.media == nil {
			if err := s.Check(ctx, actor, cmd.Endpoint, false); err != nil {
				return Call{}, err
			}
		}
		if err := s.peers.CheckCallPeer(ctx, actor.Token, cmd.TargetID); err != nil {
			return Call{}, err
		}
	}
	now := s.now().UTC()
	call, err := s.store.Mutate(ctx, actor, cmd, now, func(call *Call) (bool, error) { return Apply(call, actor.UserID, cmd, now) })
	if cmd.StartsDirect() && errors.Is(err, ErrBusy) {
		call, err = s.store.Mutate(ctx, actor, cmd, now, func(call *Call) (bool, error) {
			call.finish(now, "busy")
			call.Deadline = time.Time{}
			call.Version = 1
			return true, nil
		})
	}
	if err == nil && call.EndReason == "busy" {
		return call, ErrBusy
	}
	return call, err
}
func (s *Service) Attach(ctx context.Context, actor Actor, ref EndpointRef) (*Call, []Event, error) {
	if err := s.Check(ctx, actor, ref, true); err != nil {
		return nil, nil, err
	}
	active, err := s.store.ActiveCall(ctx, actor.UserID)
	if err != nil {
		return nil, nil, err
	}
	if active != nil {
		p := active.Participant(actor.UserID)
		if p != nil && p.EndpointID == ref.ID && p.Epoch != ref.Epoch {
			cmd := Command{Action: "resume", ID: active.ID, Endpoint: ref}
			if s.media != nil {
				endpoint, err := s.store.Endpoint(ctx, actor, ref, s.now().UTC(), false)
				if err != nil {
					return nil, nil, err
				}
				cmd.EnableSFU = s.SFUEnabled()
				cmd.SFUCapable = endpoint.SupportsSFU
				cmd.MigrationCapable = endpoint.SupportsMigration
			}
			now := s.now().UTC()
			resumed, err := s.store.Mutate(ctx, actor, cmd, now, func(call *Call) (bool, error) { return Apply(call, actor.UserID, cmd, now) })
			if err != nil {
				return nil, nil, err
			}
			active = &resumed
		}
	}
	events, err := s.store.Pending(ctx, actor, ref, s.now().UTC())
	return active, events, err
}
func (s *Service) Acknowledge(ctx context.Context, actor Actor, ref EndpointRef, event uuid.UUID) error {
	if event == uuid.Nil {
		return ErrInvalid
	}
	return s.store.Acknowledge(ctx, actor, ref, event, s.now().UTC())
}
func (s *Service) Event(ctx context.Context, actor Actor, ref EndpointRef, event uuid.UUID) (Event, error) {
	return s.store.Event(ctx, actor, ref, event, s.now().UTC())
}
func (s *Service) Get(ctx context.Context, actor Actor, id uuid.UUID) (Call, error) {
	if id == uuid.Nil {
		return Call{}, ErrInvalid
	}
	return s.store.GetCall(ctx, actor.UserID, id)
}
func (s *Service) Active(ctx context.Context, actor Actor) (*Call, error) {
	return s.store.ActiveCall(ctx, actor.UserID)
}
func (s *Service) History(ctx context.Context, actor Actor, before uuid.UUID, limit int) ([]Call, error) {
	return s.store.ListCalls(ctx, actor.UserID, before, limit)
}
func (s *Service) selected(ctx context.Context, actor Actor, ref EndpointRef, id uuid.UUID, generation int64) (Call, error) {
	if err := s.Check(ctx, actor, ref, false); err != nil {
		return Call{}, err
	}
	call, err := s.store.GetCall(ctx, actor.UserID, id)
	if err != nil {
		return Call{}, err
	}
	p := call.Participant(actor.UserID)
	if p == nil || p.EndpointID != ref.ID || p.Epoch != ref.Epoch || p.Status != "accepted" {
		return Call{}, ErrForbidden
	}
	if call.Generation != generation || (call.State != Connecting && call.State != Active) || (call.State == Connecting && !s.now().Before(call.Deadline)) {
		return Call{}, ErrStale
	}
	return call, nil
}
func (s *Service) Credentials(ctx context.Context, actor Actor, ref EndpointRef, id uuid.UUID, generation int64) ([]*callv1.IceServer, time.Time, error) {
	if err := s.preflight(ctx, actor); err != nil {
		return nil, time.Time{}, err
	}
	call, err := s.selected(ctx, actor, ref, id, generation)
	if err != nil {
		return nil, time.Time{}, err
	}
	if call.TransportKind() != "p2p" {
		return nil, time.Time{}, ErrConflict
	}
	return s.turnCredentials(ref)
}
func (s *Service) turnCredentials(ref EndpointRef) ([]*callv1.IceServer, time.Time, error) {
	if len(s.urls) == 0 || len(s.secret) < 32 {
		return nil, time.Time{}, ErrUnavailable
	}
	expires := s.now().UTC().Add(time.Minute)
	username := strconv.FormatInt(expires.Unix(), 10) + ":" + ref.ID.String()
	mac := hmac.New(sha1.New, []byte(s.secret)) //nolint:gosec
	_, _ = mac.Write([]byte(username))
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return []*callv1.IceServer{{Urls: s.urls, Username: username, Credential: credential}}, expires, nil
}
func (s *Service) SendSignal(ctx context.Context, actor Actor, ref EndpointRef, input *callv1.PeerSignal) error {
	if input == nil {
		return ErrInvalid
	}
	id, err := uuid.Parse(input.CallId)
	if err != nil {
		return ErrInvalid
	}
	target, err := uuid.Parse(input.TargetEndpointId)
	if err != nil || target == ref.ID {
		return ErrInvalid
	}
	call, err := s.selected(ctx, actor, ref, id, input.Generation)
	if err != nil {
		return err
	}
	if call.TransportKind() != "p2p" {
		return ErrConflict
	}
	if input.NegotiationId != call.NegotiationID.String() {
		return ErrStale
	}
	switch input.Kind {
	case callv1.PeerSignalKind_PEER_SIGNAL_KIND_ICE:
		if input.Ice == nil || len(input.Ice.Candidate) > 4096 {
			return ErrInvalid
		}
	case callv1.PeerSignalKind_PEER_SIGNAL_KIND_OFFER, callv1.PeerSignalKind_PEER_SIGNAL_KIND_ANSWER:
		if len(input.GetSdp()) == 0 || len(input.GetSdp()) > 16384 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	var user uuid.UUID
	for _, p := range call.Participants {
		if p.UserID != actor.UserID && p.EndpointID == target {
			user = p.UserID
		}
	}
	if user == uuid.Nil {
		return ErrForbidden
	}
	signal, ok := proto.Clone(input).(*callv1.PeerSignal)
	if !ok {
		return ErrInvalid
	}
	sender := ref.ID.String()
	signal.FromEndpointId = &sender
	signal.ExpiresAt = timestamppb.New(s.now().UTC().Add(5 * time.Second))
	raw, err := protojson.Marshal(signal)
	if err != nil {
		return fmt.Errorf("calls: encode signal: %w", err)
	}
	return s.publisher.PublishCallSignal(ctx, user, target, raw)
}
func (s *Service) ReceiveSignal(ctx context.Context, actor Actor, ref EndpointRef, raw []byte) (*callv1.PeerSignal, error) {
	var signal callv1.PeerSignal
	if err := protojson.Unmarshal(raw, &signal); err != nil {
		return nil, ErrInvalid
	}
	if signal.ExpiresAt == nil || !s.now().Before(signal.ExpiresAt.AsTime()) || signal.TargetEndpointId != ref.ID.String() {
		return nil, ErrStale
	}
	id, err := uuid.Parse(signal.CallId)
	if err != nil {
		return nil, ErrInvalid
	}
	call, err := s.selected(ctx, actor, ref, id, signal.Generation)
	if err != nil {
		return nil, err
	}
	if call.TransportKind() != "p2p" {
		return nil, ErrConflict
	}
	if signal.NegotiationId != call.NegotiationID.String() {
		return nil, ErrStale
	}
	valid := false
	for _, p := range call.Participants {
		if p.UserID != actor.UserID && p.EndpointID.String() == signal.GetFromEndpointId() {
			valid = true
		}
	}
	if !valid {
		return nil, ErrForbidden
	}
	return &signal, nil
}
func (s *Service) Run(ctx context.Context) {
	timer := time.NewTicker(250 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.tick(ctx)
		}
	}
}
func (s *Service) tick(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	now := s.now().UTC()
	commands, err := s.store.Expiring(ctx, now)
	if err != nil {
		s.log.WarnContext(ctx, "calls sweep failed", "error", err)
		return
	}
	for _, cmd := range commands {
		cmd.EnableSFU = s.SFUEnabled()
		_, err := s.store.Mutate(ctx, Actor{}, cmd, now, func(call *Call) (bool, error) {
			if call.State == Ended && (call.Deadline.IsZero() || now.Before(call.Deadline)) {
				return false, nil
			}
			if cmd.Reason == "" && (call.Deadline.IsZero() || now.Before(call.Deadline)) {
				return false, nil
			}
			return Apply(call, uuid.Nil, cmd, now)
		})
		if err != nil {
			s.log.WarnContext(ctx, "calls expiry failed", "call_id", cmd.ID, "error", err)
		}
	}
	events, err := s.store.DueEvents(ctx, now)
	if err != nil {
		s.log.WarnContext(ctx, "calls outbox claim failed", "error", err)
		return
	}
	for _, event := range events {
		if err := s.publisher.PublishCall(ctx, event.UserID, event.ID); err != nil {
			s.log.WarnContext(ctx, "calls delivery deferred", "event_id", event.ID, "error", err)
		}
	}
}
