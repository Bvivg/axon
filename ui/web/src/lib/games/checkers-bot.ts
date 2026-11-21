import { crownRow, isKing, movesOf, moveName, outcome, play, rowOf, type CheckersMove, type CheckersPosition } from "@/lib/games/checkers";

function captured(move: CheckersMove) {
  return move.jump ? move.path.length - 1 : 0;
}

function crowns(position: CheckersPosition, move: CheckersMove) {
  return !isKing(position.board[move.path[0]!]) && rowOf(move.path[move.path.length - 1]!) === crownRow(position.turn);
}

function threat(position: CheckersPosition) {
  return Math.max(0, ...movesOf(position.board, position.turn).map(captured));
}

export function chooseCheckersMove(position: CheckersPosition, random: () => number = Math.random): string | undefined {
  let best: string | undefined;
  let bestScore = -Infinity;
  for (const move of movesOf(position.board, position.turn)) {
    const name = moveName(move);
    const next = play(position, name);
    const won = outcome(next)?.winner === position.turn;
    const score = (won ? 1000 : 0) + 3 * captured(move) + (crowns(position, move) ? 2 : 0) - 3 * threat(next) + random() * 0.5;
    if (score > bestScore) { bestScore = score; best = name; }
  }
  return best;
}
