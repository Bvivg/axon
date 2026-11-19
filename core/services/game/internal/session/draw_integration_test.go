package session

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/services/game/internal/engine"
	"github.com/bvivg/axon/core/services/game/internal/games/chess"
)

func chessData(uci string) json.RawMessage {
	raw, _ := json.Marshal(chess.MoveData{UCI: uci})
	return raw
}

func TestChessDrawOffersAndRating(t *testing.T) {
	store, ctx := queueTest(t)
	white, black := uuid.New(), uuid.New()
	cleanupUsers(t, store, ctx, white, black)
	seedRating(t, store, ctx, white, 1300)
	seedRating(t, store, ctx, black, 1300)
	if _, err := store.pool.Exec(ctx, `UPDATE game.ratings SET game=$1 WHERE user_id = ANY($2)`, chess.Key, []uuid.UUID{white, black}); err != nil {
		t.Fatal(err)
	}
	for _, user := range []uuid.UUID{white, black} {
		if _, err := store.FindMatch(ctx, user, uuid.New(), chess.Key); err != nil {
			t.Fatal(err)
		}
	}
	created, err := store.MatchRound(ctx)
	if err != nil || len(created) != 1 {
		t.Fatal(created, err)
	}
	game := created[0]
	if game.Clock.RemainingMS[0] != store.TimeLimitMS(chess.Key) || store.TimeLimitMS(chess.Key) != 600_000 {
		t.Fatal("chess clock", game.Clock)
	}
	first, second := game.Creator, *game.Opponent
	if _, err := store.Move(ctx, game.ID, first, 0, "tictactoe", chessData("e2e4"), nil); !errors.Is(err, engine.ErrGameMismatch) {
		t.Fatal("wrong game accepted", err)
	}
	if _, err := store.Move(ctx, game.ID, first, 0, chess.Key, chessData("e2e5"), nil); !errors.Is(err, chess.ErrIllegalMove) {
		t.Fatal("illegal move accepted", err)
	}
	if _, err := store.Draw(ctx, game.ID, second, AcceptDraw); !errors.Is(err, ErrNoDrawOffer) {
		t.Fatal("accepted a missing offer", err)
	}
	game, err = store.Draw(ctx, game.ID, first, OfferDraw)
	if err != nil || game.DrawOffer == nil || *game.DrawOffer != first {
		t.Fatal("offer", game.DrawOffer, err)
	}
	if _, err := store.Draw(ctx, game.ID, first, AcceptDraw); !errors.Is(err, ErrNoDrawOffer) {
		t.Fatal("accepted own offer", err)
	}
	game, err = store.Move(ctx, game.ID, first, 0, chess.Key, chessData("e2e4"), nil)
	if err != nil || game.DrawOffer != nil {
		t.Fatal("move kept the offer", game.DrawOffer, err)
	}
	game, err = store.Draw(ctx, game.ID, second, OfferDraw)
	if err != nil {
		t.Fatal(err)
	}
	game, err = store.Draw(ctx, game.ID, first, DeclineDraw)
	if err != nil || game.DrawOffer != nil || game.State.Over() {
		t.Fatal("decline", game.DrawOffer, err)
	}
	if _, err := store.Draw(ctx, game.ID, second, OfferDraw); err != nil {
		t.Fatal(err)
	}
	game, err = store.Draw(ctx, game.ID, first, AcceptDraw)
	if err != nil || !game.AgreedDraw || game.State.Status != engine.StatusDraw {
		t.Fatal("accept", game.AgreedDraw, game.State.Status, err)
	}
	ratings, err := store.Ratings(ctx, chess.Key, []uuid.UUID{white, black})
	if err != nil || ratings[white].Rating != 1300 || ratings[white].Draws != 1 || ratings[black].Draws != 1 {
		t.Fatal("draw rating", ratings, err)
	}
	if _, err := store.Draw(ctx, game.ID, second, OfferDraw); !errors.Is(err, ErrNotPlaying) {
		t.Fatal("offer after the end", err)
	}
}

func TestChessCheckmateSettles(t *testing.T) {
	store, ctx := queueTest(t)
	a, b := uuid.New(), uuid.New()
	cleanupUsers(t, store, ctx, a, b)
	for _, user := range []uuid.UUID{a, b} {
		if _, err := store.FindMatch(ctx, user, uuid.New(), chess.Key); err != nil {
			t.Fatal(err)
		}
	}
	created, err := store.MatchRound(ctx)
	if err != nil || len(created) != 1 {
		t.Fatal(created, err)
	}
	game := created[0]
	players := []uuid.UUID{game.Creator, *game.Opponent}
	for ply, uci := range []string{"f2f3", "e7e5", "g2g4", "d8h4"} {
		game, err = store.Move(ctx, game.ID, players[ply%2], ply, chess.Key, chessData(uci), &game.Revision)
		if err != nil {
			t.Fatal(uci, err)
		}
	}
	if game.State.Status != engine.StatusWin || game.State.Winner != 1 {
		t.Fatal(game.State)
	}
	ratings, err := store.Ratings(ctx, chess.Key, players)
	if err != nil || ratings[players[1]].Rating != 1220 || ratings[players[0]].Rating != 1180 {
		t.Fatal(ratings, err)
	}
}
