import { Chess, type Move } from "chess.js";

export enum BotLevel {
  Easy = "easy",
  Medium = "medium",
  Hard = "hard",
}

const values: Record<string, number> = { p: 100, n: 320, b: 330, r: 500, q: 900, k: 0 };

const pawnTable = [
  0, 0, 0, 0, 0, 0, 0, 0,
  50, 50, 50, 50, 50, 50, 50, 50,
  10, 10, 20, 30, 30, 20, 10, 10,
  5, 5, 10, 25, 25, 10, 5, 5,
  0, 0, 0, 20, 20, 0, 0, 0,
  5, -5, -10, 0, 0, -10, -5, 5,
  5, 10, 10, -20, -20, 10, 10, 5,
  0, 0, 0, 0, 0, 0, 0, 0,
];

const knightTable = [
  -50, -40, -30, -30, -30, -30, -40, -50,
  -40, -20, 0, 0, 0, 0, -20, -40,
  -30, 0, 10, 15, 15, 10, 0, -30,
  -30, 5, 15, 20, 20, 15, 5, -30,
  -30, 0, 15, 20, 20, 15, 0, -30,
  -30, 5, 10, 15, 15, 10, 5, -30,
  -40, -20, 0, 5, 5, 0, -20, -40,
  -50, -40, -30, -30, -30, -30, -40, -50,
];

const centreTable = [
  -20, -10, -10, -10, -10, -10, -10, -20,
  -10, 0, 0, 0, 0, 0, 0, -10,
  -10, 0, 5, 10, 10, 5, 0, -10,
  -10, 5, 5, 10, 10, 5, 5, -10,
  -10, 0, 10, 10, 10, 10, 0, -10,
  -10, 10, 10, 10, 10, 10, 10, -10,
  -10, 5, 0, 0, 0, 0, 5, -10,
  -20, -10, -10, -10, -10, -10, -10, -20,
];

const tables: Record<string, number[]> = { p: pawnTable, n: knightTable, b: centreTable, q: centreTable };

const depths: Record<BotLevel, number> = { [BotLevel.Easy]: 1, [BotLevel.Medium]: 2, [BotLevel.Hard]: 3 };
const mate = 100_000;

function evaluate(chess: Chess): number {
  let score = 0;
  chess.board().forEach((row, rank) => row.forEach((square, file) => {
    if (!square) return;
    const index = square.color === "w" ? rank * 8 + file : (7 - rank) * 8 + file;
    const value = values[square.type]! + (tables[square.type]?.[index] ?? 0);
    score += square.color === "w" ? value : -value;
  }));
  return chess.turn() === "w" ? score : -score;
}

function ordered(chess: Chess): Move[] {
  return chess.moves({ verbose: true }).sort((a, b) =>
    (b.captured ? values[b.captured]! * 10 - values[b.piece]! : 0) + (b.promotion ? 800 : 0)
    - ((a.captured ? values[a.captured]! * 10 - values[a.piece]! : 0) + (a.promotion ? 800 : 0)));
}

function search(chess: Chess, depth: number, alpha: number, beta: number, ply: number): number {
  if (chess.isCheckmate()) return -mate + ply;
  if (chess.isDraw()) return 0;
  if (depth === 0) return evaluate(chess);
  let best = -Infinity;
  for (const move of ordered(chess)) {
    chess.move(move);
    const score = -search(chess, depth - 1, -beta, -alpha, ply + 1);
    chess.undo();
    if (score > best) best = score;
    if (score > alpha) alpha = score;
    if (alpha >= beta) break;
  }
  return best;
}

export const uciOf = (move: Pick<Move, "from" | "to" | "promotion">) => `${move.from}${move.to}${move.promotion ?? ""}`;

export function chooseMove(fen: string, level: BotLevel, random: () => number = Math.random): string | null {
  const chess = new Chess(fen);
  const moves = ordered(chess);
  if (!moves.length) return null;
  const depth = depths[level];
  const noise = level === BotLevel.Easy ? 120 : level === BotLevel.Medium ? 25 : 0;
  let best: Move = moves[0]!;
  let bestScore = -Infinity;
  for (const move of moves) {
    chess.move(move);
    const score = -search(chess, depth - 1, -Infinity, Infinity, 1) + (noise ? (random() - 0.5) * noise : 0);
    chess.undo();
    if (score > bestScore) {
      bestScore = score;
      best = move;
    }
  }
  return uciOf(best);
}
