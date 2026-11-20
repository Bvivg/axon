package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
)

type Fact int

const (
	FactNone Fact = iota
	FactCreated
	FactJoined
	FactMoved
	FactDrawOffered
	FactDrawDeclined
	FactDrawAgreed
	FactResigned
	FactTimedOut
	FactUndone
	FactCancelled
	FactClockAdjusted
)

type MoveRecord struct {
	Ply         int
	Player      uuid.UUID
	Data        json.RawMessage
	StateBefore engine.State
	ClockBefore *Clock
	PlayedAt    time.Time
}

type Change struct {
	Fact      Fact
	Actor     uuid.UUID
	Request   uuid.UUID
	Move      *MoveRecord
	UndonePly int
	At        time.Time
}

func (c Change) Recorded() bool { return c.Fact != FactNone }

type Rules struct {
	games *engine.Registry
}

func NewRules(games *engine.Registry) Rules { return Rules{games: games} }

func (r Rules) Games() *engine.Registry { return r.games }

func (r Rules) clock(game engine.Key, now time.Time) *Clock {
	return NewClock(now, r.games.TimeLimit(game).Milliseconds())
}

var sessionNamespace = uuid.MustParse("6f0c3c55-2a77-4d0a-9d1e-6b5bd1c39a51")

func SessionID(creator, request uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(sessionNamespace, append(creator[:], request[:]...))
}

func Record(game Session, change Change) Session {
	if !change.Recorded() {
		return game
	}
	game.Revision++
	game.UpdatedAt = change.At
	return game
}

func (r Rules) Create(user, request uuid.UUID, key engine.Key, now time.Time) (Session, Change, error) {
	if _, err := r.games.Get(key); err != nil {
		return Session{}, Change{}, err
	}
	game := Session{ID: SessionID(user, request), Game: key, Creator: user}
	return game, Change{Fact: FactCreated, Actor: user, Request: request, At: now}, nil
}

func (r Rules) Start(id uuid.UUID, key engine.Key, creator, opponent uuid.UUID, rated bool, now time.Time) (Session, Change, error) {
	players := []engine.PlayerID{engine.PlayerID(creator.String()), engine.PlayerID(opponent.String())}
	state, err := r.games.NewState(key, players)
	if err != nil {
		return Session{}, Change{}, err
	}
	game := Session{ID: id, Game: key, Creator: creator, Opponent: &opponent, State: state, Clock: r.clock(key, now), Rated: rated}
	return game, Change{Fact: FactCreated, Actor: creator, At: now}, nil
}

func (r Rules) Join(game Session, user uuid.UUID, now time.Time) (Session, Change, error) {
	if game.HasPlayer(user) {
		return game, Change{}, nil
	}
	if game.Opponent != nil {
		return Session{}, Change{}, ErrFull
	}
	players := []engine.PlayerID{engine.PlayerID(game.Creator.String()), engine.PlayerID(user.String())}
	state, err := r.games.NewState(game.Game, players)
	if err != nil {
		return Session{}, Change{}, err
	}
	game.Opponent, game.State = &user, state
	game.Clock = r.clock(game.Game, now)
	return game, Change{Fact: FactJoined, Actor: user, At: now}, nil
}

func (r Rules) Expire(game Session, now time.Time) (Session, Change) {
	if game.expire(now) {
		return game, Change{Fact: FactTimedOut, At: now}
	}
	return game, Change{}
}

func (r Rules) Get(game Session, user uuid.UUID, now time.Time) (Session, Change, error) {
	if !game.Watchable(user) {
		return Session{}, Change{}, ErrNotFound
	}
	next, change := r.Expire(game, now)
	return next, change, nil
}

func (r Rules) Move(game Session, user uuid.UUID, ply int, kind engine.Key, data json.RawMessage, revision *uint32, now time.Time) (Session, Change, error) {
	if !game.HasPlayer(user) {
		return Session{}, Change{}, ErrNotFound
	}
	if game.Game != kind {
		return Session{}, Change{}, engine.ErrGameMismatch
	}
	if game.Opponent == nil {
		return Session{}, Change{}, ErrNotPlaying
	}
	if ply != game.State.Ply || revision != nil && *revision != game.Revision {
		return Session{}, Change{}, engine.ErrStaleMove
	}
	rules, err := r.games.Get(game.Game)
	if err != nil {
		return Session{}, Change{}, err
	}
	if next, change := r.Expire(game, now); change.Recorded() {
		return next, change, nil
	}
	return play(rules, game, user, data, now)
}

func (r Rules) Redo(game Session, change Change) (Session, Change, error) {
	if change.Fact != FactMoved || change.Move == nil {
		return Session{}, Change{}, ErrNotReplayable
	}
	rules, err := r.games.Get(game.Game)
	if err != nil {
		return Session{}, Change{}, err
	}
	return play(rules, game, change.Actor, change.Move.Data, change.At)
}

func play(rules engine.Game, game Session, user uuid.UUID, data json.RawMessage, now time.Time) (Session, Change, error) {
	beforeState, beforeClock := game.State, game.ClockAt(now)
	move := engine.Move{Player: engine.PlayerID(user.String()), Ply: beforeState.Ply, Data: data}
	next, err := rules.ApplyMove(beforeState, move)
	if err != nil {
		return Session{}, Change{}, err
	}
	game.State, game.Clock, game.DrawOffer = next, beforeClock, nil
	record := &MoveRecord{Ply: next.Ply, Player: user, Data: data, StateBefore: beforeState, ClockBefore: beforeClock}
	return game, Change{Fact: FactMoved, Actor: user, Move: record, At: now}, nil
}

func (r Rules) Draw(game Session, user uuid.UUID, action DrawAction, now time.Time) (Session, Change, error) {
	if !game.HasPlayer(user) {
		return Session{}, Change{}, ErrNotFound
	}
	if game.Opponent == nil || game.State.Over() {
		return Session{}, Change{}, ErrNotPlaying
	}
	if next, change := r.Expire(game, now); change.Recorded() {
		return next, change, nil
	}
	pending := game.DrawOffer != nil && *game.DrawOffer != user
	switch {
	case action == AcceptDraw && pending, action == OfferDraw && pending:
		game.Clock = game.ClockAt(now)
		game.State = game.State.Finish(engine.NoSeat)
		game.AgreedDraw, game.DrawOffer = true, nil
		return game, Change{Fact: FactDrawAgreed, Actor: user, At: now}, nil
	case action == OfferDraw:
		if game.DrawOffer != nil {
			return game, Change{}, nil
		}
		game.Clock = game.ClockAt(now)
		game.DrawOffer = &user
		return game, Change{Fact: FactDrawOffered, Actor: user, At: now}, nil
	case action == DeclineDraw && pending:
		game.Clock = game.ClockAt(now)
		game.DrawOffer = nil
		return game, Change{Fact: FactDrawDeclined, Actor: user, At: now}, nil
	default:
		return Session{}, Change{}, ErrNoDrawOffer
	}
}

type Previous func(game Session) (engine.State, *Clock, error)

func (r Rules) Undo(game Session, user uuid.UUID, revision *uint32, previous Previous, now time.Time) (Session, Change, error) {
	if !game.HasPlayer(user) {
		return Session{}, Change{}, ErrNotFound
	}
	if game.Rated {
		return Session{}, Change{}, ErrRated
	}
	if revision != nil && *revision != game.Revision {
		return Session{}, Change{}, engine.ErrStaleMove
	}
	if game.Opponent == nil || game.State.Ply == 0 {
		return Session{}, Change{}, ErrNotPlaying
	}
	state, before, err := previous(game)
	if err != nil {
		return Session{}, Change{}, err
	}
	clock := r.clock(game.Game, now)
	if before != nil {
		restored := *before
		restored.StartedAt = now
		clock = &restored
	}
	ply := game.State.Ply
	game.State, game.Clock, game.TimedOut = state, clock, false
	game.DrawOffer, game.AgreedDraw = nil, false
	return game, Change{Fact: FactUndone, Actor: user, UndonePly: ply, At: now}, nil
}

func (r Rules) AdjustClock(game Session, remaining [2]int64, now time.Time) (Session, Change, error) {
	if game.Opponent == nil || game.State.Over() || game.Clock == nil {
		return Session{}, Change{}, ErrNotPlaying
	}
	game.Clock = &Clock{RemainingMS: remaining, StartedAt: now}
	return game, Change{Fact: FactClockAdjusted, At: now}, nil
}

func (r Rules) Replay(game Session, moves []MoveRecord) (engine.State, error) {
	state, err := r.games.NewState(game.Game, game.State.Players)
	if err != nil {
		return state, err
	}
	rules, err := r.games.Get(game.Game)
	if err != nil {
		return state, err
	}
	for _, move := range moves {
		state, err = rules.ApplyMove(state, engine.Move{Player: engine.PlayerID(move.Player.String()), Ply: state.Ply, Data: move.Data})
		if err != nil {
			return state, err
		}
	}
	return state, nil
}

func (r Rules) Frames(game Session, moves []MoveRecord) ([]engine.State, error) {
	state, err := r.games.NewState(game.Game, game.State.Players)
	if err != nil {
		return nil, err
	}
	rules, err := r.games.Get(game.Game)
	if err != nil {
		return nil, err
	}
	frames := make([]engine.State, 0, len(moves)+1)
	frames = append(frames, state)
	for _, move := range moves {
		if move.Ply != state.Ply+1 {
			return nil, fmt.Errorf("%w: move %d follows position %d", ErrHistoryGap, move.Ply, state.Ply)
		}
		state, err = rules.ApplyMove(state, engine.Move{Player: engine.PlayerID(move.Player.String()), Ply: state.Ply, Data: move.Data})
		if err != nil {
			return nil, fmt.Errorf("session: replay move %d: %w", move.Ply, err)
		}
		frames = append(frames, state)
	}
	return frames, nil
}

func (r Rules) Leave(game Session, user uuid.UUID, now time.Time) (Session, Change, error) {
	if !game.HasPlayer(user) {
		return Session{}, Change{}, ErrNotFound
	}
	if game.Opponent == nil {
		return game, Change{Fact: FactCancelled, Actor: user, At: now}, nil
	}
	if next, change := r.Expire(game, now); change.Recorded() {
		return next, change, nil
	}
	if game.State.Over() {
		return game, Change{}, nil
	}
	seat, ok := game.State.Seat(engine.PlayerID(user.String()))
	if !ok {
		return Session{}, Change{}, ErrNotFound
	}
	game.Clock = game.ClockAt(now)
	game.State = game.State.Finish(1 - seat)
	game.Resigned, game.DrawOffer = true, nil
	return game, Change{Fact: FactResigned, Actor: user, At: now}, nil
}
