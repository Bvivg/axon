package chess

import "strings"

func (p Position) SAN(m Move) string {
	piece := p.Board[m.From]
	var b strings.Builder
	switch {
	case piece.Kind() == King && fileOf(m.To)-fileOf(m.From) == 2:
		b.WriteString("O-O")
	case piece.Kind() == King && fileOf(m.To)-fileOf(m.From) == -2:
		b.WriteString("O-O-O")
	case piece.Kind() == Pawn:
		if p.isCapture(m) {
			b.WriteByte(byte('a' + fileOf(m.From)))
			b.WriteByte('x')
		}
		b.WriteString(SquareName(m.To))
		if m.Promo != None {
			b.WriteByte('=')
			b.WriteByte(NewPiece(m.Promo, White).letter())
		}
	default:
		b.WriteByte(NewPiece(piece.Kind(), White).letter())
		sameFile, sameRank, ambiguous := false, false, false
		for _, other := range p.LegalMoves() {
			if other.To != m.To || other.From == m.From || p.Board[other.From] != piece {
				continue
			}
			ambiguous = true
			sameFile = sameFile || fileOf(other.From) == fileOf(m.From)
			sameRank = sameRank || rankOf(other.From) == rankOf(m.From)
		}
		if ambiguous {
			switch {
			case !sameFile:
				b.WriteByte(byte('a' + fileOf(m.From)))
			case !sameRank:
				b.WriteByte(byte('1' + rankOf(m.From)))
			default:
				b.WriteString(SquareName(m.From))
			}
		}
		if p.isCapture(m) {
			b.WriteByte('x')
		}
		b.WriteString(SquareName(m.To))
	}
	next := p.Play(m)
	if next.InCheck() {
		if len(next.LegalMoves()) == 0 {
			b.WriteByte('#')
		} else {
			b.WriteByte('+')
		}
	}
	return b.String()
}
