package chess

import "strings"

type End string

const (
	Ongoing              End = ""
	Checkmate            End = "checkmate"
	Stalemate            End = "stalemate"
	InsufficientMaterial End = "insufficient_material"
	ThreefoldRepetition  End = "threefold_repetition"
	FiftyMoves           End = "fifty_moves"
)

func (p Position) RepetitionKey() string {
	ep := "-"
	if p.EP != noSquare {
		for _, m := range p.LegalMoves() {
			if m.To == p.EP && p.Board[m.From].Kind() == Pawn {
				ep = SquareName(p.EP)
				break
			}
		}
	}
	return strings.Join([]string{p.placement(), p.turnField(), p.castlingField(), ep}, " ")
}

func (p Position) insufficientMaterial() bool {
	bishopColors := map[int]bool{}
	minors := 0
	for sq, piece := range p.Board {
		switch piece.Kind() {
		case None, King:
		case Bishop:
			minors++
			bishopColors[(fileOf(sq)+rankOf(sq))%2] = true
		case Knight:
			minors++
		default:
			return false
		}
	}
	if minors <= 1 {
		return true
	}
	return len(bishopColors) == 1 && minors == countKind(p, Bishop)
}

func countKind(p Position, kind Kind) int {
	n := 0
	for _, piece := range p.Board {
		if piece.Kind() == kind {
			n++
		}
	}
	return n
}

func Outcome(p Position, positions []string) End {
	if len(p.LegalMoves()) == 0 {
		if p.InCheck() {
			return Checkmate
		}
		return Stalemate
	}
	if p.insufficientMaterial() {
		return InsufficientMaterial
	}
	if len(positions) > 0 {
		last, seen := positions[len(positions)-1], 0
		for _, key := range positions {
			if key == last {
				seen++
			}
		}
		if seen >= 3 {
			return ThreefoldRepetition
		}
	}
	if p.Halfmove >= 100 {
		return FiftyMoves
	}
	return Ongoing
}
