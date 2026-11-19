import { PieceKind, Side } from "@/components/games/chess/pieces";

export type BoardPiece = { kind: PieceKind; side: Side };

const kinds = new Set<string>(Object.values(PieceKind));

export function placement(fen: string): (BoardPiece | null)[] {
  const squares: (BoardPiece | null)[] = Array.from({ length: 64 }, () => null);
  const rows = (fen.split(" ")[0] ?? "").split("/");
  rows.forEach((row, index) => {
    const rank = 7 - index;
    let file = 0;
    for (const char of row) {
      if (/\d/.test(char)) { file += Number(char); continue; }
      const lower = char.toLowerCase();
      if (kinds.has(lower) && file < 8 && rank >= 0) squares[rank * 8 + file] = { kind: lower as PieceKind, side: char === lower ? Side.Black : Side.White };
      file++;
    }
  });
  return squares;
}

export function sideToMove(fen: string): Side {
  return fen.split(" ")[1] === "b" ? Side.Black : Side.White;
}

export const squareName = (square: number) => `${"abcdefgh"[square % 8]}${Math.floor(square / 8) + 1}`;

export function squareIndex(name: string): number {
  return (name.charCodeAt(1) - 49) * 8 + (name.charCodeAt(0) - 97);
}
