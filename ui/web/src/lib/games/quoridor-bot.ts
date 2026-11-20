import { canPlace, cellAt, colOf, distance, goalRow, pawnMoves, cellName, play, rowOf, size, wallName, WallDir, winner, type QuoridorPosition, blocked } from "@/lib/games/quoridor";

export enum QuoridorBotLevel {
  Easy = "easy",
  Medium = "medium",
  Hard = "hard",
}

const depths: Record<QuoridorBotLevel, number> = { [QuoridorBotLevel.Easy]: 1, [QuoridorBotLevel.Medium]: 2, [QuoridorBotLevel.Hard]: 3 };
const noise: Record<QuoridorBotLevel, number> = { [QuoridorBotLevel.Easy]: 2.5, [QuoridorBotLevel.Medium]: 0.6, [QuoridorBotLevel.Hard]: 0.05 };
const win = 1000;

function shortestPath(position: QuoridorPosition, seat: 0 | 1) {
  const goal = goalRow(seat);
  const from = position.pawns[seat];
  const parent = new Array<number>(size * size).fill(-2);
  parent[from] = -1;
  const queue = [from];
  for (let head = 0; head < queue.length; head++) {
    const cur = queue[head]!;
    if (rowOf(cur) === goal) {
      const path: number[] = [];
      for (let at = cur; at !== -1; at = parent[at]!) path.push(at);
      return path.reverse();
    }
    const row = rowOf(cur), col = colOf(cur);
    for (const [dr, dc] of [[1, 0], [-1, 0], [0, 1], [0, -1]] as const) {
      const nr = row + dr, nc = col + dc;
      if (nr < 0 || nr >= size || nc < 0 || nc >= size) continue;
      const next = cellAt(nr, nc);
      if (parent[next] !== -2 || blocked(position.walls, cur, next)) continue;
      parent[next] = cur;
      queue.push(next);
    }
  }
  return [from];
}

function evaluate(position: QuoridorPosition, seat: 0 | 1) {
  const done = winner(position);
  if (done !== undefined) return done === seat ? win : -win;
  const mine = distance(position.walls, position.pawns[seat], goalRow(seat));
  const theirs = distance(position.walls, position.pawns[1 - seat]!, goalRow(1 - seat));
  const tempo = position.turn === seat ? 0.5 : -0.5;
  return theirs - mine + tempo + 0.15 * (position.left[seat] - position.left[1 - seat]!);
}

function candidates(position: QuoridorPosition) {
  const seat = position.turn;
  const moves = pawnMoves(position, seat).map(cellName);
  if (position.left[seat] === 0) return moves;
  const walls = new Set<string>();
  const path = shortestPath(position, seat === 0 ? 1 : 0);
  for (const cell of path.slice(0, 5)) {
    for (let row = rowOf(cell) - 1; row <= rowOf(cell); row++) {
      for (let col = colOf(cell) - 1; col <= colOf(cell); col++) {
        if (row < 0 || col < 0 || row >= size - 1 || col >= size - 1) continue;
        for (const dir of [WallDir.Horizontal, WallDir.Vertical]) {
          const name = wallName(row, col, dir);
          if (canPlace(position, seat, name)) walls.add(name);
        }
      }
    }
  }
  return [...moves, ...walls];
}

function search(position: QuoridorPosition, depth: number, alpha: number, beta: number): number {
  const seat = position.turn;
  if (depth === 0 || winner(position) !== undefined) return evaluate(position, seat);
  let best = -Infinity;
  for (const move of candidates(position)) {
    const score = -search(play(position, move), depth - 1, -beta, -alpha);
    if (score > best) best = score;
    if (best > alpha) alpha = best;
    if (alpha >= beta) break;
  }
  return best;
}

export function chooseQuoridorMove(position: QuoridorPosition, level: QuoridorBotLevel, random: () => number = Math.random) {
  const moves = candidates(position);
  const depth = depths[level];
  let best: string | undefined;
  let bestScore = -Infinity;
  for (const move of moves) {
    const next = play(position, move);
    const immediate = winner(next) === position.turn ? win * 2 : 0;
    const score = immediate - search(next, depth - 1, -Infinity, Infinity) + (random() - 0.5) * noise[level] + (move.length === 3 ? -0.3 : 0);
    if (score > bestScore) { bestScore = score; best = move; }
  }
  return best;
}
