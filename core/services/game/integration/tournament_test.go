//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/cluster"
	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/tictactoe"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/route"
	"github.com/bvivg/axon/core/services/game/internal/server"
	"github.com/bvivg/axon/core/services/game/internal/session"
	"github.com/bvivg/axon/core/services/game/internal/tournament"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
	"github.com/bvivg/axon/core/shared/pkg/outbox"
	"github.com/bvivg/axon/core/shared/pkg/tournamentid"
)

func produce(t *testing.T, client *kgo.Client, topic string, key uuid.UUID, message proto.Message) {
	t.Helper()
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	record := &kgo.Record{Topic: topic, Key: []byte(key.String()), Value: raw, Headers: []kgo.RecordHeader{
		{Key: outbox.TypeHeader, Value: []byte(message.ProtoReflect().Descriptor().FullName())},
		{Key: outbox.IDHeader, Value: []byte(uuid.NewString())},
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.ProduceSync(ctx, record).FirstErr(); err != nil {
		t.Fatal(err)
	}
}

func TestTournamentCommandsCreateForfeitAndAbortSessionsOnce(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"outbox", "match_queue", "rating_changes", "moves", "sessions", "ratings", "owners", "projector_offsets", "cancelled_tournaments"} {
		if _, err := pool.Exec(ctx, `DELETE FROM game.`+table); err != nil {
			t.Fatal(err)
		}
	}
	games := engine.NewRegistry()
	if err := games.Register(tictactoe.Definition()); err != nil {
		t.Fatal(err)
	}
	store := session.NewStore(pool, games)
	rules := session.NewRules(games)
	model := projection.New(store, rules)
	suffix := uuid.NewString()[:8]
	cfg := journal.KafkaConfig{Brokers: brokers, Topic: "game.session-events.t-" + suffix, LeaseTopic: "game.partition-leases.t-" + suffix, Partitions: partitions, ReplicationFactor: 1}
	log, err := journal.NewKafka(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	own := owner.New(owner.Config{ID: "solo", Address: "solo:8083", Log: log, Model: model, Rules: rules, Now: time.Now})
	handler := server.New(server.Config{Store: store, Owner: own, Routes: route.New(model.Owners), Self: "solo:8083"})
	own.Listen(handler)
	ownership := spawn(func(ctx context.Context) {
		defer log.Close()
		_ = cluster.NewOwnership(cfg, own, quiet).Run(ctx)
	})
	t.Cleanup(ownership.stop)
	projector := spawn(func(ctx context.Context) { _ = cluster.NewProjector(cfg, model, quiet, nil).Run(ctx) })
	t.Cleanup(projector.stop)
	eventually(t, "the node to own every partition", func() bool { return len(own.Owned()) == partitions })

	topics := []string{"tournament.round_started.t-" + suffix, "tournament.forfeit.t-" + suffix, "tournament.cancelled.t-" + suffix}
	producer, err := kgo.NewClient(kgo.SeedBrokers(brokers...), kgo.AllowAutoTopicCreation())
	if err != nil {
		t.Fatal(err)
	}
	defer producer.Close()
	commands := tournament.NewConsumer(tournament.Config{
		Brokers: brokers, Topics: topics, Group: "game-tournaments-" + suffix, Games: handler, Cancellations: tournament.NewPostgres(pool), Log: quiet,
	})
	consumer := spawn(func(ctx context.Context) { _ = commands.Run(ctx) })
	t.Cleanup(consumer.stop)

	id := uuid.New()
	players := make([]uuid.UUID, 6)
	for i := range players {
		players[i] = uuid.New()
	}
	pairing := func(round, board int, a, b uuid.UUID) *eventsv1.TournamentPairing {
		return &eventsv1.TournamentPairing{Board: uint32(board), SessionId: tournamentid.Session(id, round, board).String(), Players: []string{a.String(), b.String()}}
	}
	round := &eventsv1.TournamentRoundStarted{EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 1,
		Pairings: []*eventsv1.TournamentPairing{pairing(1, 1, players[0], players[1]), pairing(1, 2, players[2], players[3]), pairing(1, 3, players[4], players[5])}}
	forfeit := &eventsv1.TournamentForfeit{EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 1, Pairing: pairing(1, 3, players[4], players[5])}
	produce(t, producer, topics[1], id, forfeit)
	for range 3 {
		produce(t, producer, topics[0], id, round)
	}

	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	eventually(t, "three tagged sessions", func() bool {
		return count(`SELECT count(*) FROM game.sessions WHERE tournament_id = $1`, id) == 3
	})
	board1, board3 := tournamentid.Session(id, 1, 1), tournamentid.Session(id, 1, 3)
	eventually(t, "the forfeit of board 3 to reach the outbox", func() bool {
		return count(`SELECT count(*) FROM game.outbox WHERE aggregatetype = $1`, projection.FinishedTopic) == 1
	})
	forfeited, err := model.Load(ctx, board3)
	if err != nil {
		t.Fatal(err)
	}
	if !forfeited.Resigned || forfeited.State.Winner != 1 || forfeited.State.Ply != 0 || !forfeited.Rated {
		t.Fatalf("board 3 = %+v", forfeited)
	}
	var payload []byte
	if err := pool.QueryRow(ctx, `SELECT payload FROM game.outbox WHERE aggregatetype = $1`, projection.FinishedTopic).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	finished := &eventsv1.GameFinished{}
	if err := proto.Unmarshal(payload, finished); err != nil {
		t.Fatal(err)
	}
	if finished.GetTournamentId() != id.String() || finished.GetSessionId() != board3.String() || finished.GetPlies() != 0 || finished.GetWinnerSeat() != 1 {
		t.Fatalf("game.finished = %v", finished)
	}

	if _, err := own.Move(ctx, board1, players[0], 0, tictactoe.Key, cell(4), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := own.Move(ctx, board1, players[1], 1, tictactoe.Key, cell(0), nil); err != nil {
		t.Fatal(err)
	}
	produce(t, producer, topics[1], id, &eventsv1.TournamentForfeit{EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 1, Pairing: pairing(1, 1, players[0], players[1])})
	eventually(t, "the forfeit of a board where both players moved to be ignored", func() bool {
		game, err := own.Get(ctx, board1, players[0])
		return err == nil && game.State.Ply == 2 && !game.State.Over() &&
			count(`SELECT count(*) FROM game.outbox WHERE aggregatetype = $1`, projection.FinishedTopic) == 1
	})
	produce(t, producer, topics[0], id, &eventsv1.TournamentRoundStarted{EventId: uuid.NewString(), TournamentId: id.String(), Game: "tictactoe", Round: 2,
		Pairings: []*eventsv1.TournamentPairing{pairing(2, 1, players[0], players[2])}})
	cancelled := &eventsv1.TournamentCancelled{EventId: uuid.NewString(), TournamentId: id.String(), Started: true,
		SessionIds: []string{board1.String(), tournamentid.Session(id, 1, 2).String(), tournamentid.Session(id, 2, 1).String()}}
	produce(t, producer, topics[2], id, cancelled)
	produce(t, producer, topics[2], id, cancelled)
	produce(t, producer, topics[0], id, round)
	eventually(t, "the live boards to be aborted", func() bool {
		return count(`SELECT count(*) FROM game.sessions WHERE tournament_id = $1`, id) == 1 &&
			count(`SELECT count(*) FROM game.cancelled_tournaments WHERE tournament_id = $1`, id) == 1
	})
	time.Sleep(3 * time.Second)
	if n := count(`SELECT count(*) FROM game.sessions WHERE tournament_id = $1`, id); n != 1 {
		t.Fatalf("after the cancellation %d sessions remain, want only the forfeited board", n)
	}
	if n := count(`SELECT count(*) FROM game.outbox WHERE aggregatetype = $1`, projection.FinishedTopic); n != 1 {
		t.Fatalf("aborting published %d game.finished events, want the forfeit only", n)
	}
	if _, err := own.Get(ctx, board1, players[0]); err == nil {
		t.Fatal("an aborted board is still playable")
	}
	t.Logf("round_started of round 1 delivered 4 times and the forfeit before it: %d tagged sessions; after a doubled cancellation %d remains, %d game.finished",
		3, count(`SELECT count(*) FROM game.sessions WHERE tournament_id = $1`, id), count(`SELECT count(*) FROM game.outbox WHERE aggregatetype = $1`, projection.FinishedTopic))
}
