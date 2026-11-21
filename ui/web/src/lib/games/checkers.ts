import { CheckersPiece } from "@/gen/axon/game/v1/game_pb";

export const squareCount = 32;
export const boardSize = 8;
export const quietLimit = 80;
export const maxPlies = 500;
export const maxNotation = 38;

export enum Piece {
  Empty = ".",
  BlackMan = "b",
  BlackKing = "B",
  WhiteMan = "w",
  WhiteKing = "W",
}

export enum CheckersResult {
  NoMoves = "no_moves",
  FortyMoves = "forty_moves",
  MoveLimit = "move_limit",
}

export type Seat = 0 | 1;

export type CheckersPosition = {
  board: Piece[];
  turn: Seat;
  quiet: number;
  ply: number;
};

export type CheckersMove = { path: number[]; jump: boolean };

const diagonals = [[1, -1], [1, 1], [-1, -1], [-1, 1]] as const;

export const rowOf = (square: number) => Math.floor(square / 4);
export const colOf = (square: number) => 2 * (square % 4) + 1 - (rowOf(square) % 2);

export function squareAt(row: number, col: number): number | undefined {
  if (row < 0 || row >= boardSize || col < 0 || col >= boardSize || (row + col) % 2 === 0) return undefined;
  return row * 4 + Math.floor(col / 2);
}

export function sideOf(piece: Piece | undefined): Seat | undefined {
  if (piece === Piece.BlackMan || piece === Piece.BlackKing) return 0;
  if (piece === Piece.WhiteMan || piece === Piece.WhiteKing) return 1;
  return undefined;
}

export const isKing = (piece: Piece | undefined) => piece === Piece.BlackKing || piece === Piece.WhiteKing;
export const crownRow = (seat: Seat) => seat === 0 ? boardSize - 1 : 0;

const fromProtoPiece: Record<CheckersPiece, Piece> = {
  [CheckersPiece.UNSPECIFIED]: Piece.Empty,
  [CheckersPiece.BLACK_MAN]: Piece.BlackMan,
  [CheckersPiece.BLACK_KING]: Piece.BlackKing,
  [CheckersPiece.WHITE_MAN]: Piece.WhiteMan,
  [CheckersPiece.WHITE_KING]: Piece.WhiteKing,
};

export function startBoard(): Piece[] {
  return Array.from({ length: squareCount }, (_, square) => square < 12 ? Piece.BlackMan : square >= 20 ? Piece.WhiteMan : Piece.Empty);
}

export function boardFrom(squares: CheckersPiece[] | undefined): Piece[] {
  if (!squares || squares.length !== squareCount) return startBoard();
  return squares.map(piece => fromProtoPiece[piece] ?? Piece.Empty);
}

export function startPosition(): CheckersPosition {
  return { board: startBoard(), turn: 0, quiet: 0, ply: 0 };
}

export function moveName({ path, jump }: CheckersMove) {
  return path.map(square => square + 1).join(jump ? "x" : "-");
}

export function parseMove(raw: string): CheckersMove | undefined {
  if (raw.length < 3 || raw.length > maxNotation) return undefined;
  const jump = raw.includes("x");
  if (jump === raw.includes("-")) return undefined;
  const parts = raw.split(jump ? "x" : "-");
  if (parts.length < 2 || parts.length > 13 || (!jump && parts.length !== 2)) return undefined;
  const path: number[] = [];
  for (const part of parts) {
    if (!/^[1-9][0-9]?$/.test(part)) return undefined;
    const square = Number(part);
    if (square > squareCount) return undefined;
    path.push(square - 1);
  }
  return { path, jump };
}

function step(square: number, direction: number, distance: 1 | 2) {
  const [dr, dc] = diagonals[direction]!;
  return squareAt(rowOf(square) + dr * distance, colOf(square) + dc * distance);
}

function directions(piece: Piece) {
  if (isKing(piece)) return [0, 1, 2, 3];
  return sideOf(piece) === 0 ? [0, 1] : [2, 3];
}

function extend(board: Piece[], piece: Piece, path: number[], out: CheckersMove[]) {
  const at = path[path.length - 1]!;
  const seat = sideOf(piece)!;
  let found = false;
  for (const direction of directions(piece)) {
    const over = step(at, direction, 1), to = step(at, direction, 2);
    if (over === undefined || to === undefined || board[to] !== Piece.Empty || sideOf(board[over]) !== 1 - seat) continue;
    found = true;
    const next = [...board];
    next[over] = Piece.Empty;
    const route = [...path, to];
    if (!isKing(piece) && rowOf(to) === crownRow(seat)) { out.push({ path: route, jump: true }); continue; }
    extend(next, piece, route, out);
  }
  if (!found && path.length > 1) out.push({ path, jump: true });
}

export function movesOf(board: Piece[], seat: Seat): CheckersMove[] {
  const jumps: CheckersMove[] = [];
  board.forEach((piece, square) => {
    if (sideOf(piece) !== seat) return;
    const rest = [...board];
    rest[square] = Piece.Empty;
    extend(rest, piece, [square], jumps);
  });
  if (jumps.length) return jumps;
  const steps: CheckersMove[] = [];
  board.forEach((piece, square) => {
    if (sideOf(piece) !== seat) return;
    for (const direction of directions(piece)) {
      const to = step(square, direction, 1);
      if (to !== undefined && board[to] === Piece.Empty) steps.push({ path: [square, to], jump: false });
    }
  });
  return steps;
}

export function legalMoves(position: CheckersPosition): string[] {
  return movesOf(position.board, position.turn).map(moveName);
}

function between(from: number, to: number) {
  const row = (rowOf(from) + rowOf(to)) / 2, col = (colOf(from) + colOf(to)) / 2;
  return squareAt(row, col);
}

export function playOn(board: Piece[], move: CheckersMove): Piece[] {
  const next = [...board];
  const from = move.path[0]!, to = move.path[move.path.length - 1]!;
  let piece = next[from]!;
  next[from] = Piece.Empty;
  if (move.jump) {
    for (let index = 1; index < move.path.length; index++) {
      const over = between(move.path[index - 1]!, move.path[index]!);
      if (over !== undefined) next[over] = Piece.Empty;
    }
  }
  const seat = sideOf(piece);
  if (seat !== undefined && !isKing(piece) && rowOf(to) === crownRow(seat)) piece = seat === 0 ? Piece.BlackKing : Piece.WhiteKing;
  next[to] = piece;
  return next;
}

export function play(position: CheckersPosition, raw: string): CheckersPosition {
  const move = movesOf(position.board, position.turn).find(candidate => moveName(candidate) === raw);
  if (!move) return position;
  const quiet = move.jump || !isKing(position.board[move.path[0]!]) ? 0 : position.quiet + 1;
  return { board: playOn(position.board, move), turn: position.turn === 0 ? 1 : 0, quiet, ply: position.ply + 1 };
}

export function outcome(position: CheckersPosition): { result: CheckersResult; winner?: Seat } | undefined {
  if (position.ply === 0) return undefined;
  if (!movesOf(position.board, position.turn).length) return { result: CheckersResult.NoMoves, winner: position.turn === 0 ? 1 : 0 };
  if (position.quiet >= quietLimit) return { result: CheckersResult.FortyMoves };
  if (position.ply >= maxPlies) return { result: CheckersResult.MoveLimit };
  return undefined;
}
