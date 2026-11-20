package pubsub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const (
	defaultShards = 16
	maxShards     = 1024
	defaultMaxLen = 10000
	defaultBlock  = time.Second

	defaultRetryMin = 100 * time.Millisecond
	defaultRetryMax = 5 * time.Second

	readBatch       = 256
	positionTimeout = 3 * time.Second

	streamStart = "0-0"

	fieldKind    = "kind"
	fieldTo      = "to"
	fieldPayload = "payload"

	entryMessage    = "message"
	entryRoomSignal = "room_signal"
	entryUserSignal = "user_signal"
)

type fanoutConfig struct {
	shards   int
	maxLen   int
	block    time.Duration
	retryMin time.Duration
	retryMax time.Duration
}

type Option func(*fanoutConfig)

func WithShards(n int) Option { return func(c *fanoutConfig) { c.shards = n } }

func WithMaxLen(n int) Option { return func(c *fanoutConfig) { c.maxLen = n } }

func WithBlock(d time.Duration) Option { return func(c *fanoutConfig) { c.block = d } }

func withRetry(lo, hi time.Duration) Option {
	return func(c *fanoutConfig) { c.retryMin, c.retryMax = lo, hi }
}

func (c fanoutConfig) validate() error {
	switch {
	case c.shards < 1 || c.shards > maxShards:
		return fmt.Errorf("pubsub: fan-out shards must be between 1 and %d, got %d", maxShards, c.shards)
	case c.maxLen < 1:
		return fmt.Errorf("pubsub: fan-out stream length must be positive, got %d", c.maxLen)
	case c.block < time.Millisecond:
		return fmt.Errorf("pubsub: fan-out block timeout must be at least 1ms, got %s", c.block)
	case c.retryMin <= 0 || c.retryMax < c.retryMin:
		return fmt.Errorf("pubsub: fan-out retry window %s..%s is invalid", c.retryMin, c.retryMax)
	}
	return nil
}

type Redis struct {
	client goredis.UniversalClient
	local  *Memory
	log    *slog.Logger
	cfg    fanoutConfig

	streams []string
	shardOf map[string]int

	cursor  []string
	resumed bool

	quit      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func NewRedis(client goredis.UniversalClient, log *slog.Logger, opts ...Option) (*Redis, error) {
	if client == nil {
		return nil, errors.New("pubsub: a redis client is required")
	}
	if log == nil {
		return nil, errors.New("pubsub: a logger is required")
	}

	cfg := fanoutConfig{
		shards:   defaultShards,
		maxLen:   defaultMaxLen,
		block:    defaultBlock,
		retryMin: defaultRetryMin,
		retryMax: defaultRetryMax,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	streams := make([]string, cfg.shards)
	shardOf := make(map[string]int, cfg.shards)
	for i := range streams {
		streams[i] = fanoutStream(i)
		shardOf[streams[i]] = i
	}

	r := &Redis{
		client:  client,
		local:   NewMemory(),
		log:     log,
		cfg:     cfg,
		streams: streams,
		shardOf: shardOf,
		quit:    make(chan struct{}),
		done:    make(chan struct{}),
	}

	start, cancelStart := context.WithTimeout(context.Background(), positionTimeout)
	if err := r.position(start); err != nil {
		log.Warn("chat fan-out could not find the stream ends yet, the reader will retry", "error", err)
	}
	cancelStart()

	go r.run()

	return r, nil
}

func fanoutStream(shard int) string { return "chat:fanout:" + strconv.Itoa(shard) }

type signalWire struct {
	Kind       string          `json:"kind"`
	RoomID     string          `json:"room_id"`
	UserID     string          `json:"user_id"`
	Seq        int64           `json:"seq,omitempty"`
	EventID    string          `json:"event_id,omitempty"`
	EndpointID string          `json:"endpoint_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

func toSignalWire(sig Signal) signalWire {
	return signalWire{Kind: string(sig.Kind), RoomID: sig.RoomID.String(), UserID: sig.UserID.String(), Seq: sig.Seq, EventID: sig.EventID.String(), EndpointID: sig.EndpointID.String(), Payload: json.RawMessage(sig.Payload)}
}

func (w signalWire) toDomain() (Signal, error) {
	if SignalKind(w.Kind) == SignalCallState || SignalKind(w.Kind) == SignalCallPeer {
		user, err := uuid.Parse(w.UserID)
		if err != nil {
			return Signal{}, errors.New("pubsub: invalid call recipient")
		}
		event, _ := uuid.Parse(w.EventID)
		endpoint, _ := uuid.Parse(w.EndpointID)
		if SignalKind(w.Kind) == SignalCallState && event == uuid.Nil {
			return Signal{}, errors.New("pubsub: invalid call event")
		}
		if SignalKind(w.Kind) == SignalCallPeer && (endpoint == uuid.Nil || len(w.Payload) > 24576) {
			return Signal{}, errors.New("pubsub: invalid peer signal")
		}
		return Signal{Kind: SignalKind(w.Kind), UserID: user, EventID: event, EndpointID: endpoint, Payload: string(w.Payload)}, nil
	}
	roomID, err := uuid.Parse(w.RoomID)
	if err != nil {
		return Signal{}, fmt.Errorf("pubsub: signal room id: %w", err)
	}
	userID, err := uuid.Parse(w.UserID)
	if err != nil {
		return Signal{}, fmt.Errorf("pubsub: signal user id: %w", err)
	}

	signal := Signal{Kind: SignalKind(w.Kind), RoomID: roomID, UserID: userID, Seq: w.Seq}
	if signal.Kind == SignalRoomAdded {
		signal.Payload = string(w.Payload)
	}
	return signal, nil
}

type wire struct {
	ID       string    `json:"id"`
	RoomID   string    `json:"room_id"`
	AuthorID string    `json:"author_id"`
	Body     string    `json:"body"`
	Seq      int64     `json:"seq"`
	SentAt   time.Time `json:"sent_at"`
	ClientID string    `json:"client_id,omitempty"`

	Kind    string          `json:"kind,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`

	ReplyToID       *uuid.UUID `json:"reply_to_id,omitempty"`
	ForwardedFromID *uuid.UUID `json:"forwarded_from_id,omitempty"`

	ForwardOriginAuthorID *uuid.UUID `json:"forward_origin_author_id,omitempty"`
	EditedAt              *time.Time `json:"edited_at,omitempty"`
	DeletedAt             *time.Time `json:"deleted_at,omitempty"`
	ReplyTo               *replyWire `json:"reply_to,omitempty"`

	Update bool `json:"update,omitempty"`
}

type replyWire struct {
	ID       uuid.UUID `json:"id"`
	AuthorID uuid.UUID `json:"author_id"`
	Kind     string    `json:"kind"`
	Body     string    `json:"body,omitempty"`
	Deleted  bool      `json:"deleted,omitempty"`
}

func toWire(m domain.Message) wire {
	w := wire{
		ID:              m.ID.String(),
		RoomID:          m.RoomID.String(),
		AuthorID:        m.AuthorID.String(),
		Body:            m.Body,
		Seq:             m.Seq,
		SentAt:          m.SentAt,
		ClientID:        m.ClientID,
		Kind:            string(m.Kind),
		Payload:         m.Payload,
		ReplyToID:       m.ReplyToID,
		ForwardedFromID: m.ForwardedFromID,

		ForwardOriginAuthorID: m.ForwardOriginAuthorID,
		EditedAt:              m.EditedAt,
		DeletedAt:             m.DeletedAt,
	}
	if m.ReplyTo != nil {
		w.ReplyTo = &replyWire{
			ID:       m.ReplyTo.ID,
			AuthorID: m.ReplyTo.AuthorID,
			Kind:     string(m.ReplyTo.Kind),
			Body:     m.ReplyTo.Body,
			Deleted:  m.ReplyTo.Deleted,
		}
	}
	return w
}

func (w wire) toDomain() (domain.Message, error) {
	id, err := uuid.Parse(w.ID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("pubsub: message id: %w", err)
	}
	roomID, err := uuid.Parse(w.RoomID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("pubsub: room id: %w", err)
	}
	authorID, err := uuid.Parse(w.AuthorID)
	if err != nil {
		return domain.Message{}, fmt.Errorf("pubsub: author id: %w", err)
	}

	m := domain.Message{
		ID:       id,
		RoomID:   roomID,
		AuthorID: authorID,
		Body:     w.Body,
		Seq:      w.Seq,
		SentAt:   w.SentAt,
		ClientID: w.ClientID,

		Kind:    kindOrText(w.Kind),
		Payload: w.Payload,

		ReplyToID:       w.ReplyToID,
		ForwardedFromID: w.ForwardedFromID,

		ForwardOriginAuthorID: w.ForwardOriginAuthorID,
		EditedAt:              w.EditedAt,
		DeletedAt:             w.DeletedAt,
	}
	if w.ReplyTo != nil {
		m.ReplyTo = &domain.ReplyPreview{
			ID:       w.ReplyTo.ID,
			AuthorID: w.ReplyTo.AuthorID,
			Kind:     kindOrText(w.ReplyTo.Kind),
			Body:     w.ReplyTo.Body,
			Deleted:  w.ReplyTo.Deleted,
		}
	}
	return m, nil
}

func kindOrText(raw string) domain.MessageKind {
	if raw == "" {
		return domain.MessageKindText
	}
	return domain.MessageKind(raw)
}

func (r *Redis) Publish(ctx context.Context, m domain.Message) error {
	return r.publishMessage(ctx, m.RoomID, toWire(m))
}

func (r *Redis) PublishUpdate(ctx context.Context, m domain.Message) error {
	w := toWire(m)
	w.Update = true
	return r.publishMessage(ctx, m.RoomID, w)
}

func (r *Redis) publishMessage(ctx context.Context, roomID uuid.UUID, w wire) error {
	payload, err := json.Marshal(w)
	if err != nil {
		return fmt.Errorf("pubsub: encode message: %w", err)
	}
	return r.append(ctx, entryMessage, roomID, payload)
}

func (r *Redis) PublishSignal(ctx context.Context, sig Signal) error {
	return r.publishSignal(ctx, entryRoomSignal, sig.RoomID, sig)
}

func (r *Redis) PublishUserSignal(ctx context.Context, userID uuid.UUID, sig Signal) error {
	return r.publishSignal(ctx, entryUserSignal, userID, sig)
}

func (r *Redis) publishSignal(ctx context.Context, kind string, to uuid.UUID, sig Signal) error {
	payload, err := json.Marshal(toSignalWire(sig))
	if err != nil {
		return fmt.Errorf("pubsub: encode %s signal: %w", sig.Kind, err)
	}
	return r.append(ctx, kind, to, payload)
}

func (r *Redis) append(ctx context.Context, kind string, to uuid.UUID, payload []byte) error {
	stream := r.streams[r.shard(to)]
	err := r.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: stream,
		MaxLen: int64(r.cfg.maxLen),
		Approx: true,
		Values: []any{fieldKind, kind, fieldTo, to.String(), fieldPayload, payload},
	}).Err()
	if err != nil {
		return fmt.Errorf("pubsub: append to %s: %w", stream, err)
	}
	return nil
}

func (r *Redis) shard(id uuid.UUID) int {
	h := fnv.New32a()
	_, _ = h.Write(id[:])
	return int(h.Sum32() % uint32(len(r.streams)))
}

func (r *Redis) Subscribe(ctx context.Context, roomID uuid.UUID) (<-chan Delivery, func(), error) {
	return r.local.Subscribe(ctx, roomID)
}

func (r *Redis) SubscribeSignals(ctx context.Context, roomID uuid.UUID) (<-chan Signal, func(), error) {
	return r.local.SubscribeSignals(ctx, roomID)
}

func (r *Redis) SubscribeUserSignals(ctx context.Context, userID uuid.UUID) (<-chan Signal, func(), error) {
	return r.local.SubscribeUserSignals(ctx, userID)
}

func (r *Redis) run() {
	defer close(r.done)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-r.quit:
			cancel()
		case <-ctx.Done():
		}
	}()

	delay := r.cfg.retryMin
	for {
		err := r.read(ctx)
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			delay = r.cfg.retryMin
			continue
		}

		r.resumed = true
		r.log.Warn("chat fan-out read failed, retrying", "error", err, "retry_in", delay)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay = min(delay*2, r.cfg.retryMax)
	}
}

func (r *Redis) read(ctx context.Context) error {
	if r.cursor == nil {
		if err := r.position(ctx); err != nil {
			return err
		}
	}
	if r.resumed {
		if err := r.checkGaps(ctx); err != nil {
			return err
		}
		r.resumed = false
		r.log.Info("chat fan-out reconnected, continuing from the last processed entries")
	}

	streams, err := r.client.XRead(ctx, &goredis.XReadArgs{
		Streams: append(append(make([]string, 0, 2*len(r.streams)), r.streams...), r.cursor...),
		Count:   readBatch,
		Block:   r.cfg.block,
	}).Result()
	if errors.Is(err, goredis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("pubsub: read fan-out: %w", err)
	}

	for _, stream := range streams {
		shard, ok := r.shardOf[stream.Stream]
		if !ok {
			continue
		}
		for _, entry := range stream.Messages {
			if err := r.dispatch(entry); err != nil {
				r.log.Error("could not deliver a fan-out entry", "stream", stream.Stream, "id", entry.ID, "error", err)
			}
			r.cursor[shard] = entry.ID
		}
	}
	return nil
}

func (r *Redis) position(ctx context.Context) error {
	pipe := r.client.Pipeline()
	last := make([]*goredis.XMessageSliceCmd, len(r.streams))
	for i, stream := range r.streams {
		last[i] = pipe.XRevRangeN(ctx, stream, "+", "-", 1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pubsub: find fan-out stream ends: %w", err)
	}

	cursor := make([]string, len(r.streams))
	for i, cmd := range last {
		cursor[i] = streamStart
		if entries := cmd.Val(); len(entries) > 0 {
			cursor[i] = entries[0].ID
		}
	}
	r.cursor = cursor
	return nil
}

func (r *Redis) checkGaps(ctx context.Context) error {
	pipe := r.client.Pipeline()
	oldest := make([]*goredis.XMessageSliceCmd, len(r.streams))
	for i, stream := range r.streams {
		oldest[i] = pipe.XRangeN(ctx, stream, "-", "+", 1)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("pubsub: check fan-out gaps: %w", err)
	}

	for i, cmd := range oldest {
		entries := cmd.Val()
		if len(entries) == 0 || r.cursor[i] == streamStart {
			continue
		}
		if streamIDBefore(r.cursor[i], entries[0].ID) {
			r.log.Warn("chat fan-out resumed after its last entry was trimmed, events in between may be lost",
				"stream", r.streams[i], "last_id", r.cursor[i], "oldest_id", entries[0].ID)
		}
	}
	return nil
}

func streamIDBefore(a, b string) bool {
	aMS, aSeq := splitStreamID(a)
	bMS, bSeq := splitStreamID(b)
	if aMS != bMS {
		return aMS < bMS
	}
	return aSeq < bSeq
}

func splitStreamID(id string) (uint64, uint64) {
	ms, seq, _ := strings.Cut(id, "-")
	msValue, _ := strconv.ParseUint(ms, 10, 64)
	seqValue, _ := strconv.ParseUint(seq, 10, 64)
	return msValue, seqValue
}

func (r *Redis) dispatch(entry goredis.XMessage) error {
	kind, _ := entry.Values[fieldKind].(string)
	payload, _ := entry.Values[fieldPayload].(string)

	switch kind {
	case entryMessage:
		var w wire
		if err := json.Unmarshal([]byte(payload), &w); err != nil {
			return fmt.Errorf("pubsub: decode message: %w", err)
		}
		m, err := w.toDomain()
		if err != nil {
			return err
		}
		r.local.deliver(Delivery{Message: m, Update: w.Update})
		return nil

	case entryRoomSignal, entryUserSignal:
		raw, _ := entry.Values[fieldTo].(string)
		to, err := uuid.Parse(raw)
		if err != nil {
			return fmt.Errorf("pubsub: signal recipient: %w", err)
		}
		var w signalWire
		if err := json.Unmarshal([]byte(payload), &w); err != nil {
			return fmt.Errorf("pubsub: decode %s: %w", kind, err)
		}
		sig, err := w.toDomain()
		if err != nil {
			return err
		}
		if kind == entryRoomSignal {
			r.local.signals.publish(to, sig)
		} else {
			r.local.userSignals.publish(to, sig)
		}
		return nil
	}

	return fmt.Errorf("pubsub: unknown fan-out entry kind %q", kind)
}

func (r *Redis) Close() error {
	r.closeOnce.Do(func() { close(r.quit) })
	<-r.done
	return nil
}
