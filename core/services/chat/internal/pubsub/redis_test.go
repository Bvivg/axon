package pubsub

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"

	"github.com/bvivg/axon/core/services/chat/internal/domain"
)

const waitFor = 5 * time.Second

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newClient(t *testing.T, addr string) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:            addr,
		DialTimeout:     time.Second,
		ReadTimeout:     time.Second,
		WriteTimeout:    time.Second,
		MaxRetries:      -1,
		DisableIdentity: true,
	})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func newFanout(t *testing.T, addr string, log *slog.Logger, opts ...Option) *Redis {
	t.Helper()
	opts = append([]Option{WithBlock(100 * time.Millisecond), withRetry(10*time.Millisecond, 50*time.Millisecond)}, opts...)
	bus, err := NewRedis(newClient(t, addr), log, opts...)
	if err != nil {
		t.Fatalf("NewRedis: %v", err)
	}
	t.Cleanup(func() { _ = bus.Close() })
	return bus
}

func message(room uuid.UUID, seq int64) domain.Message {
	return domain.Message{
		ID:       uuid.New(),
		RoomID:   room,
		AuthorID: uuid.New(),
		Body:     "body",
		Seq:      seq,
		SentAt:   time.Now().UTC().Truncate(time.Millisecond),
	}
}

func next[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("the subscription closed")
		}
		return v
	case <-time.After(waitFor):
		t.Fatal("nothing arrived")
	}
	var zero T
	return zero
}

func silent[T any](t *testing.T, ch <-chan T, d time.Duration) {
	t.Helper()
	select {
	case v := <-ch:
		t.Fatalf("unexpected delivery %+v", v)
	case <-time.After(d):
	}
}

func subscribe(t *testing.T, bus *Redis, room uuid.UUID) <-chan Delivery {
	t.Helper()
	ch, stop, err := bus.Subscribe(t.Context(), room)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	t.Cleanup(stop)
	return ch
}

func TestNewRedisValidatesItsOptions(t *testing.T) {
	server := miniredis.RunT(t)
	client := newClient(t, server.Addr())

	cases := []struct {
		name   string
		client goredis.UniversalClient
		log    *slog.Logger
		opts   []Option
		ok     bool
	}{
		{name: "defaults", client: client, log: discard(), ok: true},
		{name: "one shard", client: client, log: discard(), opts: []Option{WithShards(1)}, ok: true},
		{name: "no client", log: discard()},
		{name: "no logger", client: client},
		{name: "zero shards", client: client, log: discard(), opts: []Option{WithShards(0)}},
		{name: "too many shards", client: client, log: discard(), opts: []Option{WithShards(maxShards + 1)}},
		{name: "zero length", client: client, log: discard(), opts: []Option{WithMaxLen(0)}},
		{name: "no block", client: client, log: discard(), opts: []Option{WithBlock(0)}},
		{name: "inverted retry", client: client, log: discard(), opts: []Option{withRetry(time.Second, time.Millisecond)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus, err := NewRedis(tc.client, tc.log, tc.opts...)
			if bus != nil {
				t.Cleanup(func() { _ = bus.Close() })
			}
			if (err == nil) != tc.ok {
				t.Fatalf("NewRedis error = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestMessagesCrossBetweenInstances(t *testing.T) {
	server := miniredis.RunT(t)
	first, second := newFanout(t, server.Addr(), discard()), newFanout(t, server.Addr(), discard())

	cases := []struct {
		name     string
		from, to *Redis
		update   bool
	}{
		{name: "new message first to second", from: first, to: second},
		{name: "new message second to first", from: second, to: first},
		{name: "update first to second", from: first, to: second, update: true},
		{name: "update second to first", from: second, to: first, update: true},
		{name: "own message", from: first, to: first},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			room := uuid.New()
			ch := subscribe(t, tc.to, room)

			sent := message(room, 7)
			publish := tc.from.Publish
			if tc.update {
				publish = tc.from.PublishUpdate
			}
			if err := publish(t.Context(), sent); err != nil {
				t.Fatalf("publish: %v", err)
			}

			got := next(t, ch)
			if got.Update != tc.update {
				t.Errorf("update = %v, want %v", got.Update, tc.update)
			}
			if got.Message.ID != sent.ID || got.Message.Seq != sent.Seq || !got.Message.SentAt.Equal(sent.SentAt) {
				t.Errorf("received %+v, want %+v", got.Message, sent)
			}
		})
	}
}

func TestSignalsCrossBetweenInstances(t *testing.T) {
	server := miniredis.RunT(t)
	first, second := newFanout(t, server.Addr(), discard()), newFanout(t, server.Addr(), discard())

	type subscribeFunc func(*Redis, context.Context, uuid.UUID) (<-chan Signal, func(), error)
	room, user := uuid.New(), uuid.New()

	cases := []struct {
		name      string
		from, to  *Redis
		subscribe subscribeFunc
		topic     uuid.UUID
		toUser    bool
		want      Signal
	}{
		{
			name: "room signal", from: first, to: second, subscribe: (*Redis).SubscribeSignals, topic: room,
			want: Signal{Kind: SignalTyping, RoomID: room, UserID: user},
		},
		{
			name: "read receipt back", from: second, to: first, subscribe: (*Redis).SubscribeSignals, topic: room,
			want: Signal{Kind: SignalRead, RoomID: room, UserID: user, Seq: 9},
		},
		{
			name: "user signal", from: first, to: second, subscribe: (*Redis).SubscribeUserSignals, topic: user, toUser: true,
			want: Signal{Kind: SignalRoomAdded, RoomID: room, UserID: uuid.New(), Payload: `{"title":"x"}`},
		},
		{
			name: "call state", from: second, to: first, subscribe: (*Redis).SubscribeUserSignals, topic: user, toUser: true,
			want: Signal{Kind: SignalCallState, UserID: user, EventID: uuid.New()},
		},
		{
			name: "call peer", from: first, to: second, subscribe: (*Redis).SubscribeUserSignals, topic: user, toUser: true,
			want: Signal{Kind: SignalCallPeer, UserID: user, EndpointID: uuid.New(), Payload: `{"sdp":"offer"}`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, stop, err := tc.subscribe(tc.to, t.Context(), tc.topic)
			if err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			defer stop()

			if tc.toUser {
				err = tc.from.PublishUserSignal(t.Context(), tc.topic, tc.want)
			} else {
				err = tc.from.PublishSignal(t.Context(), tc.want)
			}
			if err != nil {
				t.Fatalf("publish: %v", err)
			}

			if got := next(t, ch); got != tc.want {
				t.Errorf("received %+v, want %+v", got, tc.want)
			}
			silent(t, ch, 50*time.Millisecond)
		})
	}
}

func TestRoomOrderSurvivesInterleaving(t *testing.T) {
	server := miniredis.RunT(t)

	cases := []struct {
		name   string
		shards int
		rooms  int
		each   int
	}{
		{name: "one shard", shards: 1, rooms: 4, each: 15},
		{name: "default shards", shards: defaultShards, rooms: 6, each: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writer := newFanout(t, server.Addr(), discard(), WithShards(tc.shards))
			reader := newFanout(t, server.Addr(), discard(), WithShards(tc.shards))

			rooms := make([]uuid.UUID, tc.rooms)
			channels := make([]<-chan Delivery, tc.rooms)
			for i := range rooms {
				rooms[i] = uuid.New()
				channels[i] = subscribe(t, reader, rooms[i])
			}

			for seq := 1; seq <= tc.each; seq++ {
				for _, room := range rooms {
					if err := writer.Publish(t.Context(), message(room, int64(seq))); err != nil {
						t.Fatalf("Publish: %v", err)
					}
				}
			}

			for i, ch := range channels {
				for seq := 1; seq <= tc.each; seq++ {
					if got := next(t, ch).Message; got.Seq != int64(seq) || got.RoomID != rooms[i] {
						t.Fatalf("room %d: got seq %d of %s, want %d", i, got.Seq, got.RoomID, seq)
					}
				}
			}
		})
	}
}

func TestAnInstanceStartsAtTheEndOfTheStreams(t *testing.T) {
	server := miniredis.RunT(t)
	writer := newFanout(t, server.Addr(), discard())
	room := uuid.New()

	if err := writer.Publish(t.Context(), message(room, 1)); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	late := newFanout(t, server.Addr(), discard())
	ch := subscribe(t, late, room)

	if err := writer.Publish(t.Context(), message(room, 2)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got := next(t, ch).Message.Seq; got != 2 {
		t.Fatalf("the late instance first received seq %d, want 2", got)
	}
	silent(t, ch, 100*time.Millisecond)
}

func TestEventsPublishedDuringAnOutageArriveAfterReconnect(t *testing.T) {
	server := miniredis.RunT(t)
	link := newLink(t, server.Addr())

	writer := newFanout(t, server.Addr(), discard())
	reader := newFanout(t, link.addr(), discard())

	room, user := uuid.New(), uuid.New()
	messages := subscribe(t, reader, room)

	if err := writer.Publish(t.Context(), message(room, 1)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if got := next(t, messages).Message.Seq; got != 1 {
		t.Fatalf("got seq %d before the outage, want 1", got)
	}

	link.cut()

	userSignals, stop, err := reader.SubscribeUserSignals(t.Context(), user)
	if err != nil {
		t.Fatalf("SubscribeUserSignals during the outage: %v", err)
	}
	defer stop()

	for seq := int64(2); seq <= 6; seq++ {
		if err := writer.Publish(t.Context(), message(room, seq)); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	call := Signal{Kind: SignalCallState, UserID: user, EventID: uuid.New()}
	if err := writer.PublishUserSignal(t.Context(), user, call); err != nil {
		t.Fatalf("PublishUserSignal: %v", err)
	}

	silent(t, messages, 300*time.Millisecond)

	link.restore()

	for seq := int64(2); seq <= 6; seq++ {
		if got := next(t, messages).Message.Seq; got != seq {
			t.Fatalf("after reconnect got seq %d, want %d", got, seq)
		}
	}
	if got := next(t, userSignals); got != call {
		t.Fatalf("after reconnect got %+v, want %+v", got, call)
	}
	silent(t, messages, 100*time.Millisecond)
}

func TestATrimmedGapIsReportedAndTheReaderContinuesFromTheOldestEntry(t *testing.T) {
	server := miniredis.RunT(t)
	link := newLink(t, server.Addr())
	logs := &records{}

	const maxLen = 5
	writer := newFanout(t, server.Addr(), discard(), WithMaxLen(maxLen))
	reader := newFanout(t, link.addr(), slog.New(logs), WithMaxLen(maxLen))

	room := uuid.New()
	messages := subscribe(t, reader, room)

	if err := writer.Publish(t.Context(), message(room, 1)); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	next(t, messages)

	link.cut()
	const last = 20
	for seq := int64(2); seq <= last; seq++ {
		if err := writer.Publish(t.Context(), message(room, seq)); err != nil {
			t.Fatalf("Publish: %v", err)
		}
	}
	silent(t, messages, 200*time.Millisecond)
	link.restore()

	for seq := int64(last - maxLen + 1); seq <= last; seq++ {
		if got := next(t, messages).Message.Seq; got != seq {
			t.Fatalf("after the gap got seq %d, want %d", got, seq)
		}
	}
	silent(t, messages, 100*time.Millisecond)

	if !logs.warned("trimmed") {
		t.Fatalf("the gap was not reported, logs: %v", logs.all())
	}
}

func TestStreamsStayWithinTheirCap(t *testing.T) {
	cases := []struct {
		name      string
		shards    int
		maxLen    int
		published int
	}{
		{name: "single entry cap", shards: 2, maxLen: 1, published: 50},
		{name: "small cap", shards: 4, maxLen: 10, published: 200},
		{name: "under the cap", shards: 4, maxLen: 1000, published: 40},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := miniredis.RunT(t)
			bus := newFanout(t, server.Addr(), discard(), WithShards(tc.shards), WithMaxLen(tc.maxLen))

			for i := 0; i < tc.published; i++ {
				if err := bus.Publish(t.Context(), message(uuid.New(), 1)); err != nil {
					t.Fatalf("Publish: %v", err)
				}
			}

			client := newClient(t, server.Addr())
			total := int64(0)
			for shard := 0; shard < tc.shards; shard++ {
				n, err := client.XLen(t.Context(), fanoutStream(shard)).Result()
				if err != nil {
					t.Fatalf("XLEN: %v", err)
				}
				if n > int64(tc.maxLen) {
					t.Errorf("shard %d holds %d entries, cap %d", shard, n, tc.maxLen)
				}
				total += n
			}
			if tc.published <= tc.maxLen && total != int64(tc.published) {
				t.Errorf("streams hold %d entries, want all %d", total, tc.published)
			}
		})
	}
}

func TestShardIsStablePerTopic(t *testing.T) {
	server := miniredis.RunT(t)
	bus := newFanout(t, server.Addr(), discard())

	seen := make(map[int]bool)
	for i := 0; i < 500; i++ {
		id := uuid.New()
		shard := bus.shard(id)
		if shard < 0 || shard >= defaultShards {
			t.Fatalf("shard %d out of range", shard)
		}
		if bus.shard(id) != shard {
			t.Fatalf("shard of %s changed", id)
		}
		seen[shard] = true
	}
	if len(seen) != defaultShards {
		t.Errorf("500 topics spread over %d of %d shards", len(seen), defaultShards)
	}
}

func TestStreamIDOrder(t *testing.T) {
	cases := []struct {
		a, b   string
		before bool
	}{
		{a: "1-0", b: "2-0", before: true},
		{a: "2-0", b: "1-0"},
		{a: "5-1", b: "5-2", before: true},
		{a: "5-2", b: "5-2"},
		{a: "9-9", b: "10-0", before: true},
		{a: streamStart, b: "1-0", before: true},
	}
	for _, tc := range cases {
		if got := streamIDBefore(tc.a, tc.b); got != tc.before {
			t.Errorf("streamIDBefore(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.before)
		}
	}
}

type records struct {
	mu    sync.Mutex
	lines []string
	warns []string
}

func (r *records) Enabled(context.Context, slog.Level) bool { return true }

func (r *records) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, rec.Level.String()+" "+rec.Message)
	if rec.Level == slog.LevelWarn {
		r.warns = append(r.warns, rec.Message)
	}
	return nil
}

func (r *records) WithAttrs([]slog.Attr) slog.Handler { return r }

func (r *records) WithGroup(string) slog.Handler { return r }

func (r *records) warned(fragment string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, msg := range r.warns {
		if strings.Contains(msg, fragment) {
			return true
		}
	}
	return false
}

func (r *records) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

type link struct {
	ln       net.Listener
	upstream string

	mu    sync.Mutex
	down  bool
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup
}

func newLink(t *testing.T, upstream string) *link {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	l := &link{ln: ln, upstream: upstream, conns: make(map[net.Conn]struct{})}
	l.wg.Add(1)
	go l.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		l.cut()
		l.wg.Wait()
	})
	return l
}

func (l *link) addr() string { return l.ln.Addr().String() }

func (l *link) serve() {
	defer l.wg.Done()
	for {
		conn, err := l.ln.Accept()
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			continue
		}
		if l.isDown() {
			_ = conn.Close()
			continue
		}
		up, err := (&net.Dialer{Timeout: time.Second}).DialContext(context.Background(), "tcp", l.upstream)
		if err != nil {
			_ = conn.Close()
			continue
		}

		l.mu.Lock()
		if l.down {
			l.mu.Unlock()
			_ = conn.Close()
			_ = up.Close()
			continue
		}
		l.conns[conn] = struct{}{}
		l.conns[up] = struct{}{}
		l.wg.Add(2)
		l.mu.Unlock()

		go l.pipe(conn, up)
		go l.pipe(up, conn)
	}
}

func (l *link) pipe(dst, src net.Conn) {
	defer l.wg.Done()
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

func (l *link) isDown() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.down
}

func (l *link) cut() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.down = true
	for conn := range l.conns {
		_ = conn.Close()
	}
	clear(l.conns)
}

func (l *link) restore() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.down = false
}
