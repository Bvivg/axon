package server

import (
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

var durakEnds = map[durak.End]gamev1.DurakEnd{
	durak.Lost:      gamev1.DurakEnd_DURAK_END_DURAK,
	durak.Drawn:     gamev1.DurakEnd_DURAK_END_DRAW,
	durak.MoveLimit: gamev1.DurakEnd_DURAK_END_MOVE_LIMIT,
}

func durakBoard(snap durak.Snapshot) *gamev1.DurakBoard {
	out := &gamev1.DurakBoard{
		Trump: snap.Trump, Stock: uint32(snap.Stock), Discard: uint32(snap.Discard), AttackerSeat: uint32(snap.Attacker),
		Taking: snap.Taking, BoutLimit: uint32(snap.Limit), Moves: snap.Moves, LegalMoves: snap.Legal, End: durakEnds[snap.End],
	}
	for _, hand := range snap.Hands {
		out.Hands = append(out.Hands, &gamev1.DurakHand{Cards: hand.Cards, Count: uint32(hand.Count), Hidden: hand.Hidden})
	}
	for _, pair := range snap.Table {
		item := &gamev1.DurakPair{Attack: pair.Attack}
		if pair.Defense != "" {
			item.Defense = &pair.Defense
		}
		out.Table = append(out.Table, item)
	}
	if snap.LastMove != "" {
		out.LastMove = &snap.LastMove
	}
	return out
}
