package chess

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

var ErrBadFEN = errors.New("chess: malformed FEN")

type Color uint8

const (
	White Color = 0
	Black Color = 1
)

func (c Color) Other() Color { return c ^ 1 }

type Kind uint8

const (
	None Kind = iota
	Pawn
	Knight
	Bishop
	Rook
	Queen
	King
)

type Piece uint8

func NewPiece(kind Kind, color Color) Piece { return Piece(kind) | Piece(color)<<3 }

func (p Piece) Kind() Kind   { return Kind(p & 7) }
func (p Piece) Color() Color { return Color(p >> 3) }
func (p Piece) Empty() bool  { return p.Kind() == None }

const (
	whiteKingside uint8 = 1 << iota
	whiteQueenside
	blackKingside
	blackQueenside
)

const noSquare = -1

const StartFEN = "rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1"

type Position struct {
	Board    [64]Piece
	Turn     Color
	Castling uint8
	EP       int
	Halfmove int
	Fullmove int
}

func square(file, rank int) int { return rank*8 + file }
func fileOf(sq int) int         { return sq % 8 }
func rankOf(sq int) int         { return sq / 8 }

func step(sq, df, dr int) int {
	f, r := fileOf(sq)+df, rankOf(sq)+dr
	if f < 0 || f > 7 || r < 0 || r > 7 {
		return noSquare
	}
	return square(f, r)
}

func SquareName(sq int) string {
	return string([]byte{byte('a' + fileOf(sq)), byte('1' + rankOf(sq))})
}

func ParseSquare(s string) (int, bool) {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return noSquare, false
	}
	return square(int(s[0]-'a'), int(s[1]-'1')), true
}

const pieceLetters = " pnbrqk"

func (p Piece) letter() byte {
	c := pieceLetters[p.Kind()]
	if p.Color() == White {
		c -= 'a' - 'A'
	}
	return c
}

func pieceFromLetter(c byte) (Piece, bool) {
	color := White
	lower := c
	if c >= 'a' && c <= 'z' {
		color = Black
	} else {
		lower = c + ('a' - 'A')
	}
	i := strings.IndexByte(pieceLetters, lower)
	if i <= 0 {
		return 0, false
	}
	return NewPiece(Kind(i), color), true
}

func ParseFEN(fen string) (Position, error) {
	fields := strings.Fields(fen)
	if len(fields) != 6 {
		return Position{}, fmt.Errorf("%w: want 6 fields, got %d", ErrBadFEN, len(fields))
	}
	var pos Position
	ranks := strings.Split(fields[0], "/")
	if len(ranks) != 8 {
		return Position{}, fmt.Errorf("%w: want 8 ranks", ErrBadFEN)
	}
	kings := [2]int{}
	for i, row := range ranks {
		rank, file := 7-i, 0
		for j := 0; j < len(row); j++ {
			c := row[j]
			if c >= '1' && c <= '8' {
				file += int(c - '0')
				continue
			}
			piece, ok := pieceFromLetter(c)
			if !ok || file > 7 {
				return Position{}, fmt.Errorf("%w: rank %d", ErrBadFEN, rank+1)
			}
			if piece.Kind() == King {
				kings[piece.Color()]++
			}
			pos.Board[square(file, rank)] = piece
			file++
		}
		if file != 8 {
			return Position{}, fmt.Errorf("%w: rank %d has %d files", ErrBadFEN, rank+1, file)
		}
	}
	if kings != [2]int{1, 1} {
		return Position{}, fmt.Errorf("%w: each side needs one king", ErrBadFEN)
	}
	switch fields[1] {
	case "w":
		pos.Turn = White
	case "b":
		pos.Turn = Black
	default:
		return Position{}, fmt.Errorf("%w: side to move %q", ErrBadFEN, fields[1])
	}
	if fields[2] != "-" {
		for _, c := range fields[2] {
			switch c {
			case 'K':
				pos.Castling |= whiteKingside
			case 'Q':
				pos.Castling |= whiteQueenside
			case 'k':
				pos.Castling |= blackKingside
			case 'q':
				pos.Castling |= blackQueenside
			default:
				return Position{}, fmt.Errorf("%w: castling %q", ErrBadFEN, fields[2])
			}
		}
	}
	pos.EP = noSquare
	if fields[3] != "-" {
		sq, ok := ParseSquare(fields[3])
		if !ok {
			return Position{}, fmt.Errorf("%w: en passant %q", ErrBadFEN, fields[3])
		}
		pos.EP = sq
	}
	var err error
	if pos.Halfmove, err = strconv.Atoi(fields[4]); err != nil || pos.Halfmove < 0 {
		return Position{}, fmt.Errorf("%w: halfmove clock %q", ErrBadFEN, fields[4])
	}
	if pos.Fullmove, err = strconv.Atoi(fields[5]); err != nil || pos.Fullmove < 1 {
		return Position{}, fmt.Errorf("%w: move number %q", ErrBadFEN, fields[5])
	}
	return pos, nil
}

func (p Position) placement() string {
	var b strings.Builder
	for rank := 7; rank >= 0; rank-- {
		empty := 0
		for file := range 8 {
			piece := p.Board[square(file, rank)]
			if piece.Empty() {
				empty++
				continue
			}
			if empty > 0 {
				b.WriteByte(byte('0' + empty))
				empty = 0
			}
			b.WriteByte(piece.letter())
		}
		if empty > 0 {
			b.WriteByte(byte('0' + empty))
		}
		if rank > 0 {
			b.WriteByte('/')
		}
	}
	return b.String()
}

func (p Position) castlingField() string {
	var b strings.Builder
	for _, r := range []struct {
		flag uint8
		c    byte
	}{{whiteKingside, 'K'}, {whiteQueenside, 'Q'}, {blackKingside, 'k'}, {blackQueenside, 'q'}} {
		if p.Castling&r.flag != 0 {
			b.WriteByte(r.c)
		}
	}
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}

func (p Position) turnField() string {
	if p.Turn == White {
		return "w"
	}
	return "b"
}

func (p Position) FEN() string {
	ep := "-"
	if p.EP != noSquare {
		ep = SquareName(p.EP)
	}
	return fmt.Sprintf("%s %s %s %s %d %d", p.placement(), p.turnField(), p.castlingField(), ep, p.Halfmove, p.Fullmove)
}

func (p Position) kingSquare(color Color) int {
	king := NewPiece(King, color)
	for sq, piece := range p.Board {
		if piece == king {
			return sq
		}
	}
	return noSquare
}
