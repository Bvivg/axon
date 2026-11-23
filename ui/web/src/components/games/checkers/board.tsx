"use client";

import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from "react";

import { boardSize, isKing, parseMove, Piece, sideOf, squareAt } from "@/lib/games/checkers";
import { slideThrough } from "@/lib/games/motion";
import { cn } from "@/lib/utils";

type Motion = { path: number[]; ghosts: Map<number, Piece>; id: string };

function capturedPieces(before: Piece[], after: Piece[], path: number[]): Map<number, Piece> {
  const mover = after[path.at(-1) ?? -1];
  const ghosts = new Map<number, Piece>();
  if (mover === undefined || mover === Piece.Empty) return ghosts;
  before.forEach((piece, square) => {
    if (piece !== Piece.Empty && sideOf(piece) !== sideOf(mover) && after[square] === Piece.Empty) ghosts.set(square, piece);
  });
  return ghosts;
}

export const seatNames = ["Black", "White"];

const pieceNames: Record<Piece, string> = {
  [Piece.Empty]: "empty",
  [Piece.BlackMan]: "black man",
  [Piece.BlackKing]: "black king",
  [Piece.WhiteMan]: "white man",
  [Piece.WhiteKing]: "white king",
};

const last = boardSize - 1;

export function CheckersPiece({ piece, className, ghost }: { piece: Piece; className?: string; ghost?: boolean }) {
  if (piece === Piece.Empty) return null;
  const black = sideOf(piece) === 0;
  const fill = black ? "var(--color-chess-piece-black)" : "var(--color-chess-piece-white)";
  const detail = black ? "var(--color-chess-piece-white)" : "var(--color-chess-piece-black)";
  return (
    <svg viewBox="0 0 100 100" aria-hidden data-piece={ghost ? undefined : piece} className={cn("pointer-events-none select-none drop-shadow-[0_2px_2px_rgb(0_0_0/.3)]", className)}>
      <circle cx="50" cy="50" r="44" fill={fill} stroke="var(--color-chess-piece-black)" strokeWidth="4" />
      <circle cx="50" cy="50" r="31" fill="none" stroke={detail} strokeOpacity=".35" strokeWidth="3" />
      {isKing(piece) ? <path d="M30 62h40l4-26-12 10-12-16-12 16-12-10Z" fill={detail} stroke={detail} strokeWidth="3" strokeLinejoin="round" /> : null}
    </svg>
  );
}

export function PiecesLeft({ count }: { count: number }) {
  return <span data-pieces-left={count} className="font-mono text-[12px] text-game-text2">{count} {count === 1 ? "piece" : "pieces"}</span>;
}

type Candidate = { name: string; path: number[] };

export function CheckersBoard({ squares, seat, legal, lastMove, playable, onMove }: {
  squares: Piece[];
  seat: number;
  legal: string[];
  lastMove?: string;
  playable: boolean;
  onMove: (move: string) => void;
}) {
  const flipped = seat === 0;
  const [path, setPath] = useState<number[]>([]);
  const [focused, setFocused] = useState(() => flipped ? 9 : 22);
  const [position, setPosition] = useState(() => `${squares.join("")}|${legal.join(" ")}`);
  const [placed, setPlaced] = useState(squares);
  const [motion, setMotion] = useState<Motion | null>(null);
  const board = useRef<HTMLDivElement>(null);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const signature = `${squares.join("")}|${legal.join(" ")}`;
  if (position !== signature) {
    setPosition(signature);
    setPath([]);
  }
  if (placed.join("") !== squares.join("")) {
    const moved = parseMove(lastMove ?? "")?.path;
    setMotion(moved && moved.length > 1 ? { path: moved, ghosts: capturedPieces(placed, squares, moved), id: signature } : null);
    setPlaced(squares);
  }

  useLayoutEffect(() => {
    if (!motion) return;
    const square = (index: number) => board.current?.querySelector(`[data-square="${index + 1}"]`);
    const end = motion.path.at(-1)!;
    slideThrough(square(end)?.querySelector(":scope > svg[data-piece]"), motion.path.map(square));
  }, [motion]);

  const candidates = useMemo<Candidate[]>(() => (playable ? legal : []).flatMap(name => {
    const move = parseMove(name);
    return move ? [{ name, path: move.path }] : [];
  }), [legal, playable]);
  const matching = candidates.filter(candidate => path.every((square, index) => candidate.path[index] === square));
  const origins = new Set(candidates.map(candidate => candidate.path[0]));
  const targets = new Set(path.length ? matching.flatMap(candidate => [candidate.path[path.length], candidate.path.at(-1)]).filter(square => square !== undefined) : []);
  const trail = new Set(parseMove(lastMove ?? "")?.path ?? []);

  const toReal = (row: number, col: number) => flipped ? [last - row, last - col] as const : [row, col] as const;

  function activate(square: number) {
    if (!playable) return;
    if (path.length === 1 && path[0] === square) { setPath([]); return; }
    if (path.length) {
      const next = matching.filter(candidate => candidate.path[path.length] === square);
      const exact = next.filter(candidate => candidate.path.length === path.length + 1);
      const longer = next.filter(candidate => candidate.path.length > path.length + 1);
      if (exact.length === 1 && !longer.length) { setPath([]); onMove(exact[0]!.name); return; }
      if (longer.length) { setPath([...path, square]); return; }
      const ending = matching.filter(candidate => candidate.path.at(-1) === square);
      if (ending.length === 1) { setPath([]); onMove(ending[0]!.name); return; }
    }
    setPath(origins.has(square) ? [square] : []);
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, row: number, col: number, square: number) {
    let next: [number, number] | null = null;
    switch (event.key) {
      case "ArrowLeft": next = [row, col - 2]; break;
      case "ArrowRight": next = [row, col + 2]; break;
      case "ArrowUp": next = [row - 1, col + 1 <= last ? col + 1 : col - 1]; break;
      case "ArrowDown": next = [row + 1, col - 1 >= 0 ? col - 1 : col + 1]; break;
      case "Enter": case " ": event.preventDefault(); activate(square); return;
      case "Escape": setPath([]); return;
      default: return;
    }
    event.preventDefault();
    const [r, c] = toReal(next[0], next[1]);
    const target = squareAt(r, c);
    if (target === undefined) return;
    setFocused(target);
    buttons.current[target]?.focus();
  }

  const cells = Array.from({ length: boardSize * boardSize }, (_, display) => {
    const row = Math.floor(display / boardSize), col = display % boardSize;
    const [r, c] = toReal(row, col);
    const square = squareAt(r, c);
    if (square === undefined) return <span key={display} aria-hidden className="bg-chess-light" />;
    const piece = squares[square] ?? Piece.Empty;
    const number = square + 1;
    const selected = path.includes(square);
    const target = targets.has(square) && !(path.length === 1 && path[0] === square);
    const movable = origins.has(square);
    const label = [`${number}, ${pieceNames[piece]}`, selected ? "selected" : movable && !path.length ? "can move" : "", target ? "move here" : ""].filter(Boolean).join(", ");
    return (
      <button key={display} ref={element => { buttons.current[square] = element; }} type="button" data-square={number}
        data-target={target || undefined} data-movable={movable || undefined} aria-label={label} aria-pressed={path[0] === square}
        tabIndex={square === focused ? 0 : -1} onFocus={() => setFocused(square)} onKeyDown={event => onKeyDown(event, row, col, square)} onClick={() => activate(square)}
        className={cn("relative grid size-full min-h-0 min-w-0 place-items-center bg-chess-dark p-0 outline-none focus-visible:z-10 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-game-primary",
          movable || target ? "cursor-pointer" : "cursor-default")}>
        {trail.has(square) ? <span aria-hidden className="absolute inset-0 bg-chess-last" /> : null}
        {selected ? <span aria-hidden className="absolute inset-0 bg-chess-mark" /> : null}
        {movable && !path.length ? <span aria-hidden className="absolute inset-[7%] rounded-full ring-2 ring-game-primary/70" /> : null}
        {motion?.ghosts.has(square) ? <CheckersPiece key={motion.id} ghost piece={motion.ghosts.get(square)!} className="absolute size-[80%] motion-safe:animate-game-vanish motion-reduce:hidden" /> : null}
        <CheckersPiece piece={piece} className="relative size-[80%]" />
        {target ? <span aria-hidden className="absolute size-[28%] rounded-full bg-chess-mark" /> : null}
        <span aria-hidden className="absolute top-0.5 left-1 text-[9px] font-semibold text-chess-light/90">{number}</span>
      </button>
    );
  });

  return (
    <div ref={board} role="group" aria-label="Checkers board"
      className="grid aspect-square w-full touch-manipulation grid-cols-8 grid-rows-8 overflow-hidden rounded-game-md border border-game-border select-none">
      {cells}
    </div>
  );
}
