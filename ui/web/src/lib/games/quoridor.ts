export const size = 9;
export const wallsEach = 10;

export enum WallDir {
  Horizontal = "h",
  Vertical = "v",
}

export type QuoridorPosition = {
  pawns: [number, number];
  left: [number, number];
  walls: Set<string>;
  turn: 0 | 1;
};

export const cellAt = (row: number, col: number) => row * size + col;
export const rowOf = (cell: number) => Math.floor(cell / size);
export const colOf = (cell: number) => cell % size;
export const cellName = (cell: number) => `${String.fromCharCode(97 + colOf(cell))}${rowOf(cell) + 1}`;
export const wallName = (row: number, col: number, dir: WallDir) => `${cellName(cellAt(row, col))}${dir}`;
export const goalRow = (seat: number) => seat === 0 ? size - 1 : 0;

export function parseCell(name: string): number | undefined {
  const col = name.charCodeAt(0) - 97;
  const row = Number(name.slice(1, 2)) - 1;
  if (name.length !== 2 || col < 0 || col >= size || !(row >= 0 && row < size)) return undefined;
  return cellAt(row, col);
}

export function parseWall(name: string): { row: number; col: number; dir: WallDir } | undefined {
  if (name.length !== 3) return undefined;
  const cell = parseCell(name.slice(0, 2));
  const dir = name[2] === WallDir.Horizontal ? WallDir.Horizontal : name[2] === WallDir.Vertical ? WallDir.Vertical : undefined;
  if (cell === undefined || !dir || rowOf(cell) >= size - 1 || colOf(cell) >= size - 1) return undefined;
  return { row: rowOf(cell), col: colOf(cell), dir };
}

export function startPosition(): QuoridorPosition {
  return { pawns: [cellAt(0, 4), cellAt(size - 1, 4)], left: [wallsEach, wallsEach], walls: new Set(), turn: 0 };
}

export function blocked(walls: Set<string>, from: number, to: number) {
  const fr = rowOf(from), fc = colOf(from), tr = rowOf(to), tc = colOf(to);
  if (fc === tc && Math.abs(fr - tr) === 1) {
    const row = Math.min(fr, tr);
    return walls.has(wallName(row, fc, WallDir.Horizontal)) || (fc > 0 && walls.has(wallName(row, fc - 1, WallDir.Horizontal)));
  }
  if (fr === tr && Math.abs(fc - tc) === 1) {
    const col = Math.min(fc, tc);
    return walls.has(wallName(fr, col, WallDir.Vertical)) || (fr > 0 && walls.has(wallName(fr - 1, col, WallDir.Vertical)));
  }
  return true;
}

function neighbours(walls: Set<string>, cell: number) {
  const out: number[] = [];
  const row = rowOf(cell), col = colOf(cell);
  for (const [dr, dc] of [[1, 0], [-1, 0], [0, 1], [0, -1]] as const) {
    const nr = row + dr, nc = col + dc;
    if (nr < 0 || nr >= size || nc < 0 || nc >= size) continue;
    const next = cellAt(nr, nc);
    if (!blocked(walls, cell, next)) out.push(next);
  }
  return out;
}

export function distance(walls: Set<string>, from: number, goal: number) {
  if (rowOf(from) === goal) return 0;
  const dist = new Array<number>(size * size).fill(-1);
  dist[from] = 0;
  const queue = [from];
  for (let head = 0; head < queue.length; head++) {
    const cur = queue[head]!;
    for (const next of neighbours(walls, cur)) {
      if (dist[next] !== -1) continue;
      dist[next] = dist[cur]! + 1;
      if (rowOf(next) === goal) return dist[next]!;
      queue.push(next);
    }
  }
  return -1;
}

export function pawnMoves(position: QuoridorPosition, seat: 0 | 1) {
  const me = position.pawns[seat], other = position.pawns[1 - seat]!;
  const out: number[] = [];
  for (const next of neighbours(position.walls, me)) {
    if (next !== other) { out.push(next); continue; }
    const dr = rowOf(other) - rowOf(me), dc = colOf(other) - colOf(me);
    const jr = rowOf(other) + dr, jc = colOf(other) + dc;
    if (jr >= 0 && jr < size && jc >= 0 && jc < size && !blocked(position.walls, other, cellAt(jr, jc))) { out.push(cellAt(jr, jc)); continue; }
    for (const side of neighbours(position.walls, other)) {
      if ((dr !== 0 && rowOf(side) === rowOf(other)) || (dc !== 0 && colOf(side) === colOf(other))) out.push(side);
    }
  }
  return out.sort((a, b) => a - b);
}

function overlaps(walls: Set<string>, row: number, col: number, dir: WallDir) {
  if (walls.has(wallName(row, col, WallDir.Horizontal)) || walls.has(wallName(row, col, WallDir.Vertical))) return true;
  if (dir === WallDir.Horizontal) return (col > 0 && walls.has(wallName(row, col - 1, dir))) || (col < size - 2 && walls.has(wallName(row, col + 1, dir)));
  return (row > 0 && walls.has(wallName(row - 1, col, dir))) || (row < size - 2 && walls.has(wallName(row + 1, col, dir)));
}

export function canPlace(position: QuoridorPosition, seat: 0 | 1, name: string) {
  const wall = parseWall(name);
  if (!wall || position.left[seat] === 0 || overlaps(position.walls, wall.row, wall.col, wall.dir)) return false;
  const walls = new Set(position.walls).add(name);
  return distance(walls, position.pawns[0], goalRow(0)) !== -1 && distance(walls, position.pawns[1], goalRow(1)) !== -1;
}

export function legalMoves(position: QuoridorPosition, seat: 0 | 1 = position.turn) {
  const out = pawnMoves(position, seat).map(cellName);
  if (position.left[seat] === 0) return out;
  for (let row = 0; row < size - 1; row++) {
    for (let col = 0; col < size - 1; col++) {
      for (const dir of [WallDir.Horizontal, WallDir.Vertical]) {
        const name = wallName(row, col, dir);
        if (canPlace(position, seat, name)) out.push(name);
      }
    }
  }
  return out;
}

export function isLegal(position: QuoridorPosition, move: string) {
  if (move.length === 3) return canPlace(position, position.turn, move);
  const cell = parseCell(move);
  return cell !== undefined && pawnMoves(position, position.turn).includes(cell);
}

export function play(position: QuoridorPosition, move: string): QuoridorPosition {
  const seat = position.turn;
  const next: QuoridorPosition = { pawns: [...position.pawns], left: [...position.left], walls: position.walls, turn: seat === 0 ? 1 : 0 };
  if (move.length === 3) {
    next.walls = new Set(position.walls).add(move);
    next.left[seat] -= 1;
  } else {
    next.pawns[seat] = parseCell(move)!;
  }
  return next;
}

export function winner(position: QuoridorPosition): 0 | 1 | undefined {
  if (rowOf(position.pawns[0]) === goalRow(0)) return 0;
  if (rowOf(position.pawns[1]) === goalRow(1)) return 1;
  return undefined;
}

export function fromSnapshot(pawns: string[], walls: string[], left: number[], turn: 0 | 1): QuoridorPosition {
  return {
    pawns: [parseCell(pawns[0] ?? "e1") ?? cellAt(0, 4), parseCell(pawns[1] ?? "e9") ?? cellAt(size - 1, 4)],
    left: [left[0] ?? wallsEach, left[1] ?? wallsEach],
    walls: new Set(walls),
    turn,
  };
}
