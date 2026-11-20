package chess

import (
	"errors"
	"fmt"
)

var ErrBadMove = errors.New("chess: malformed move")

type Move struct {
	From  int
	To    int
	Promo Kind
}

func (m Move) UCI() string {
	s := SquareName(m.From) + SquareName(m.To)
	if m.Promo != None {
		s += string(pieceLetters[m.Promo])
	}
	return s
}

func ParseUCI(s string) (Move, error) {
	if len(s) != 4 && len(s) != 5 {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, s)
	}
	from, ok1 := ParseSquare(s[0:2])
	to, ok2 := ParseSquare(s[2:4])
	if !ok1 || !ok2 {
		return Move{}, fmt.Errorf("%w: %q", ErrBadMove, s)
	}
	m := Move{From: from, To: to}
	if len(s) == 5 {
		switch s[4] {
		case 'n':
			m.Promo = Knight
		case 'b':
			m.Promo = Bishop
		case 'r':
			m.Promo = Rook
		case 'q':
			m.Promo = Queen
		default:
			return Move{}, fmt.Errorf("%w: promotion %q", ErrBadMove, s)
		}
	}
	return m, nil
}

var (
	knightSteps = [8][2]int{{1, 2}, {2, 1}, {2, -1}, {1, -2}, {-1, -2}, {-2, -1}, {-2, 1}, {-1, 2}}
	kingSteps   = [8][2]int{{1, 0}, {1, 1}, {0, 1}, {-1, 1}, {-1, 0}, {-1, -1}, {0, -1}, {1, -1}}
	rookDirs    = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	bishopDirs  = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}
	promotions  = [4]Kind{Queen, Rook, Bishop, Knight}
)

func pawnDir(c Color) int {
	if c == White {
		return 1
	}
	return -1
}

func (p Position) Attacked(sq int, by Color) bool {
	back := -pawnDir(by)
	for _, df := range [2]int{-1, 1} {
		if from := step(sq, df, back); from != noSquare && p.Board[from] == NewPiece(Pawn, by) {
			return true
		}
	}
	for _, s := range knightSteps {
		if from := step(sq, s[0], s[1]); from != noSquare && p.Board[from] == NewPiece(Knight, by) {
			return true
		}
	}
	for _, s := range kingSteps {
		if from := step(sq, s[0], s[1]); from != noSquare && p.Board[from] == NewPiece(King, by) {
			return true
		}
	}
	slide := func(dirs [4][2]int, kind Kind) bool {
		for _, d := range dirs {
			for cur := step(sq, d[0], d[1]); cur != noSquare; cur = step(cur, d[0], d[1]) {
				piece := p.Board[cur]
				if piece.Empty() {
					continue
				}
				if piece.Color() == by && (piece.Kind() == kind || piece.Kind() == Queen) {
					return true
				}
				break
			}
		}
		return false
	}
	return slide(rookDirs, Rook) || slide(bishopDirs, Bishop)
}

func (p Position) InCheck() bool {
	return p.Attacked(p.kingSquare(p.Turn), p.Turn.Other())
}

func (p Position) pseudoMoves() []Move {
	moves := make([]Move, 0, 48)
	us := p.Turn
	for from, piece := range p.Board {
		if piece.Empty() || piece.Color() != us {
			continue
		}
		switch piece.Kind() {
		case Pawn:
			moves = p.pawnMoves(moves, from)
		case Knight:
			moves = p.stepMoves(moves, from, knightSteps)
		case King:
			moves = p.stepMoves(moves, from, kingSteps)
			moves = p.castlingMoves(moves, from)
		case Bishop:
			moves = p.slideMoves(moves, from, bishopDirs[:])
		case Rook:
			moves = p.slideMoves(moves, from, rookDirs[:])
		case Queen:
			moves = p.slideMoves(moves, from, rookDirs[:])
			moves = p.slideMoves(moves, from, bishopDirs[:])
		}
	}
	return moves
}

func addPawn(moves []Move, from, to int) []Move {
	if r := rankOf(to); r == 0 || r == 7 {
		for _, k := range promotions {
			moves = append(moves, Move{From: from, To: to, Promo: k})
		}
		return moves
	}
	return append(moves, Move{From: from, To: to})
}

func (p Position) pawnMoves(moves []Move, from int) []Move {
	us := p.Turn
	dir := pawnDir(us)
	if one := step(from, 0, dir); one != noSquare && p.Board[one].Empty() {
		moves = addPawn(moves, from, one)
		start := 1
		if us == Black {
			start = 6
		}
		if two := step(from, 0, 2*dir); rankOf(from) == start && p.Board[two].Empty() {
			moves = append(moves, Move{From: from, To: two})
		}
	}
	for _, df := range [2]int{-1, 1} {
		to := step(from, df, dir)
		if to == noSquare {
			continue
		}
		target := p.Board[to]
		if (!target.Empty() && target.Color() != us) || to == p.EP {
			moves = addPawn(moves, from, to)
		}
	}
	return moves
}

func (p Position) stepMoves(moves []Move, from int, steps [8][2]int) []Move {
	for _, s := range steps {
		to := step(from, s[0], s[1])
		if to == noSquare {
			continue
		}
		if target := p.Board[to]; target.Empty() || target.Color() != p.Turn {
			moves = append(moves, Move{From: from, To: to})
		}
	}
	return moves
}

func (p Position) slideMoves(moves []Move, from int, dirs [][2]int) []Move {
	for _, d := range dirs {
		for to := step(from, d[0], d[1]); to != noSquare; to = step(to, d[0], d[1]) {
			target := p.Board[to]
			if target.Empty() {
				moves = append(moves, Move{From: from, To: to})
				continue
			}
			if target.Color() != p.Turn {
				moves = append(moves, Move{From: from, To: to})
			}
			break
		}
	}
	return moves
}

func (p Position) castlingMoves(moves []Move, from int) []Move {
	us, them := p.Turn, p.Turn.Other()
	rank := 0
	kingside, queenside := whiteKingside, whiteQueenside
	if us == Black {
		rank, kingside, queenside = 7, blackKingside, blackQueenside
	}
	if from != square(4, rank) || p.Attacked(from, them) {
		return moves
	}
	rook := NewPiece(Rook, us)
	empty := func(files ...int) bool {
		for _, f := range files {
			if !p.Board[square(f, rank)].Empty() {
				return false
			}
		}
		return true
	}
	safe := func(files ...int) bool {
		for _, f := range files {
			if p.Attacked(square(f, rank), them) {
				return false
			}
		}
		return true
	}
	if p.Castling&kingside != 0 && p.Board[square(7, rank)] == rook && empty(5, 6) && safe(5, 6) {
		moves = append(moves, Move{From: from, To: square(6, rank)})
	}
	if p.Castling&queenside != 0 && p.Board[square(0, rank)] == rook && empty(1, 2, 3) && safe(3, 2) {
		moves = append(moves, Move{From: from, To: square(2, rank)})
	}
	return moves
}

func (p Position) Play(m Move) Position {
	next := p
	piece := p.Board[m.From]
	captured := p.Board[m.To]
	us := p.Turn
	pawnMove := piece.Kind() == Pawn
	next.Board[m.From] = 0
	next.EP = noSquare
	switch piece.Kind() {
	case Pawn:
		if m.To == p.EP {
			next.Board[step(m.To, 0, -pawnDir(us))] = 0
			captured = NewPiece(Pawn, us.Other())
		}
		if d := rankOf(m.To) - rankOf(m.From); d == 2 || d == -2 {
			next.EP = step(m.From, 0, pawnDir(us))
		}
		if m.Promo != None {
			piece = NewPiece(m.Promo, us)
		}
	case King:
		if d := fileOf(m.To) - fileOf(m.From); d == 2 || d == -2 {
			rank := rankOf(m.From)
			rookFrom, rookTo := square(7, rank), square(5, rank)
			if d < 0 {
				rookFrom, rookTo = square(0, rank), square(3, rank)
			}
			next.Board[rookTo] = next.Board[rookFrom]
			next.Board[rookFrom] = 0
		}
		if us == White {
			next.Castling &^= whiteKingside | whiteQueenside
		} else {
			next.Castling &^= blackKingside | blackQueenside
		}
	}
	next.Board[m.To] = piece
	for _, corner := range []struct {
		sq   int
		flag uint8
	}{{square(0, 0), whiteQueenside}, {square(7, 0), whiteKingside}, {square(0, 7), blackQueenside}, {square(7, 7), blackKingside}} {
		if m.From == corner.sq || m.To == corner.sq {
			next.Castling &^= corner.flag
		}
	}
	if pawnMove || !captured.Empty() {
		next.Halfmove = 0
	} else {
		next.Halfmove++
	}
	if us == Black {
		next.Fullmove++
	}
	next.Turn = us.Other()
	return next
}

func (p Position) LegalMoves() []Move {
	pseudo := p.pseudoMoves()
	legal := pseudo[:0]
	for _, m := range pseudo {
		next := p.Play(m)
		if !next.Attacked(next.kingSquare(p.Turn), next.Turn) {
			legal = append(legal, m)
		}
	}
	return legal
}

func (p Position) isCapture(m Move) bool {
	return !p.Board[m.To].Empty() || (p.Board[m.From].Kind() == Pawn && m.To == p.EP)
}
