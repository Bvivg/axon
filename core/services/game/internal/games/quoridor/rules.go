package quoridor

import "sort"

func (b Board) PawnMoves(seat int) []Cell {
	me, other := b.Pawns[seat], b.Pawns[1-seat]
	var out []Cell
	for _, next := range b.neighbours(me) {
		if next != other {
			out = append(out, next)
			continue
		}
		dr, dc := other.Row()-me.Row(), other.Col()-me.Col()
		jr, jc := other.Row()+dr, other.Col()+dc
		if jr >= 0 && jr < Size && jc >= 0 && jc < Size && !b.Blocked(other, At(jr, jc)) {
			out = append(out, At(jr, jc))
			continue
		}
		for _, side := range b.neighbours(other) {
			if (dr != 0 && side.Row() == other.Row()) || (dc != 0 && side.Col() == other.Col()) {
				out = append(out, side)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (b Board) overlaps(w Wall) bool {
	r, c := w.Anchor.Row(), w.Anchor.Col()
	if b.wall(r, c, Horizontal) || b.wall(r, c, Vertical) {
		return true
	}
	if w.Dir == Horizontal {
		return (c > 0 && b.wall(r, c-1, Horizontal)) || (c < anchors-1 && b.wall(r, c+1, Horizontal))
	}
	return (r > 0 && b.wall(r-1, c, Vertical)) || (r < anchors-1 && b.wall(r+1, c, Vertical))
}

func (b Board) placeable(seat int, w Wall) bool {
	return b.Left[seat] > 0 && w.Anchor.Row() < anchors && w.Anchor.Col() < anchors && !b.overlaps(w)
}

func (b Board) CanPlace(seat int, w Wall) bool {
	if !b.placeable(seat, w) {
		return false
	}
	next := b.with(w)
	return HasPath(next, next.Pawns[0], GoalRow(0)) && HasPath(next, next.Pawns[1], GoalRow(1))
}

func (b Board) LegalMoves(seat int) []Move {
	var out []Move
	for _, to := range b.PawnMoves(seat) {
		out = append(out, Move{To: to})
	}
	if b.Left[seat] == 0 {
		return out
	}
	white, okWhite := cuts(b, b.Pawns[0], GoalRow(0))
	black, okBlack := cuts(b, b.Pawns[1], GoalRow(1))
	if !okWhite || !okBlack {
		return out
	}
	critical := [2]uint64{white[0] | black[0], white[1] | black[1]}
	for r := 0; r < anchors; r++ {
		for c := 0; c < anchors; c++ {
			for _, d := range [2]Orientation{Horizontal, Vertical} {
				w := Wall{At(r, c), d}
				if !b.placeable(seat, w) {
					continue
				}
				if critical[orientation(d)]&bit(r, c) != 0 && !b.CanPlace(seat, w) {
					continue
				}
				out = append(out, Move{Wall: &w})
			}
		}
	}
	return out
}

func (b Board) Legal(seat int, m Move) bool {
	if m.Wall != nil {
		return b.CanPlace(seat, *m.Wall)
	}
	for _, to := range b.PawnMoves(seat) {
		if to == m.To {
			return true
		}
	}
	return false
}

func (b Board) Play(seat int, m Move) Board {
	if m.Wall != nil {
		next := b.with(*m.Wall)
		next.Left[seat]--
		return next
	}
	next := b
	next.Pawns[seat] = m.To
	return next
}
