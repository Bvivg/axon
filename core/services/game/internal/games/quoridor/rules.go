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
	if b.walls[w] || b.walls[Wall{w.Anchor, flip(w.Dir)}] {
		return true
	}
	if w.Dir == Horizontal {
		return (c > 0 && b.walls[Wall{At(r, c-1), Horizontal}]) || (c < Size-2 && b.walls[Wall{At(r, c+1), Horizontal}])
	}
	return (r > 0 && b.walls[Wall{At(r-1, c), Vertical}]) || (r < Size-2 && b.walls[Wall{At(r+1, c), Vertical}])
}

func flip(d Orientation) Orientation {
	if d == Horizontal {
		return Vertical
	}
	return Horizontal
}

func (b Board) CanPlace(seat int, w Wall) bool {
	if b.Left[seat] == 0 || w.Anchor.Row() >= Size-1 || w.Anchor.Col() >= Size-1 || b.overlaps(w) {
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
	for r := 0; r < Size-1; r++ {
		for c := 0; c < Size-1; c++ {
			for _, d := range []Orientation{Horizontal, Vertical} {
				w := Wall{At(r, c), d}
				if b.CanPlace(seat, w) {
					out = append(out, Move{Wall: &w})
				}
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
