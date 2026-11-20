//go:build e2e

package e2e

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/leaderboard"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/rebuild"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	ratingv1 "github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1"
	"github.com/bvivg/axon/core/shared/gen/go/axon/rating/v1/ratingv1connect"
)

func env(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Skipf("%s is unset", key)
	}
	return value
}

func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for", what)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func finished(key string, players [2]uuid.UUID, winner int32, rated bool) *eventsv1.GameFinished {
	outcome := eventsv1.GameFinished_OUTCOME_WIN
	if winner < 0 {
		outcome = eventsv1.GameFinished_OUTCOME_DRAW
	}
	return &eventsv1.GameFinished{
		EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: key, Players: []string{players[0].String(), players[1].String()},
		Outcome: outcome, WinnerSeat: winner, FinishedAt: timestamppb.Now(), Rated: rated,
	}
}

func record(t *testing.T, topic string, event *eventsv1.GameFinished) *kgo.Record {
	t.Helper()
	raw, err := proto.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return &kgo.Record{Topic: topic, Key: []byte(event.Game), Value: raw, Headers: []kgo.RecordHeader{
		{Key: "event_id", Value: []byte(event.EventId)},
		{Key: "event_type", Value: []byte("axon.events.v1.GameFinished")},
	}}
}

func TestFinishedGamesBecomeRatingsLeaderboardsAndAnnouncements(t *testing.T) {
	brokers := strings.Split(env(t, "RATING_E2E_KAFKA_BROKERS"), ",")
	dsn := env(t, "RATING_E2E_POSTGRES_DSN")
	redisAddr := env(t, "RATING_E2E_REDIS_ADDR")
	api := env(t, "RATING_E2E_API_URL")
	topic := ledger.FinishedTopic
	ctx := context.Background()

	client, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.RequiredAcks(kgo.AllISRAcks()))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := kadm.NewClient(client).CreateTopic(ctx, 3, 1, nil, topic); err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	cache := goredis.NewClient(&goredis.Options{Addr: redisAddr, Password: os.Getenv("RATING_E2E_REDIS_PASSWORD")})
	defer func() { _ = cache.Close() }()
	var before int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM rating.processed_events`).Scan(&before); err != nil {
		t.Fatal(err)
	}

	people := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	games := []*eventsv1.GameFinished{
		finished("chess", [2]uuid.UUID{people[0], people[1]}, 0, true),
		finished("chess", [2]uuid.UUID{people[1], people[2]}, -1, true),
		finished("chess", [2]uuid.UUID{people[2], people[0]}, 1, true),
		finished("quoridor", [2]uuid.UUID{people[3], people[0]}, 0, true),
		finished("tictactoe", [2]uuid.UUID{people[1], people[3]}, 1, true),
	}
	unrated := finished("chess", [2]uuid.UUID{people[0], people[3]}, 0, false)
	var records []*kgo.Record
	for _, game := range games {
		records = append(records, record(t, topic, game))
	}
	records = append(records, record(t, topic, games[0]), record(t, topic, unrated))
	if err := client.ProduceSync(ctx, records...).FirstErr(); err != nil {
		t.Fatal(err)
	}

	eventually(t, "every rated event to be processed", func() bool {
		var processed int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM rating.processed_events`).Scan(&processed); err != nil {
			t.Fatal(err)
		}
		return processed == before+len(games)
	})

	book := ledger.NewBook(glicko.Default)
	for _, game := range games {
		parsed, err := ledger.Parse(game)
		if err != nil {
			t.Fatal(err)
		}
		book.Apply(parsed)
	}
	live := store.New(pool, glicko.Default)
	details, err := live.Players(ctx, "chess", people)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range book.Players {
		if key.Game != "chess" {
			continue
		}
		if got := details[key.User]; got != want {
			t.Fatalf("chess %s: live %+v, want %+v", key.User, got, want)
		}
	}

	for _, game := range games {
		var announced []byte
		id := ledger.ChangedEventID(uuid.MustParse(game.EventId))
		if err := pool.QueryRow(ctx, `SELECT payload FROM rating.outbox WHERE id = $1 AND aggregatetype = 'rating.changed.v1' AND aggregateid = $2`, id, game.Game).Scan(&announced); err != nil {
			t.Fatal("rating.changed for", game.SessionId, err)
		}
		changed := &eventsv1.RatingChanged{}
		if err := proto.Unmarshal(announced, changed); err != nil || changed.SessionId != game.SessionId || len(changed.Changes) != 2 {
			t.Fatal("announcement", changed, err)
		}
	}

	board := leaderboard.New(cache)
	eventually(t, "the chess leaderboard to hold the new players", func() bool {
		scores, err := board.Scores(ctx, "chess")
		if err != nil {
			t.Fatal(err)
		}
		for _, user := range people[:3] {
			if scores[user] != book.Players[ledger.Key{Game: "chess", User: user}].Rating.Rating {
				return false
			}
		}
		return true
	})

	rpc := ratingv1connect.NewRatingServiceClient(http.DefaultClient, api)
	response, err := rpc.GetLeaderboard(ctx, connect.NewRequest(&ratingv1.GetLeaderboardRequest{Game: "quoridor", Limit: 100}))
	if err != nil {
		t.Fatal(err)
	}
	ranks := map[string]*ratingv1.LeaderboardEntry{}
	for _, entry := range response.Msg.Entries {
		ranks[entry.UserId] = entry
	}
	winner, loser := ranks[people[3].String()], ranks[people[0].String()]
	if winner == nil || loser == nil || winner.Rank >= loser.Rank || winner.Wins != 1 || loser.Losses != 1 || winner.Rating <= loser.Rating {
		t.Fatalf("quoridor leaderboard = %v", response.Msg.Entries)
	}
	if _, err := rpc.GetLeaderboard(ctx, connect.NewRequest(&ratingv1.GetLeaderboardRequest{Game: "no such game!"})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("an invalid game key was accepted", err)
	}

	recomputed, stats, err := rebuild.Recompute(ctx, rebuild.Kafka{Brokers: brokers, Topic: topic}, glicko.Default)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Duplicates < 1 || stats.Skipped < 1 {
		t.Fatalf("rebuild stats = %+v", stats)
	}
	differences, err := rebuild.Compare(ctx, recomputed, live, board)
	if err != nil || len(differences) != 0 {
		t.Fatalf("a rebuild from offset zero differs from the live ratings: %v %v", differences, err)
	}
}
