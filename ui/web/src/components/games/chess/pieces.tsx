import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

export enum PieceKind {
  Pawn = "p",
  Knight = "n",
  Bishop = "b",
  Rook = "r",
  Queen = "q",
  King = "k",
}

export enum Side {
  White = "w",
  Black = "b",
}

export const pieceNames: Record<PieceKind, string> = {
  [PieceKind.Pawn]: "pawn",
  [PieceKind.Knight]: "knight",
  [PieceKind.Bishop]: "bishop",
  [PieceKind.Rook]: "rook",
  [PieceKind.Queen]: "queen",
  [PieceKind.King]: "king",
};

const base = <rect x="22" y="79" width="56" height="11" rx="4" />;

const shapes: Record<PieceKind, (detail: string) => ReactNode> = {
  [PieceKind.Pawn]: () => <>
    <circle cx="50" cy="31" r="12" />
    <path d="M38 79 42 54Q50 47 58 54L62 79Z" />
    {base}
  </>,
  [PieceKind.Rook]: () => <>
    <path d="M28 19h10v8h6v-8h12v8h6v-8h10v21H28Z" />
    <path d="M34 40h32l2 39H32Z" />
    {base}
  </>,
  [PieceKind.Bishop]: detail => <>
    <circle cx="50" cy="13" r="5" />
    <path d="M50 19C66 31 66 52 58 62L62 79H38L42 62C34 52 34 31 50 19Z" />
    <path d="M55 32 46 45" stroke={detail} strokeWidth="4" strokeLinecap="round" fill="none" />
    {base}
  </>,
  [PieceKind.Knight]: detail => <>
    <path d="M36 79 40 58C30 54 25 46 29 40L46 25V15L54 23C67 25 75 39 72 56L68 79Z" />
    <circle cx="52" cy="36" r="3.5" fill={detail} stroke="none" />
    {base}
  </>,
  [PieceKind.Queen]: () => <>
    <circle cx="24" cy="29" r="5" />
    <circle cx="50" cy="20" r="5" />
    <circle cx="76" cy="29" r="5" />
    <path d="M31 79 24 35 38 53 50 26 62 53 76 35 69 79Z" />
    {base}
  </>,
  [PieceKind.King]: () => <>
    <path d="M47 11h6v8h7v6h-7v9h-6v-9h-7v-6h7Z" />
    <path d="M32 79 28 47Q50 37 72 47L68 79Z" />
    {base}
  </>,
};

export function ChessPiece({ kind, side, className }: { kind: PieceKind; side: Side; className?: string }) {
  const white = side === Side.White;
  const fill = white ? "var(--color-chess-piece-white)" : "var(--color-chess-piece-black)";
  const edge = "var(--color-chess-piece-black)";
  const detail = white ? "var(--color-chess-piece-black)" : "var(--color-chess-piece-white)";
  return (
    <svg viewBox="0 0 100 100" aria-hidden className={cn("pointer-events-none select-none", className)} fill={fill} stroke={edge} strokeWidth="4" strokeLinejoin="round">
      {shapes[kind](detail)}
    </svg>
  );
}
