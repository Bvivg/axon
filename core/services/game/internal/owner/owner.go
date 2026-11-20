package owner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/session"
)

var (
	ErrNotOwner = errors.New("owner: partition is not served by this instance")
	ErrUnknown  = errors.New("owner: session does not exist")
)

type Projection struct {
	Offset     int64
	Generation int64
}

type ReadModel interface {
	Claim(ctx context.Context, partition int32, owner, address string) (int64, error)
	Projected(ctx context.Context, partition int32) (Projection, error)
	Open(ctx context.Context, partition, partitions int32) ([]session.Session, error)
	Load(ctx context.Context, id uuid.UUID) (session.Session, error)
	MoveBefore(ctx context.Context, id uuid.UUID, ply int) (session.MoveRecord, bool, error)
	Moves(ctx context.Context, id uuid.UUID, below int) ([]session.MoveRecord, error)
}

type Listener interface {
	Changed(id uuid.UUID)
	Moved(id uuid.UUID)
}

type Config struct {
	ID       string
	Address  string
	Log      journal.Log
	Model    ReadModel
	Rules    session.Rules
	Listener Listener
	Now      func() time.Time
}

type Owner struct {
	cfg   Config
	mu    sync.RWMutex
	parts map[int32]*part
}

type part struct {
	id       int32
	epoch    int64
	mu       sync.Mutex
	serving  bool
	cells    map[uuid.UUID]*cell
	overlay  map[uuid.UUID]*overlay
	inflight sync.WaitGroup
}

type overlay struct {
	game      session.Session
	cancelled bool
	moves     map[int]session.MoveRecord
}

type cell struct {
	mu      sync.Mutex
	loaded  bool
	exists  bool
	game    session.Session
	history map[int]session.MoveRecord
	timer   *time.Timer
}

func New(cfg Config) *Owner {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Owner{cfg: cfg, parts: map[int32]*part{}}
}

func (o *Owner) ID() string { return o.cfg.ID }

func (o *Owner) Listen(listener Listener) { o.cfg.Listener = listener }

func (o *Owner) Partitions() int32 { return o.cfg.Log.Partitions() }

func (o *Owner) PartitionOf(id uuid.UUID) int32 {
	return journal.PartitionOf(id, o.cfg.Log.Partitions())
}

func (o *Owner) Serves(id uuid.UUID) bool { return o.Serving(o.PartitionOf(id)) }

func (o *Owner) Serving(partition int32) bool {
	o.mu.RLock()
	p := o.parts[partition]
	o.mu.RUnlock()
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.serving
}

func (o *Owner) Owned() []int32 {
	o.mu.RLock()
	defer o.mu.RUnlock()
	owned := make([]int32, 0, len(o.parts))
	for id := range o.parts {
		owned = append(owned, id)
	}
	return owned
}

func (o *Owner) Assign(ctx context.Context, partition int32) error {
	o.mu.Lock()
	if _, ok := o.parts[partition]; ok {
		o.mu.Unlock()
		return nil
	}
	p := &part{id: partition, cells: map[uuid.UUID]*cell{}, overlay: map[uuid.UUID]*overlay{}}
	o.parts[partition] = p
	o.mu.Unlock()
	if err := o.claim(ctx, p); err != nil {
		o.mu.Lock()
		delete(o.parts, partition)
		o.mu.Unlock()
		return fmt.Errorf("owner: claim partition %d: %w", partition, err)
	}
	return nil
}

func (o *Owner) claim(ctx context.Context, p *part) error {
	epoch, err := o.cfg.Model.Claim(ctx, p.id, o.cfg.ID, o.cfg.Address)
	if err != nil {
		return err
	}
	marker, err := o.cfg.Log.Append(ctx, p.id, journal.Entry{Claim: &journal.Claim{Generation: epoch, Owner: o.cfg.ID, At: o.cfg.Now()}})
	if err != nil {
		return err
	}
	projected, err := o.cfg.Model.Projected(ctx, p.id)
	if err != nil {
		return err
	}
	fence := journal.NewFence()
	fence.Seed(p.id, projected.Generation)
	if projected.Offset < marker {
		err = o.cfg.Log.Read(ctx, p.id, projected.Offset, marker, func(entry journal.Entry) error {
			if !fence.Admit(entry) {
				return nil
			}
			p.absorb(*entry.Event)
			return nil
		})
		if err != nil {
			return err
		}
	}
	open, err := o.cfg.Model.Open(ctx, p.id, o.cfg.Log.Partitions())
	if err != nil {
		return err
	}
	o.mu.RLock()
	current := o.parts[p.id] == p
	o.mu.RUnlock()
	if !current {
		return ErrNotOwner
	}
	p.mu.Lock()
	p.epoch = epoch
	for _, game := range open {
		if _, ok := p.overlay[game.ID]; ok {
			continue
		}
		p.cells[game.ID] = &cell{loaded: true, exists: true, game: game, history: map[int]session.MoveRecord{}}
	}
	for id, item := range p.overlay {
		p.cells[id] = &cell{loaded: true, exists: !item.cancelled, game: item.game, history: item.copyMoves()}
	}
	cells := make(map[uuid.UUID]*cell, len(p.cells))
	for id, c := range p.cells {
		cells[id] = c
	}
	p.serving = true
	p.mu.Unlock()
	for id, c := range cells {
		c.mu.Lock()
		o.schedule(id, c)
		c.mu.Unlock()
	}
	return nil
}

func (p *part) absorb(event journal.Event) {
	id := event.Session.ID
	item := p.overlay[id]
	if item == nil {
		item = &overlay{moves: map[int]session.MoveRecord{}}
		p.overlay[id] = item
	}
	if event.Session.Revision < item.game.Revision {
		return
	}
	item.game = event.Session
	switch event.Change.Fact {
	case session.FactMoved:
		item.moves[event.Change.Move.Ply] = *event.Change.Move
	case session.FactUndone:
		delete(item.moves, event.Change.UndonePly)
	case session.FactCancelled:
		item.cancelled = true
	}
}

func (item *overlay) copyMoves() map[int]session.MoveRecord {
	out := make(map[int]session.MoveRecord, len(item.moves))
	for ply, move := range item.moves {
		out[ply] = move
	}
	return out
}

func (o *Owner) Revoke(partition int32) {
	o.mu.Lock()
	p := o.parts[partition]
	delete(o.parts, partition)
	o.mu.Unlock()
	if p == nil {
		return
	}
	p.mu.Lock()
	p.serving = false
	p.mu.Unlock()
	p.inflight.Wait()
	p.mu.Lock()
	cells := p.cells
	p.cells = map[uuid.UUID]*cell{}
	p.mu.Unlock()
	for id, c := range cells {
		c.mu.Lock()
		if c.timer != nil {
			c.timer.Stop()
		}
		c.mu.Unlock()
		if o.cfg.Listener != nil {
			o.cfg.Listener.Moved(id)
		}
	}
}

func (o *Owner) RevokeAll() {
	for _, partition := range o.Owned() {
		o.Revoke(partition)
	}
}

type step func(c *cell, now time.Time) (session.Session, session.Change, error)

func (o *Owner) do(ctx context.Context, id uuid.UUID, run step) (session.Session, error) {
	o.mu.RLock()
	p := o.parts[o.PartitionOf(id)]
	o.mu.RUnlock()
	if p == nil {
		return session.Session{}, ErrNotOwner
	}
	p.mu.Lock()
	if !p.serving {
		p.mu.Unlock()
		return session.Session{}, ErrNotOwner
	}
	p.inflight.Add(1)
	c := p.cells[id]
	if c == nil {
		c = &cell{history: map[int]session.MoveRecord{}}
		p.cells[id] = c
	}
	epoch := p.epoch
	p.mu.Unlock()
	defer p.inflight.Done()

	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.loaded {
		game, err := o.cfg.Model.Load(ctx, id)
		switch {
		case errors.Is(err, session.ErrNotFound):
		case err != nil:
			return session.Session{}, err
		default:
			c.game, c.exists = game, true
		}
		c.loaded = true
	}
	next, change, err := run(c, o.cfg.Now())
	if err != nil {
		return session.Session{}, err
	}
	if !change.Recorded() {
		return next, nil
	}
	next = session.Record(next, change)
	if _, err := o.cfg.Log.Append(ctx, p.id, journal.Entry{Event: &journal.Event{Session: next, Change: change, Generation: epoch}}); err != nil {
		return session.Session{}, fmt.Errorf("owner: append session event: %w", err)
	}
	c.game = next
	c.exists = change.Fact != session.FactCancelled
	switch change.Fact {
	case session.FactMoved:
		c.history[change.Move.Ply] = *change.Move
	case session.FactUndone:
		delete(c.history, change.UndonePly)
	}
	o.schedule(id, c)
	if o.cfg.Listener != nil {
		o.cfg.Listener.Changed(id)
	}
	return next, nil
}

func (o *Owner) schedule(id uuid.UUID, c *cell) {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	game := c.game
	if !c.exists || game.Clock == nil || game.State.Over() || game.State.Turn < 0 || game.State.Turn >= len(game.Clock.RemainingMS) {
		return
	}
	deadline := game.Clock.StartedAt.Add(time.Duration(game.Clock.RemainingMS[game.State.Turn]) * time.Millisecond)
	wait := max(time.Until(deadline), 0) + 5*time.Millisecond
	c.timer = time.AfterFunc(wait, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = o.Expire(ctx, id)
	})
}

func (o *Owner) existing(c *cell) (session.Session, error) {
	if !c.exists {
		return session.Session{}, session.ErrNotFound
	}
	return c.game, nil
}

func (o *Owner) Create(ctx context.Context, user, request uuid.UUID, key engine.Key) (session.Session, error) {
	return o.do(ctx, session.SessionID(user, request), func(c *cell, now time.Time) (session.Session, session.Change, error) {
		if c.exists {
			return c.game, session.Change{}, nil
		}
		return o.cfg.Rules.Create(user, request, key, now)
	})
}

func (o *Owner) Start(ctx context.Context, id uuid.UUID, key engine.Key, creator, opponent uuid.UUID, rated bool) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		if c.exists {
			return c.game, session.Change{}, nil
		}
		return o.cfg.Rules.Start(id, key, creator, opponent, rated, now)
	})
}

func (o *Owner) Get(ctx context.Context, id, user uuid.UUID) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		return o.cfg.Rules.Get(game, user, now)
	})
}

func (o *Owner) Join(ctx context.Context, id, user uuid.UUID) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		return o.cfg.Rules.Join(game, user, now)
	})
}

func (o *Owner) Move(ctx context.Context, id, user uuid.UUID, ply int, kind engine.Key, data json.RawMessage, revision *uint32) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		return o.cfg.Rules.Move(game, user, ply, kind, data, revision, now)
	})
}

func (o *Owner) Draw(ctx context.Context, id, user uuid.UUID, action session.DrawAction) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		return o.cfg.Rules.Draw(game, user, action, now)
	})
}

func (o *Owner) Undo(ctx context.Context, id, user uuid.UUID, revision *uint32) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		return o.cfg.Rules.Undo(game, user, revision, func(game session.Session) (engine.State, *session.Clock, error) {
			return o.previous(ctx, c, game)
		}, now)
	})
}

func (o *Owner) previous(ctx context.Context, c *cell, game session.Session) (engine.State, *session.Clock, error) {
	ply := game.State.Ply
	if move, ok := c.history[ply]; ok && move.StateBefore.Game != "" {
		return move.StateBefore, move.ClockBefore, nil
	}
	move, found, err := o.cfg.Model.MoveBefore(ctx, game.ID, ply)
	if err != nil {
		return engine.State{}, nil, err
	}
	if found && move.StateBefore.Game != "" {
		return move.StateBefore, move.ClockBefore, nil
	}
	moves, err := o.cfg.Model.Moves(ctx, game.ID, ply)
	if err != nil {
		return engine.State{}, nil, err
	}
	byPly := make(map[int]session.MoveRecord, len(moves)+len(c.history))
	for _, move := range moves {
		byPly[move.Ply] = move
	}
	for recent, move := range c.history {
		if recent < ply {
			byPly[recent] = move
		}
	}
	merged := make([]session.MoveRecord, 0, len(byPly))
	for _, move := range byPly {
		merged = append(merged, move)
	}
	slices.SortFunc(merged, func(a, b session.MoveRecord) int { return a.Ply - b.Ply })
	state, err := o.cfg.Rules.Replay(game, merged)
	var clock *session.Clock
	if found {
		clock = move.ClockBefore
	}
	return state, clock, err
}

func (o *Owner) Leave(ctx context.Context, id, user uuid.UUID) (session.Session, bool, error) {
	cancelled := false
	game, err := o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		next, change, err := o.cfg.Rules.Leave(game, user, now)
		cancelled = change.Fact == session.FactCancelled
		return next, change, err
	})
	return game, cancelled, err
}

func (o *Owner) AdjustClock(ctx context.Context, id uuid.UUID, remaining [2]int64) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		return o.cfg.Rules.AdjustClock(game, remaining, now)
	})
}

func (o *Owner) Expire(ctx context.Context, id uuid.UUID) (session.Session, error) {
	return o.do(ctx, id, func(c *cell, now time.Time) (session.Session, session.Change, error) {
		game, err := o.existing(c)
		if err != nil {
			return session.Session{}, session.Change{}, err
		}
		next, change := o.cfg.Rules.Expire(game, now)
		return next, change, nil
	})
}
