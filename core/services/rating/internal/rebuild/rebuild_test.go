package rebuild_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/bvivg/axon/core/services/rating/internal/glicko"
	"github.com/bvivg/axon/core/services/rating/internal/leaderboard"
	"github.com/bvivg/axon/core/services/rating/internal/ledger"
	"github.com/bvivg/axon/core/services/rating/internal/rebuild"
	"github.com/bvivg/axon/core/services/rating/internal/store"
	eventsv1 "github.com/bvivg/axon/core/shared/gen/go/axon/events/v1"
)

type message struct {
	kind  string
	value []byte
}

type topic []message

func (t topic) Read(_ context.Context, visit func(kind string, value []byte) error) error {
	for _, m := range t {
		if err := visit(m.kind, m.value); err != nil {
			return err
		}
	}
	return nil
}

type live struct {
	book *ledger.Book
	err  error
}

func (l *live) Standings(context.Context) ([]store.Standing, error) {
	var out []store.Standing
	for key, player := range l.book.Players {
		out = append(out, store.Standing{Game: key.Game, Player: player})
	}
	return out, nil
}

func (l *live) Replace(_ context.Context, book *ledger.Book) error {
	if l.err != nil {
		return l.err
	}
	l.book = book
	return nil
}

var players = []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}

func game(t *testing.T, key string, a, b, winner int, rated bool) message {
	t.Helper()
	outcome, seat := eventsv1.GameFinished_OUTCOME_WIN, int32(winner)
	if winner < 0 {
		outcome = eventsv1.GameFinished_OUTCOME_DRAW
	}
	raw, err := proto.Marshal(&eventsv1.GameFinished{
		EventId: uuid.NewString(), SessionId: uuid.NewString(), Game: key, Players: []string{players[a].String(), players[b].String()},
		Outcome: outcome, WinnerSeat: seat, FinishedAt: timestamppb.New(time.Now()), Rated: rated,
	})
	if err != nil {
		t.Fatal(err)
	}
	return message{kind: "axon.events.v1.GameFinished", value: raw}
}

func history(t *testing.T) topic {
	first := game(t, "chess", 0, 1, 0, true)
	return topic{
		first,
		game(t, "chess", 1, 2, -1, true),
		first,
		game(t, "chess", 2, 0, 1, true),
		game(t, "tictactoe", 0, 2, 1, true),
		game(t, "tictactoe", 0, 2, 1, false),
		{kind: "axon.events.v1.GameFinished", value: []byte{0xff}},
		{kind: "axon.events.v1.UserRegistered", value: first.value},
	}
}

func redisBoard(t *testing.T) *leaderboard.Redis {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return leaderboard.New(client)
}

func TestRecomputeCountsEveryKindOfEvent(t *testing.T) {
	book, stats, err := rebuild.Recompute(context.Background(), history(t), glicko.Default)
	if err != nil {
		t.Fatal(err)
	}
	want := rebuild.Stats{Events: 8, Applied: 4, Duplicates: 1, Skipped: 2, Invalid: 1}
	if stats != want || len(book.Players) != 5 || len(book.Changes) != 8 {
		t.Fatalf("stats = %+v, players = %d, changes = %d", stats, len(book.Players), len(book.Changes))
	}
	again, _, err := rebuild.Recompute(context.Background(), history(t)[:0], glicko.Default)
	if err != nil || len(again.Players) != 0 {
		t.Fatal("an empty topic produced ratings", err)
	}
}

func TestCompareFindsDriftInPostgresAndRedis(t *testing.T) {
	ctx := context.Background()
	events := history(t)
	book, _, err := rebuild.Recompute(ctx, events, glicko.Default)
	if err != nil {
		t.Fatal(err)
	}
	livePlayers, _, err := rebuild.Recompute(ctx, events, glicko.Default)
	if err != nil {
		t.Fatal(err)
	}
	current := &live{book: livePlayers}
	board := redisBoard(t)
	if err := rebuild.Sync(ctx, current, board); err != nil {
		t.Fatal(err)
	}
	differences, err := rebuild.Compare(ctx, book, current, board)
	if err != nil || len(differences) != 0 {
		t.Fatalf("identical histories differ: %v %v", differences, err)
	}
	drifted := ledger.Key{Game: "chess", User: players[1]}
	player := current.book.Players[drifted]
	player.Rating.Rating += 0.5
	current.book.Players[drifted] = player
	delete(current.book.Players, ledger.Key{Game: "tictactoe", User: players[0]})
	if err := board.Update(ctx, "chess", ledger.Player{User: players[2], Rating: glicko.Rating{Rating: 1}}); err != nil {
		t.Fatal(err)
	}
	if err := board.Update(ctx, "quoridor", ledger.Newcomer(players[0])); err != nil {
		t.Fatal(err)
	}
	differences, err = rebuild.Compare(ctx, book, current, board)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, d := range differences {
		got = append(got, d.Store+" "+d.Game+" "+d.User.String())
	}
	want := []string{
		"postgres chess " + players[1].String(),
		"postgres tictactoe " + players[0].String(),
		"redis chess " + players[2].String(),
		"redis quoridor " + players[0].String(),
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("differences = %v, want %v", got, want)
	}
}

func TestApplyReplacesBothStores(t *testing.T) {
	ctx := context.Background()
	book, _, err := rebuild.Recompute(ctx, history(t), glicko.Default)
	if err != nil {
		t.Fatal(err)
	}
	current := &live{book: ledger.NewBook(glicko.Default)}
	board := redisBoard(t)
	if err := board.Update(ctx, "quoridor", ledger.Newcomer(players[0])); err != nil {
		t.Fatal(err)
	}
	if err := rebuild.Apply(ctx, book, current, board); err != nil {
		t.Fatal(err)
	}
	if differences, err := rebuild.Compare(ctx, book, current, board); err != nil || len(differences) != 0 {
		t.Fatalf("after apply: %v %v", differences, err)
	}
	if games, err := board.Games(ctx); err != nil || slices.Contains(games, "quoridor") {
		t.Fatal("a leaderboard without ratings survived", games, err)
	}
	refused := &live{book: ledger.NewBook(glicko.Default), err: store.ErrAhead}
	if err := rebuild.Apply(ctx, book, refused, board); !errors.Is(err, store.ErrAhead) {
		t.Fatal("apply ignored a refusal", err)
	}
}
