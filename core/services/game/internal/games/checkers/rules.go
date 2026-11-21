package checkers

func (b Board) LegalMoves(side int) []Move {
	if jumps := b.Jumps(side); len(jumps) > 0 {
		return jumps
	}
	return b.Steps(side)
}

func (b Board) Steps(side int) []Move {
	var out []Move
	for s, p := range b {
		if p.Side() != side {
			continue
		}
		for _, d := range directions(p) {
			if to := neighbours[s][d]; to != noSquare && b[to] == Empty {
				out = append(out, Move{Path: []Square{Square(s), to}})
			}
		}
	}
	return out
}

func (b Board) Jumps(side int) []Move {
	var out []Move
	for s, p := range b {
		if p.Side() != side {
			continue
		}
		rest := b
		rest[s] = Empty
		rest.extend(p, []Square{Square(s)}, &out)
	}
	return out
}

func (b Board) extend(p Piece, path []Square, out *[]Move) {
	at := path[len(path)-1]
	found := false
	for _, d := range directions(p) {
		over, to := neighbours[at][d], landings[at][d]
		if to == noSquare || b[to] != Empty || b[over].Side() != 1-p.Side() {
			continue
		}
		found = true
		next := b
		next[over] = Empty
		route := append(append(make([]Square, 0, len(path)+1), path...), to)
		if !p.King() && to.Row() == crownRow(p.Side()) {
			*out = append(*out, Move{Path: route, Jump: true})
			continue
		}
		next.extend(p, route, out)
	}
	if !found && len(path) > 1 {
		*out = append(*out, Move{Path: path, Jump: true})
	}
}

func (b Board) Play(m Move) Board {
	from, to := m.From(), m.To()
	p := b[from]
	b[from] = Empty
	if m.Jump {
		for i := 1; i < len(m.Path); i++ {
			if over := between(m.Path[i-1], m.Path[i]); over != noSquare {
				b[over] = Empty
			}
		}
	}
	if !p.King() && to.Row() == crownRow(p.Side()) {
		p = p.crowned()
	}
	b[to] = p
	return b
}

func between(from, to Square) Square {
	for d := range diagonals {
		if landings[from][d] == to {
			return neighbours[from][d]
		}
	}
	return noSquare
}

func (b Board) Legal(side int, m Move) (Move, bool) {
	want := m.String()
	for _, legal := range b.LegalMoves(side) {
		if legal.String() == want {
			return legal, true
		}
	}
	return Move{}, false
}
