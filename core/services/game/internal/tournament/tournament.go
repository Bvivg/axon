package tournament

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/tournamentid"
)

const (
	RoundStartedTopic = "tournament.round_started.v1"
	ForfeitTopic      = "tournament.forfeit.v1"
	CancelledTopic    = "tournament.cancelled.v1"
	Group             = "game-tournaments"
)

var ErrMalformed = errors.New("tournament: malformed tournament command")

var (
	roundStartedType = string((&eventsv1.TournamentRoundStarted{}).ProtoReflect().Descriptor().FullName())
	forfeitType      = string((&eventsv1.TournamentForfeit{}).ProtoReflect().Descriptor().FullName())
	cancelledType    = string((&eventsv1.TournamentCancelled{}).ProtoReflect().Descriptor().FullName())
)

type Game struct {
	Tournament uuid.UUID
	Round      int
	Board      int
	Session    uuid.UUID
	Game       engine.Key
	Players    [2]uuid.UUID
}

type Games interface {
	StartTournamentGame(ctx context.Context, game Game) error
	ForfeitTournamentGame(ctx context.Context, game Game) error
	AbortTournamentGame(ctx context.Context, session uuid.UUID) error
}

type Cancellations interface {
	Cancel(ctx context.Context, tournament uuid.UUID) error
	Cancelled(ctx context.Context, tournament uuid.UUID) (bool, error)
}

type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) Cancel(ctx context.Context, tournament uuid.UUID) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO game.cancelled_tournaments (tournament_id) VALUES ($1) ON CONFLICT DO NOTHING`, tournament)
	return err
}

func (p *Postgres) Cancelled(ctx context.Context, tournament uuid.UUID) (bool, error) {
	var cancelled bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM game.cancelled_tournaments WHERE tournament_id = $1)`, tournament).Scan(&cancelled)
	return cancelled, err
}

func parseID(raw, what string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("%w: %s %q", ErrMalformed, what, raw)
	}
	return id, nil
}

func pairing(tournament uuid.UUID, key engine.Key, round uint32, in *eventsv1.TournamentPairing) (Game, error) {
	if in == nil || round == 0 || in.GetBoard() == 0 {
		return Game{}, fmt.Errorf("%w: pairing without a round or board", ErrMalformed)
	}
	if len(in.GetPlayers()) != 2 {
		return Game{}, fmt.Errorf("%w: board %d has %d players", ErrMalformed, in.GetBoard(), len(in.GetPlayers()))
	}
	game := Game{Tournament: tournament, Round: int(round), Board: int(in.GetBoard()), Game: key}
	for seat, raw := range in.GetPlayers() {
		player, err := parseID(raw, "player")
		if err != nil {
			return Game{}, err
		}
		game.Players[seat] = player
	}
	if game.Players[0] == game.Players[1] {
		return Game{}, fmt.Errorf("%w: board %d pairs a player with themselves", ErrMalformed, in.GetBoard())
	}
	game.Session = tournamentid.Session(tournament, game.Round, game.Board)
	if in.GetSessionId() != game.Session.String() {
		return Game{}, fmt.Errorf("%w: board %d session %q is not %s", ErrMalformed, in.GetBoard(), in.GetSessionId(), game.Session)
	}
	return game, nil
}

type Command struct {
	Tournament uuid.UUID
	Games      []Game
	Forfeit    bool
	Cancelled  bool
	Aborted    []uuid.UUID
}

func Decode(topic, kind string, value []byte) (Command, error) {
	switch {
	case kind == roundStartedType, kind == "" && topic == RoundStartedTopic:
		return decodeRound(value)
	case kind == forfeitType, kind == "" && topic == ForfeitTopic:
		return decodeForfeit(value)
	case kind == cancelledType, kind == "" && topic == CancelledTopic:
		return decodeCancelled(value)
	default:
		return Command{}, fmt.Errorf("%w: event type %q on %s", ErrMalformed, kind, topic)
	}
}

func decodeRound(value []byte) (Command, error) {
	event := &eventsv1.TournamentRoundStarted{}
	if err := proto.Unmarshal(value, event); err != nil {
		return Command{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	tournament, err := parseID(event.GetTournamentId(), "tournament")
	if err != nil {
		return Command{}, err
	}
	if event.GetGame() == "" {
		return Command{}, fmt.Errorf("%w: no game", ErrMalformed)
	}
	command := Command{Tournament: tournament}
	for _, in := range event.GetPairings() {
		game, err := pairing(tournament, engine.Key(event.GetGame()), event.GetRound(), in)
		if err != nil {
			return Command{}, err
		}
		command.Games = append(command.Games, game)
	}
	return command, nil
}

func decodeForfeit(value []byte) (Command, error) {
	event := &eventsv1.TournamentForfeit{}
	if err := proto.Unmarshal(value, event); err != nil {
		return Command{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	tournament, err := parseID(event.GetTournamentId(), "tournament")
	if err != nil {
		return Command{}, err
	}
	if event.GetGame() == "" {
		return Command{}, fmt.Errorf("%w: no game", ErrMalformed)
	}
	game, err := pairing(tournament, engine.Key(event.GetGame()), event.GetRound(), event.GetPairing())
	if err != nil {
		return Command{}, err
	}
	return Command{Tournament: tournament, Games: []Game{game}, Forfeit: true}, nil
}

func decodeCancelled(value []byte) (Command, error) {
	event := &eventsv1.TournamentCancelled{}
	if err := proto.Unmarshal(value, event); err != nil {
		return Command{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	tournament, err := parseID(event.GetTournamentId(), "tournament")
	if err != nil {
		return Command{}, err
	}
	command := Command{Tournament: tournament, Cancelled: true}
	for _, raw := range event.GetSessionIds() {
		id, err := parseID(raw, "session")
		if err != nil {
			return Command{}, err
		}
		command.Aborted = append(command.Aborted, id)
	}
	return command, nil
}
