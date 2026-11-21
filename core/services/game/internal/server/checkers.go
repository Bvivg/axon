package server

import (
	"github.com/bvivg/axon/core/services/game/internal/games/checkers"
	gamev1 "github.com/bvivg/axon/core/shared/gen/go/axon/game/v1"
)

var checkersPieces = map[checkers.Piece]gamev1.CheckersPiece{
	checkers.BlackMan:  gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_MAN,
	checkers.BlackKing: gamev1.CheckersPiece_CHECKERS_PIECE_BLACK_KING,
	checkers.WhiteMan:  gamev1.CheckersPiece_CHECKERS_PIECE_WHITE_MAN,
	checkers.WhiteKing: gamev1.CheckersPiece_CHECKERS_PIECE_WHITE_KING,
}

var checkersEnds = map[checkers.End]gamev1.CheckersEnd{
	checkers.NoMoves:    gamev1.CheckersEnd_CHECKERS_END_NO_MOVES,
	checkers.FortyMoves: gamev1.CheckersEnd_CHECKERS_END_FORTY_MOVES,
	checkers.MoveLimit:  gamev1.CheckersEnd_CHECKERS_END_MOVE_LIMIT,
}

func checkersSquares(board string) []gamev1.CheckersPiece {
	out := make([]gamev1.CheckersPiece, len(board))
	for i := range len(board) {
		out[i] = checkersPieces[checkers.Piece(board[i])]
	}
	return out
}

func checkersBoard(snap checkers.Snapshot) *gamev1.CheckersBoard {
	out := &gamev1.CheckersBoard{
		Squares: checkersSquares(snap.Board), Moves: snap.Moves, LegalMoves: snap.Legal,
		End: checkersEnds[snap.End], QuietPlies: uint32(snap.Quiet),
	}
	if snap.LastMove != "" {
		out.LastMove = &snap.LastMove
	}
	return out
}
