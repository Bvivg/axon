package ledger

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
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

const (
	MinPlayers = 2
	MaxPlayers = 6
)

type Game struct {
	EventID    uuid.UUID
	Session    uuid.UUID
	Game       string
	Players    []uuid.UUID
	Places     []int
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
	n := len(event.GetPlayers())
	if n < MinPlayers || n > MaxPlayers {
		return Game{}, fmt.Errorf("%w: %d players", ErrInvalid, n)
	}
	game := Game{EventID: eventID, Session: session, Game: event.GetGame(), Winner: -1, Players: make([]uuid.UUID, n)}
	for seat, raw := range event.GetPlayers() {
		if game.Players[seat], err = uuid.Parse(raw); err != nil {
			return Game{}, fmt.Errorf("%w: player %q", ErrInvalid, raw)
		}
		if slices.Contains(game.Players[:seat], game.Players[seat]) {
			return Game{}, fmt.Errorf("%w: a player cannot face themselves", ErrInvalid)
		}
	}
	switch event.GetOutcome() {
	case eventsv1.GameFinished_OUTCOME_WIN:
		if event.GetWinnerSeat() < 0 || int(event.GetWinnerSeat()) >= n {
			return Game{}, fmt.Errorf("%w: winner seat %d", ErrInvalid, event.GetWinnerSeat())
		}
		game.Winner = int(event.GetWinnerSeat())
	case eventsv1.GameFinished_OUTCOME_DRAW:
	default:
		return Game{}, fmt.Errorf("%w: outcome %v", ErrInvalid, event.GetOutcome())
	}
	if game.Places, err = places(event.GetPlaces(), n, game.Winner); err != nil {
		return Game{}, err
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

func places(raw []uint32, n, winner int) ([]int, error) {
	out := make([]int, n)
	if len(raw) == 0 {
		if n != MinPlayers {
			return nil, fmt.Errorf("%w: %d players without places", ErrInvalid, n)
		}
		for seat := range out {
			out[seat] = 1
			if winner >= 0 && seat != winner {
				out[seat] = 2
			}
		}
		return out, nil
	}
	if len(raw) != n {
		return nil, fmt.Errorf("%w: %d places for %d players", ErrInvalid, len(raw), n)
	}
	for seat, place := range raw {
		if place < 1 || int(place) > n {
			return nil, fmt.Errorf("%w: place %d", ErrInvalid, place)
		}
		out[seat] = int(place)
	}
	return out, nil
}

func score(mine, theirs int) glicko.Score {
	switch {
	case mine < theirs:
		return glicko.Win
	case mine > theirs:
		return glicko.Loss
	default:
		return glicko.Draw
	}
}

func Settle(system glicko.System, game Game, players []Player) []Player {
	after := slices.Clone(players)
	best, worst := slices.Min(game.Places), slices.Max(game.Places)
	for seat := range after {
		results := make([]glicko.Result, 0, len(players)-1)
		for other := range players {
			if other != seat {
				results = append(results, glicko.Result{Opponent: players[other].Rating, Score: score(game.Places[seat], game.Places[other])})
			}
		}
		after[seat].Rating = system.Rate(players[seat].Rating, results)
		after[seat].Games++
		switch place := game.Places[seat]; {
		case best == worst:
			after[seat].Draws++
		case place == best:
			after[seat].Wins++
		case place == worst:
			after[seat].Losses++
		default:
			after[seat].Draws++
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

func Changes(game Game, before, after []Player) []Change {
	out := make([]Change, len(before))
	for seat := range before {
		out[seat] = Change{EventID: game.EventID, Session: game.Session, Game: game.Game, At: game.FinishedAt, Before: before[seat], After: after[seat]}
	}
	return out
}

func Rounded(rating float64) int32 { return int32(math.Round(rating)) }

func ChangedEventID(source uuid.UUID) uuid.UUID { return uuid.NewSHA1(changedNamespace, source[:]) }

func Announcement(game Game, before, after []Player) outbox.Event {
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
	before := make([]Player, len(game.Players))
	for seat, user := range game.Players {
		before[seat] = b.player(game.Game, user)
	}
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
