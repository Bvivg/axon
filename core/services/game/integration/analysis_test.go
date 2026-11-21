//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
	"github.com/bvivg/axon/core/services/game/internal/journal"
	"github.com/bvivg/axon/core/services/game/internal/owner"
	"github.com/bvivg/axon/core/services/game/internal/projection"
	"github.com/bvivg/axon/core/services/game/internal/session"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

func TestOnlyRatedChessGamesRequestAnAnalysis(t *testing.T) {
	ctx := context.Background()
	games := engine.NewRegistry()
	if err := games.Register(chess.Definition()); err != nil {
		t.Fatal(err)
	}
	rules := session.NewRules(games)
	store := session.NewStore(pool, games)
	model := projection.New(store, rules)
	log := journal.NewMemory(partitions)
	node := owner.New(owner.Config{ID: "analysis", Address: "analysis:8083", Log: log, Model: model, Rules: rules, Now: time.Now})
	for p := range int32(partitions) {
		if err := node.Assign(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(node.RevokeAll)
	mate := func(rated bool) session.Session {
		x, o := uuid.New(), uuid.New()
		game, err := node.Start(ctx, session.SessionID(x, uuid.New()), chess.Key, x, o, rated)
		if err != nil {
			t.Fatal(err)
		}
		players := []uuid.UUID{x, o}
		for ply, uci := range []string{"f2f3", "e7e5", "g2g4", "d8h4"} {
			data, _ := json.Marshal(chess.MoveData{UCI: uci})
			if game, err = node.Move(ctx, game.ID, players[ply%2], ply, chess.Key, data, &game.Revision); err != nil {
				t.Fatal(uci, err)
			}
		}
		return game
	}
	rated, casual := mate(true), mate(false)
	batches := map[int32][]journal.Entry{}
	for p := range int32(partitions) {
		end, _ := log.End(ctx, p)
		if err := log.Read(ctx, p, 0, end, func(entry journal.Entry) error {
			batches[p] = append(batches[p], entry)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := model.Apply(ctx, batches); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT aggregatetype, aggregateid, payload FROM game.outbox WHERE aggregatetype = ANY($1)`,
		[]string{projection.FinishedTopic, projection.AnalysisTopic})
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := map[string][]string{}
	for rows.Next() {
		var topic, key string
		var payload []byte
		if err := rows.Scan(&topic, &key, &payload); err != nil {
			t.Fatal(err)
		}
		event := &eventsv1.GameFinished{}
		if err := proto.Unmarshal(payload, event); err != nil {
			t.Fatal(err)
		}
		if event.SessionId != rated.ID.String() && event.SessionId != casual.ID.String() {
			continue
		}
		seen[topic] = append(seen[topic], key)
		if topic == projection.AnalysisTopic && (len(event.Moves) != 4 || !event.Rated) {
			t.Fatalf("analysis request = %v", event)
		}
		if topic == projection.FinishedTopic && len(event.Moves) != 0 {
			t.Fatalf("game.finished carries moves: %v", event.Moves)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(seen[projection.FinishedTopic]) != 2 || len(seen[projection.AnalysisTopic]) != 1 || seen[projection.AnalysisTopic][0] != rated.ID.String() {
		t.Fatalf("outbox rows = %v, want two finished games and one analysis request keyed by the rated session", seen)
	}
}
