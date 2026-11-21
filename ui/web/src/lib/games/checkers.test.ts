import { describe, expect, it } from "vitest";

import { CheckersPiece } from "@/gen/axon/game/v1/game_pb";
import { chooseCheckersMove } from "@/lib/games/checkers-bot";
import { boardFrom, CheckersResult, colOf, legalMoves, maxPlies, movesOf, moveName, outcome, parseMove, Piece, play, playOn, quietLimit, rowOf, squareAt, startPosition, type CheckersPosition, type Seat } from "@/lib/games/checkers";

function at(pieces: Record<number, Piece>, turn: Seat = 0, quiet = 0, ply = 10): CheckersPosition {
  const board = Array.from({ length: 32 }, () => Piece.Empty);
  for (const [square, piece] of Object.entries(pieces)) board[Number(square) - 1] = piece;
  return { board, turn, quiet, ply };
}

function perft(board: Piece[], seat: Seat, depth: number): number {
  const moves = movesOf(board, seat);
  if (depth === 1) return moves.length;
  return moves.reduce((sum, move) => sum + perft(playOn(board, move), seat === 0 ? 1 : 0, depth - 1), 0);
}

const shortWin = "9-14 23-18 14x23 26x19 11-16 30-26 16x23x30 21-17 30x21x14 31-26 10-15 24-19 15x24x31 29-25 12-16 22-17 14x21x30x23 32-27 31x24 28x19x12 5-9".split(" ");

describe("checkers rules", () => {
  it("counts the same perft as the server", () => {
    expect([1, 2, 3, 4, 5].map(depth => perft(startPosition().board, 0, depth))).toEqual([7, 49, 302, 1469, 7361]);
  });

  it("numbers the dark squares like the standard board", () => {
    expect([0, 3, 4, 28, 31].map(square => [rowOf(square), colOf(square)])).toEqual([[0, 1], [0, 7], [1, 0], [7, 0], [7, 6]]);
    expect(squareAt(2, 1)).toBe(8);
    expect(squareAt(0, 0)).toBeUndefined();
  });

  it.each([
    ["black opening", startPosition(), ["9-13", "9-14", "10-14", "10-15", "11-15", "11-16", "12-16"]],
    ["capture is forced", at({ 15: Piece.BlackMan, 1: Piece.BlackMan, 18: Piece.WhiteMan, 32: Piece.WhiteMan }), ["15x22"]],
    ["any branch of a multi-jump", at({ 6: Piece.BlackMan, 10: Piece.WhiteMan, 18: Piece.WhiteMan, 19: Piece.WhiteMan, 27: Piece.WhiteMan }), ["6x15x22", "6x15x24x31"]],
    ["crowning ends the jump", at({ 22: Piece.BlackMan, 26: Piece.WhiteMan, 27: Piece.WhiteMan }), ["22x31"]],
    ["kings step every way", at({ 18: Piece.BlackKing, 1: Piece.WhiteMan }), ["18-22", "18-23", "18-14", "18-15"]],
    ["kings capture backwards", at({ 18: Piece.WhiteKing, 22: Piece.BlackMan, 1: Piece.BlackMan }, 1), ["18x25"]],
    ["a king may circle home", at({ 18: Piece.BlackKing, 15: Piece.WhiteMan, 7: Piece.WhiteMan, 6: Piece.WhiteMan, 14: Piece.WhiteMan }), ["18x9x2x11x18", "18x11x2x9x18"]],
  ] as const)("%s", (_, position, want) => {
    expect(legalMoves(position).toSorted()).toEqual([...want].toSorted());
  });

  it("parses and prints notation without accepting junk", () => {
    for (const raw of ["11-15", "22x15x6", "18x11x2x9x18"]) expect(moveName(parseMove(raw)!)).toBe(raw);
    for (const raw of ["", "11", "0-4", "33-29", "01-05", "11-15x19", "11-15-19", "1x".repeat(20) + "1"]) expect(parseMove(raw)).toBeUndefined();
  });

  it("plays the short game to a blockade win", () => {
    const final = shortWin.reduce<CheckersPosition>((position, move) => {
      expect(legalMoves(position)).toContain(move);
      return play(position, move);
    }, startPosition());
    expect(final.board.join("")).toBe("bbbb.bbbb..w..........B.........");
    expect(outcome(final)).toEqual({ result: CheckersResult.NoMoves, winner: 0 });
  });

  it("draws after forty quiet moves each and at the ply cap", () => {
    const kings = at({ 18: Piece.BlackKing, 1: Piece.WhiteKing }, 0, quietLimit - 1);
    expect(outcome(play(kings, "18-22"))).toEqual({ result: CheckersResult.FortyMoves });
    expect(play(at({ 9: Piece.BlackMan, 1: Piece.WhiteKing }, 0, quietLimit - 1), "9-13").quiet).toBe(0);
    expect(outcome(play(at({ 9: Piece.BlackMan, 1: Piece.WhiteKing }, 0, 0, maxPlies - 1), "9-13"))).toEqual({ result: CheckersResult.MoveLimit });
  });

  it("ignores illegal moves", () => {
    expect(play(startPosition(), "9-5")).toEqual(startPosition());
  });

  it("reads the server board", () => {
    const squares = Array.from({ length: 32 }, () => CheckersPiece.UNSPECIFIED);
    squares[0] = CheckersPiece.BLACK_KING;
    squares[31] = CheckersPiece.WHITE_MAN;
    expect(boardFrom(squares)[0]).toBe(Piece.BlackKing);
    expect(boardFrom(squares)[31]).toBe(Piece.WhiteMan);
    expect(boardFrom(undefined)).toEqual(startPosition().board);
  });
});

describe("checkers bot", () => {
  it("prefers the longer capture and crowning", () => {
    const position = at({ 6: Piece.BlackMan, 10: Piece.WhiteMan, 18: Piece.WhiteMan, 19: Piece.WhiteMan, 27: Piece.WhiteMan });
    expect(chooseCheckersMove(position, () => 0.5)).toBe("6x15x24x31");
  });

  it("does not hang a piece when a safe move exists", () => {
    const position = at({ 1: Piece.BlackMan, 11: Piece.BlackMan, 19: Piece.WhiteMan, 32: Piece.WhiteMan });
    expect(["1-5", "1-6"]).toContain(chooseCheckersMove(position, () => 0.5));
  });

  it("finishes games against itself with legal moves", () => {
    let seed = 7;
    const random = () => { seed = (seed * 16807) % 2147483647; return seed / 2147483647; };
    for (let game = 0; game < 5; game++) {
      let position = startPosition();
      while (!outcome(position)) {
        const move = chooseCheckersMove(position, random);
        expect(move && legalMoves(position).includes(move)).toBe(true);
        position = play(position, move!);
      }
      expect(position.ply).toBeLessThanOrEqual(maxPlies);
    }
  });
});
