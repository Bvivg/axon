package server

import (
	"github.com/bvivg/axon/core/services/game/internal/games/durak"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

var durakEnds = map[durak.End]gamev1.DurakEnd{
	durak.Lost:  gamev1.DurakEnd_DURAK_END_DURAK,
	durak.Drawn: gamev1.DurakEnd_DURAK_END_DRAW,
}

func durakBoard(snap durak.Snapshot) *gamev1.DurakBoard {
	out := &gamev1.DurakBoard{
		Trump: snap.Trump, Stock: uint32(snap.Stock), Discard: uint32(snap.Discard), AttackerSeat: uint32(snap.Attacker),
		Taking: snap.Taking, BoutLimit: uint32(snap.Limit), Moves: snap.Moves, LegalMoves: snap.Legal, End: durakEnds[snap.End],
		DefenderSeat: uint32(max(snap.Defender, 0)),
	}
	for _, seat := range snap.Passed {
		out.Passed = append(out.Passed, uint32(seat))
	}
	for _, place := range snap.Places {
		out.Places = append(out.Places, uint32(place))
	}
	for _, hand := range snap.Hands {
		out.Hands = append(out.Hands, &gamev1.DurakHand{Cards: hand.Cards, Count: uint32(hand.Count), Hidden: hand.Hidden})
	}
	for _, pair := range snap.Table {
		item := &gamev1.DurakPair{Attack: pair.Attack, By: uint32(max(pair.By, 0)), Checked: pair.Checked}
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
