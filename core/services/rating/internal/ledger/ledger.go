package ledger

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

const (
	FinishedTopic = "game.finished.v1"
	ChangedTopic  = "rating.changed.v1"
	OutboxTable   = "rating.outbox"
)

var (
	ErrUnrated = errors.New("ledger: unrated game")
	ErrForeign = errors.New("ledger: not a game.finished event")
	ErrInvalid = errors.New("ledger: invalid game.finished event")
)

var (
	finishedType     = string((&eventsv1.GameFinished{}).ProtoReflect().Descriptor().FullName())
	gameKey          = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	changedNamespace = uuid.MustParse("015bd27a-27d4-4003-bbbf-a36fb7ba687b")
)

func ValidGame(game string) bool { return gameKey.MatchString(game) }

type Player struct {
	User   uuid.UUID
	Rating glicko.Rating
	Games  int
	Wins   int
	Losses int
	Draws  int
}

func Newcomer(user uuid.UUID) Player { return Player{User: user, Rating: glicko.Newcomer} }

type Game struct {
	EventID    uuid.UUID
	Session    uuid.UUID
	Game       string
	Players    [2]uuid.UUID
	Winner     int
	FinishedAt time.Time
}

func Decode(kind string, value []byte) (Game, error) {
	if kind != "" && kind != finishedType {
		return Game{}, fmt.Errorf("%w: %s", ErrForeign, kind)
	}
	event := &eventsv1.GameFinished{}
	if err := proto.Unmarshal(value, event); err != nil {
		return Game{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return Parse(event)
}

func Parse(event *eventsv1.GameFinished) (Game, error) {
	eventID, err := uuid.Parse(event.GetEventId())
	if err != nil {
		return Game{}, fmt.Errorf("%w: event id %q", ErrInvalid, event.GetEventId())
	}
	session, err := uuid.Parse(event.GetSessionId())
	if err != nil {
		return Game{}, fmt.Errorf("%w: session %q", ErrInvalid, event.GetSessionId())
	}
	if !ValidGame(event.GetGame()) {
		return Game{}, fmt.Errorf("%w: game %q", ErrInvalid, event.GetGame())
	}
	if len(event.GetPlayers()) != 2 {
		return Game{}, fmt.Errorf("%w: %d players", ErrInvalid, len(event.GetPlayers()))
	}
	game := Game{EventID: eventID, Session: session, Game: event.GetGame(), Winner: -1}
	for seat, raw := range event.GetPlayers() {
		if game.Players[seat], err = uuid.Parse(raw); err != nil {
			return Game{}, fmt.Errorf("%w: player %q", ErrInvalid, raw)
		}
	}
	if game.Players[0] == game.Players[1] {
		return Game{}, fmt.Errorf("%w: a player cannot face themselves", ErrInvalid)
	}
	switch event.GetOutcome() {
	case eventsv1.GameFinished_OUTCOME_WIN:
		if event.GetWinnerSeat() != 0 && event.GetWinnerSeat() != 1 {
			return Game{}, fmt.Errorf("%w: winner seat %d", ErrInvalid, event.GetWinnerSeat())
		}
		game.Winner = int(event.GetWinnerSeat())
	case eventsv1.GameFinished_OUTCOME_DRAW:
	default:
		return Game{}, fmt.Errorf("%w: outcome %v", ErrInvalid, event.GetOutcome())
	}
	if !event.GetFinishedAt().IsValid() {
		return Game{}, fmt.Errorf("%w: finished_at is missing", ErrInvalid)
	}
	game.FinishedAt = event.GetFinishedAt().AsTime().UTC()
	if !event.GetRated() {
		return game, ErrUnrated
	}
	return game, nil
}

func (g Game) score() glicko.Score {
	switch g.Winner {
	case 0:
		return glicko.Win
	case 1:
		return glicko.Loss
	default:
		return glicko.Draw
	}
}

func Settle(system glicko.System, game Game, players [2]Player) [2]Player {
	after := players
	after[0].Rating, after[1].Rating = system.Game(players[0].Rating, players[1].Rating, game.score())
	for seat := range after {
		after[seat].Games++
		switch {
		case game.Winner == seat:
			after[seat].Wins++
		case game.Winner < 0:
			after[seat].Draws++
		default:
			after[seat].Losses++
		}
	}
	return after
}

type Change struct {
	EventID uuid.UUID
	Session uuid.UUID
	Game    string
	At      time.Time
	Before  Player
	After   Player
}

func Changes(game Game, before, after [2]Player) []Change {
	out := make([]Change, len(before))
	for seat := range before {
		out[seat] = Change{EventID: game.EventID, Session: game.Session, Game: game.Game, At: game.FinishedAt, Before: before[seat], After: after[seat]}
	}
	return out
}

func Rounded(rating float64) int32 { return int32(math.Round(rating)) }

func ChangedEventID(source uuid.UUID) uuid.UUID { return uuid.NewSHA1(changedNamespace, source[:]) }

func Announcement(game Game, before, after [2]Player) outbox.Event {
	id := ChangedEventID(game.EventID)
	payload := &eventsv1.RatingChanged{
		EventId: id.String(), SourceEventId: game.EventID.String(), SessionId: game.Session.String(), Game: game.Game,
		ChangedAt: timestamppb.New(game.FinishedAt),
	}
	for seat := range before {
		next := after[seat]
		payload.Changes = append(payload.Changes, &eventsv1.RatingChange{
			UserId: next.User.String(), Before: Rounded(before[seat].Rating.Rating), After: Rounded(next.Rating.Rating),
			Deviation: next.Rating.Deviation, Volatility: next.Rating.Volatility,
			Games: int32(next.Games), Wins: int32(next.Wins), Losses: int32(next.Losses), Draws: int32(next.Draws),
		})
	}
	return outbox.Event{ID: id, Topic: ChangedTopic, AggregateID: game.Game, Payload: payload}
}

type Key struct {
	Game string
	User uuid.UUID
}

type Book struct {
	system   glicko.System
	Players  map[Key]Player
	Events   map[uuid.UUID]uuid.UUID
	Changes  []Change
	sessions map[uuid.UUID]bool
}

func NewBook(system glicko.System) *Book {
	return &Book{system: system, Players: map[Key]Player{}, Events: map[uuid.UUID]uuid.UUID{}, sessions: map[uuid.UUID]bool{}}
}

func (b *Book) player(game string, user uuid.UUID) Player {
	if player, ok := b.Players[Key{Game: game, User: user}]; ok {
		return player
	}
	return Newcomer(user)
}

func (b *Book) Apply(game Game) bool {
	if _, seen := b.Events[game.EventID]; seen {
		return false
	}
	b.Events[game.EventID] = game.Session
	if b.sessions[game.Session] {
		return false
	}
	b.sessions[game.Session] = true
	before := [2]Player{b.player(game.Game, game.Players[0]), b.player(game.Game, game.Players[1])}
	after := Settle(b.system, game, before)
	for _, player := range after {
		b.Players[Key{Game: game.Game, User: player.User}] = player
	}
	b.Changes = append(b.Changes, Changes(game, before, after)...)
	return true
}

func (b *Book) Games() []string {
	seen := map[string]bool{}
	var games []string
	for key := range b.Players {
		if !seen[key.Game] {
			seen[key.Game] = true
			games = append(games, key.Game)
		}
	}
	return games
}

func (b *Book) Standings(game string) []Player {
	var out []Player
	for key, player := range b.Players {
		if key.Game == game {
			out = append(out, player)
		}
	}
	return out
}
