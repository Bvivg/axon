package session

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
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
	FactLeft
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
	seeds func() uint64
}

func NewRules(games *engine.Registry) Rules { return Rules{games: games, seeds: secretSeed} }

func (r Rules) WithSeeds(seeds func() uint64) Rules {
	r.seeds = seeds
	return r
}

func secretSeed() uint64 {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	return binary.LittleEndian.Uint64(raw[:])
}

func (r Rules) deal(key engine.Key, players []engine.PlayerID, options json.RawMessage) (engine.State, error) {
	seeds := r.seeds
	if seeds == nil {
		seeds = secretSeed
	}
	return r.games.NewTableState(key, players, seeds(), options)
}

func roster(users []uuid.UUID) []engine.PlayerID {
	players := make([]engine.PlayerID, len(users))
	for seat, user := range users {
		players[seat] = engine.PlayerID(user.String())
	}
	return players
}

func (r Rules) begin(game Session, now time.Time) (Session, error) {
	users := game.Players()
	if len(users) < DefaultSeats {
		return Session{}, ErrNotPlaying
	}
	state, err := r.deal(game.Game, roster(users), game.Options)
	if err != nil {
		return Session{}, err
	}
	opponent := users[1]
	game.Opponent, game.State, game.Clock = &opponent, state, r.clock(game.Game, now, len(users))
	return game, nil
}

func (r Rules) Games() *engine.Registry { return r.games }

func (r Rules) clock(game engine.Key, now time.Time, seats int) *Clock {
	return NewClock(now, r.games.TimeLimit(game).Milliseconds(), seats)
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

func (r Rules) table(key engine.Key, table Table) (Table, error) {
	table.Seats = max(table.Seats, DefaultSeats)
	if err := r.games.ValidateTable(key, table.Seats, table.Options); err != nil {
		return Table{}, fmt.Errorf("%w: %w", ErrInvalidTable, err)
	}
	if table.Bots < 0 || table.Bots >= table.Seats {
		return Table{}, fmt.Errorf("%w: %d bots at %d seats", ErrInvalidTable, table.Bots, table.Seats)
	}
	if table.Bots > 0 && !r.Botted(key) {
		return Table{}, ErrNoBots
	}
	return table, nil
}

func (r Rules) Botted(key engine.Key) bool {
	game, err := r.games.Get(key)
	if err != nil {
		return false
	}
	_, ok := game.(engine.Botted)
	return ok
}

func (r Rules) Create(user, request uuid.UUID, key engine.Key, table Table, now time.Time) (Session, Change, error) {
	if _, err := r.games.Get(key); err != nil {
		return Session{}, Change{}, err
	}
	game := Session{ID: SessionID(user, request), Game: key, Creator: user}
	change := Change{Fact: FactCreated, Actor: user, Request: request, At: now}
	if table.Plain() {
		return game, change, nil
	}
	table, err := r.table(key, table)
	if err != nil {
		return Session{}, Change{}, err
	}
	game.Seats, game.Options = table.Seats, table.Options
	game.Seated = []uuid.UUID{user}
	for seat := 1; seat <= table.Bots; seat++ {
		game.Seated = append(game.Seated, BotID(seat))
	}
	if len(game.Seated) == game.Seats {
		if game, err = r.begin(game, now); err != nil {
			return Session{}, Change{}, err
		}
	}
	return game, change, nil
}

func (r Rules) Start(id uuid.UUID, key engine.Key, players []uuid.UUID, table Table, rated bool, now time.Time) (Session, Change, error) {
	if len(players) < DefaultSeats {
		return Session{}, Change{}, fmt.Errorf("%w: %d players", ErrInvalidTable, len(players))
	}
	game := Session{ID: id, Game: key, Creator: players[0], Rated: rated}
	if len(players) > DefaultSeats || !table.Plain() {
		table.Seats = len(players)
		checked, err := r.table(key, table)
		if err != nil {
			return Session{}, Change{}, err
		}
		game.Seats, game.Options, game.Seated = checked.Seats, checked.Options, slices.Clone(players)
	} else {
		opponent := players[1]
		game.Opponent = &opponent
	}
	game, err := r.begin(game, now)
	if err != nil {
		return Session{}, Change{}, err
	}
	return game, Change{Fact: FactCreated, Actor: game.Creator, At: now}, nil
}

func (r Rules) StartTournament(id uuid.UUID, key engine.Key, tournament, creator, opponent uuid.UUID, now time.Time) (Session, Change, error) {
	game, change, err := r.Start(id, key, []uuid.UUID{creator, opponent}, Table{}, true, now)
	if err != nil {
		return Session{}, Change{}, err
	}
	game.Tournament = &tournament
	return game, change, nil
}

func (r Rules) Forfeit(game Session, now time.Time) (Session, Change, error) {
	if game.Opponent == nil {
		return Session{}, Change{}, ErrNotPlaying
	}
	if game.Multiseat() {
		return game, Change{}, nil
	}
	if next, change := r.Expire(game, now); change.Recorded() {
		return next, change, nil
	}
	seat := game.State.Turn
	if game.State.Over() || seat < 0 || seat >= len(game.State.Players) || game.State.Ply > seat {
		return game, Change{}, nil
	}
	absent, err := uuid.Parse(string(game.State.Players[seat]))
	if err != nil {
		return Session{}, Change{}, fmt.Errorf("session: forfeit %s: %w", game.ID, err)
	}
	game.Clock = game.ClockAt(now)
	game.State = game.State.Finish(1 - seat)
	game.Resigned, game.DrawOffer = true, nil
	return game, Change{Fact: FactResigned, Actor: absent, At: now}, nil
}

func (r Rules) Abort(game Session, now time.Time) (Session, Change) {
	if game.Opponent != nil && game.State.Over() {
		return game, Change{}
	}
	return game, Change{Fact: FactCancelled, At: now}
}

func (r Rules) Join(game Session, user uuid.UUID, now time.Time) (Session, Change, error) {
	if game.HasPlayer(user) {
		return game, Change{}, nil
	}
	if game.Started() || len(game.Players()) >= game.Capacity() || IsBot(user) {
		return Session{}, Change{}, ErrFull
	}
	seated := slices.Concat(game.Players(), []uuid.UUID{user})
	if game.Seats > 0 {
		game.Seated = seated
	}
	if len(seated) == game.Capacity() {
		game.Seated = seated
		next, err := r.begin(game, now)
		if err != nil {
			return Session{}, Change{}, err
		}
		if game.Seats == 0 {
			next.Seated = nil
		}
		game = next
	}
	return game, Change{Fact: FactJoined, Actor: user, At: now}, nil
}

func (r Rules) FillWithBots(game Session, user uuid.UUID, now time.Time) (Session, Change, error) {
	if !game.HasPlayer(user) {
		return Session{}, Change{}, ErrNotFound
	}
	if game.Creator != user {
		return Session{}, Change{}, ErrNotHost
	}
	if game.Started() {
		return game, Change{}, nil
	}
	if !r.Botted(game.Game) {
		return Session{}, Change{}, ErrNoBots
	}
	seated := slices.Clone(game.Players())
	for seat := len(seated); seat < game.Capacity(); seat++ {
		seated = append(seated, BotID(seat))
	}
	game.Seats, game.Seated = game.Capacity(), seated
	game, err := r.begin(game, now)
	if err != nil {
		return Session{}, Change{}, err
	}
	return game, Change{Fact: FactJoined, Actor: user, At: now}, nil
}

func (r Rules) BotChoice(game Session) (uuid.UUID, engine.Move, bool) {
	if !game.Started() || game.State.Over() {
		return uuid.Nil, engine.Move{}, false
	}
	players := game.Players()
	order := make([]int, 0, len(players))
	if game.State.Turn >= 0 && game.State.Turn < len(players) {
		order = append(order, game.State.Turn)
	}
	for seat := range players {
		if seat != game.State.Turn {
			order = append(order, seat)
		}
	}
	seed := game.State.Seed ^ uint64(game.State.Ply+1)*0x9e3779b97f4a7c15
	for _, seat := range order {
		if !game.Automated(seat) {
			continue
		}
		if move, ok := r.games.BotMove(game.State, engine.PlayerID(players[seat].String()), seed); ok {
			return players[seat], move, true
		}
	}
	return uuid.Nil, engine.Move{}, false
}

func (r Rules) Bot(game Session, now time.Time) (Session, Change, error) {
	if !game.Started() || game.State.Over() {
		return game, Change{}, nil
	}
	if next, change := r.Expire(game, now); change.Recorded() {
		return next, change, nil
	}
	player, move, ok := r.BotChoice(game)
	if !ok {
		return game, Change{}, nil
	}
	rules, err := r.games.Get(game.Game)
	if err != nil {
		return Session{}, Change{}, err
	}
	return play(rules, game, player, move.Data, now)
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
	if seat, _ := game.SeatOf(user); slices.Contains(game.Left, seat) {
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
	if game.Multiseat() {
		return Session{}, Change{}, ErrTwoPlayers
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
	if r.games.Hidden(game.Game) || game.Multiseat() {
		return Session{}, Change{}, ErrNoUndo
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
	clock := r.clock(game.Game, now, len(game.Players()))
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

func (r Rules) AdjustClock(game Session, remaining []int64, now time.Time) (Session, Change, error) {
	if game.Opponent == nil || game.State.Over() || game.Clock == nil {
		return Session{}, Change{}, ErrNotPlaying
	}
	if len(remaining) != len(game.Clock.RemainingMS) {
		return Session{}, Change{}, fmt.Errorf("%w: %d clocks for %d seats", ErrInvalidTable, len(remaining), len(game.Clock.RemainingMS))
	}
	game.Clock = &Clock{RemainingMS: slices.Clone(remaining), StartedAt: now}
	return game, Change{Fact: FactClockAdjusted, At: now}, nil
}

func (r Rules) opening(game Session) (engine.State, error) {
	state, err := r.games.NewTableState(game.Game, game.State.Players, game.State.Seed, game.Options)
	if err != nil {
		return state, err
	}
	state.MaxPlies = game.State.MaxPlies
	return state, nil
}

func (r Rules) Replay(game Session, moves []MoveRecord) (engine.State, error) {
	state, err := r.opening(game)
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

func (r Rules) Frames(ctx context.Context, game Session, moves []MoveRecord, visit func(engine.State) error) error {
	state, err := r.opening(game)
	if err != nil {
		return err
	}
	rules, err := r.games.Get(game.Game)
	if err != nil {
		return err
	}
	if err := visit(state); err != nil {
		return err
	}
	for _, move := range moves {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("session: replay: %w", err)
		}
		if move.Ply != state.Ply+1 {
			return fmt.Errorf("%w: move %d follows position %d", ErrHistoryGap, move.Ply, state.Ply)
		}
		state, err = rules.ApplyMove(state, engine.Move{Player: engine.PlayerID(move.Player.String()), Ply: state.Ply, Data: move.Data})
		if err != nil {
			return fmt.Errorf("session: replay move %d: %w", move.Ply, err)
		}
		if err := visit(state); err != nil {
			return err
		}
	}
	return nil
}

func (r Rules) Leave(game Session, user uuid.UUID, now time.Time) (Session, Change, error) {
	if !game.HasPlayer(user) {
		return Session{}, Change{}, ErrNotFound
	}
	if game.Opponent == nil && user != game.Creator {
		game.Seated = slices.DeleteFunc(slices.Clone(game.Players()), func(id uuid.UUID) bool { return id == user })
		return game, Change{Fact: FactLeft, Actor: user, At: now}, nil
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
	if game.Multiseat() {
		if game.Out(seat) || slices.Contains(game.Left, seat) {
			return game, Change{}, nil
		}
		game.Clock = game.ClockAt(now)
		game.Left = slices.Concat(game.Left, []int{seat})
		return game, Change{Fact: FactLeft, Actor: user, At: now}, nil
	}
	game.Clock = game.ClockAt(now)
	game.State = game.State.Finish(1 - seat)
	game.Resigned, game.DrawOffer = true, nil
	return game, Change{Fact: FactResigned, Actor: user, At: now}, nil
}
