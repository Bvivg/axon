package store_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
)

func TestOutboxMigrationMatchesTheSharedSchema(t *testing.T) {
	raw, err := os.ReadFile("../../migrations/000002_outbox.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != strings.TrimSpace(outbox.Migration("rating")) {
		t.Fatalf("000002_outbox.up.sql drifted from outbox.Migration(\"rating\"):\n%s", raw)
	}
}

func setup(t *testing.T) (context.Context, *pgxpool.Pool, *store.Postgres) {
	t.Helper()
	dsn := os.Getenv("RATING_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("RATING_TEST_DATABASE_URL is unset")
	}
	if !strings.Contains(dsn, "/axon_rating_test") {
		t.Fatal("requires the isolated axon_rating_test database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	var applied bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('rating.outbox') IS NOT NULL`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if !applied {
		if _, err := pool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS rating`); err != nil {
			t.Fatal(err)
		}
		for _, file := range []string{"000001_ratings", "000002_outbox"} {
			raw, err := os.ReadFile("../../migrations/" + file + ".up.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, string(raw)); err != nil {
				t.Fatal(file, err)
			}
		}
	}
	if _, err := pool.Exec(ctx, `TRUNCATE rating.changes, rating.ratings, rating.processed_events, rating.outbox`); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, store.New(pool, glicko.Default)
}

var users = []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

func played(key string, a, b, winner int) ledger.Game {
	return ledger.Game{EventID: uuid.New(), Session: uuid.New(), Game: key, Players: [2]uuid.UUID{users[a], users[b]}, Winner: winner,
		FinishedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func count(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestApplySettlesOnceAndAnnouncesThroughTheOutbox(t *testing.T) {
	ctx, pool, ratings := setup(t)
	game := played("chess", 0, 1, 1)
	applied, ok, err := ratings.Apply(ctx, game)
	if err != nil || !ok {
		t.Fatal("apply", ok, err)
	}
	want := ledger.Settle(glicko.Default, game, [2]ledger.Player{ledger.Newcomer(users[0]), ledger.Newcomer(users[1])})
	if applied.After != want || applied.Before[0] != ledger.Newcomer(users[0]) {
		t.Fatalf("applied = %+v, want %+v", applied.After, want)
	}
	var id uuid.UUID
	var kind, key string
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT id, aggregatetype, aggregateid, payload FROM rating.outbox`).Scan(&id, &kind, &key, &payload); err != nil {
		t.Fatal(err)
	}
	announced := &eventsv1.RatingChanged{}
	if err := proto.Unmarshal(payload, announced); err != nil {
		t.Fatal(err)
	}
	if id != ledger.ChangedEventID(game.EventID) || kind != "rating.changed.v1" || key != "chess" || announced.SessionId != game.Session.String() ||
		len(announced.Changes) != 2 || announced.Changes[1].UserId != users[1].String() || announced.Changes[1].Wins != 1 {
		t.Fatalf("outbox row %s %s %s %v", id, kind, key, announced)
	}
	if _, ok, err := ratings.Apply(ctx, game); err != nil || ok {
		t.Fatal("a redelivered event was applied", ok, err)
	}
	again := game
	again.EventID = uuid.New()
	if _, ok, err := ratings.Apply(ctx, again); err != nil || ok {
		t.Fatal("a second event for a settled session was applied", ok, err)
	}
	if count(t, ctx, pool, "rating.changes") != 2 || count(t, ctx, pool, "rating.outbox") != 1 || count(t, ctx, pool, "rating.processed_events") != 2 {
		t.Fatal("duplicates left rows behind")
	}
}

func TestLiveRatingsEqualAnInMemoryReplay(t *testing.T) {
	ctx, pool, ratings := setup(t)
	history := []ledger.Game{played("chess", 0, 1, 0), played("chess", 1, 2, -1), played("chess", 2, 0, 1), played("tictactoe", 0, 2, 0)}
	history = append(history, history[1])
	book := ledger.NewBook(glicko.Default)
	for _, game := range history {
		book.Apply(game)
		if _, _, err := ratings.Apply(ctx, game); err != nil {
			t.Fatal(err)
		}
	}
	standings, err := ratings.Standings(ctx)
	if err != nil || len(standings) != len(book.Players) {
		t.Fatal("standings", len(standings), err)
	}
	for _, standing := range standings {
		if rebuilt := book.Players[ledger.Key{Game: standing.Game, User: standing.Player.User}]; rebuilt != standing.Player {
			t.Fatalf("live %+v, rebuilt %+v", standing.Player, rebuilt)
		}
	}
	top, total, err := ratings.Top(ctx, "chess", 0, 2)
	if err != nil || total != 3 || len(top) != 2 || top[0].Rating.Rating < top[1].Rating.Rating {
		t.Fatal("top", top, total, err)
	}
	details, err := ratings.Players(ctx, "chess", []uuid.UUID{users[0], uuid.New()})
	if err != nil || len(details) != 1 || details[users[0]].Games != 2 {
		t.Fatal("players", details, err)
	}

	replacement := ledger.NewBook(glicko.Default)
	for _, game := range history {
		replacement.Apply(game)
	}
	if err := ratings.Replace(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if count(t, ctx, pool, "rating.changes") != len(replacement.Changes) || count(t, ctx, pool, "rating.processed_events") != len(replacement.Events) {
		t.Fatal("replace did not rewrite the history")
	}
	if _, _, err := ratings.Apply(ctx, played("chess", 0, 1, 0)); err != nil {
		t.Fatal(err)
	}
	if err := ratings.Replace(ctx, replacement); !errors.Is(err, store.ErrAhead) {
		t.Fatal("replace dropped an event the rebuild never saw", err)
	}
}
