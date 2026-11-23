"use client";

import { useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";

import { ChessPiece, PieceKind, pieceNames, Side } from "@/components/games/chess/pieces";
import { placement, sideToMove, squareIndex, squareName } from "@/lib/games/chess-fen";
import { slideThrough } from "@/lib/games/motion";
import { cn } from "@/lib/utils";

type Drag = { from: number; x: number; y: number; moved: boolean; wasSelected: boolean };
type Promotion = { from: number; to: number };
type Placed = ReturnType<typeof placement>[number];
type Motion = { move: string; fen: string; ghosts: Map<number, NonNullable<Placed>> };

function castlingRook(move: string, piece: Placed): [string, string] | null {
  if (piece?.kind !== PieceKind.King) return null;
  const from = move.charCodeAt(0), to = move.charCodeAt(2), rank = move[3];
  if (Math.abs(from - to) !== 2) return null;
  return to > from ? [`h${rank}`, `f${rank}`] : [`a${rank}`, `d${rank}`];
}

function captured(before: Placed[], after: Placed[], mover: Side | undefined): Map<number, NonNullable<Placed>> {
  const ghosts = new Map<number, NonNullable<Placed>>();
  before.forEach((piece, square) => {
    const now = after[square];
    if (piece && mover !== undefined && piece.side !== mover && (!now || now.side === mover)) ghosts.set(square, piece);
  });
  return ghosts;
}

const promotionKinds = [PieceKind.Queen, PieceKind.Rook, PieceKind.Bishop, PieceKind.Knight];

export function ChessBoard({ fen, orientation, legal, lastMove, check, playable, onMove }: {
  fen: string;
  orientation: Side;
  legal: string[];
  lastMove?: string;
  check: boolean;
  playable: boolean;
  onMove: (uci: string) => void;
}) {
  const squares = useMemo(() => placement(fen), [fen]);
  const turn = sideToMove(fen);
  const [selected, setSelected] = useState<number | null>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [promotion, setPromotion] = useState<Promotion | null>(null);
  const [focused, setFocused] = useState(() => orientation === Side.White ? squareIndex("e2") : squareIndex("e7"));
  const [position, setPosition] = useState(fen);
  const [motion, setMotion] = useState<Motion | null>(null);
  const board = useRef<HTMLDivElement>(null);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const dropped = useRef<string | null>(null);
  if (position !== fen) {
    const mover = lastMove ? squares[squareIndex(lastMove.slice(2, 4))]?.side : undefined;
    setMotion(lastMove ? { move: lastMove, fen, ghosts: captured(placement(position), squares, mover) } : null);
    setPosition(fen);
    setSelected(null);
    setPromotion(null);
    setDrag(null);
  }

  useLayoutEffect(() => {
    if (!motion) return;
    const move = motion.move.slice(0, 4);
    const skip = dropped.current === move;
    dropped.current = null;
    if (skip) return;
    const square = (name: string) => board.current?.querySelector(`[data-square="${name}"]`);
    const piece = (name: string) => square(name)?.querySelector(":scope > svg[data-live]");
    const from = move.slice(0, 2), to = move.slice(2, 4);
    slideThrough(piece(to), [square(from), square(to)]);
    const rook = castlingRook(move, placement(motion.fen)[squareIndex(to)]);
    if (rook) slideThrough(piece(rook[1]), [square(rook[0]), square(rook[1])]);
  }, [motion]);

  const order = useMemo(() => {
    const ranks = orientation === Side.White ? [7, 6, 5, 4, 3, 2, 1, 0] : [0, 1, 2, 3, 4, 5, 6, 7];
    const files = orientation === Side.White ? [0, 1, 2, 3, 4, 5, 6, 7] : [7, 6, 5, 4, 3, 2, 1, 0];
    return ranks.map(rank => files.map(file => rank * 8 + file));
  }, [orientation]);

  const movesFrom = (square: number) => playable ? legal.filter(move => move.startsWith(squareName(square))) : [];
  const targets = new Set(selected === null ? [] : movesFrom(selected).map(move => squareIndex(move.slice(2, 4))));
  const kingInCheck = check ? squares.findIndex(piece => piece?.kind === PieceKind.King && piece.side === turn) : -1;
  const last = lastMove ? [squareIndex(lastMove.slice(0, 2)), squareIndex(lastMove.slice(2, 4))] : [];

  function attempt(from: number, to: number) {
    const candidates = movesFrom(from).filter(move => move.slice(2, 4) === squareName(to));
    setSelected(null);
    if (candidates.length > 1) setPromotion({ from, to });
    else if (candidates[0]) onMove(candidates[0]);
  }

  function activate(square: number) {
    if (promotion) return;
    if (selected !== null && targets.has(square)) { attempt(selected, square); return; }
    if (selected === square) { setSelected(null); return; }
    setSelected(movesFrom(square).length ? square : null);
  }

  function squareAt(clientX: number, clientY: number) {
    const rect = board.current?.getBoundingClientRect();
    if (!rect) return null;
    const column = Math.floor(((clientX - rect.left) / rect.width) * 8);
    const row = Math.floor(((clientY - rect.top) / rect.height) * 8);
    if (column < 0 || column > 7 || row < 0 || row > 7) return null;
    return order[row]?.[column] ?? null;
  }

  function onPointerDown(event: PointerEvent<HTMLButtonElement>, square: number) {
    if (event.button !== 0 || promotion) return;
    if (selected !== null && targets.has(square)) { attempt(selected, square); return; }
    if (!movesFrom(square).length) { setSelected(null); return; }
    const rect = board.current?.getBoundingClientRect();
    board.current?.setPointerCapture(event.pointerId);
    setDrag({ from: square, x: event.clientX - (rect?.left ?? 0), y: event.clientY - (rect?.top ?? 0), moved: false, wasSelected: selected === square });
    setSelected(square);
  }

  function onPointerMove(event: PointerEvent<HTMLDivElement>) {
    if (!drag) return;
    const rect = board.current?.getBoundingClientRect();
    const x = event.clientX - (rect?.left ?? 0), y = event.clientY - (rect?.top ?? 0);
    const moved = drag.moved || Math.hypot(x - drag.x, y - drag.y) > 4;
    setDrag({ ...drag, x, y, moved });
  }

  function onPointerUp(event: PointerEvent<HTMLDivElement>) {
    if (!drag) return;
    const target = squareAt(event.clientX, event.clientY);
    if (drag.moved && target !== null && target !== drag.from && movesFrom(drag.from).some(move => move.slice(2, 4) === squareName(target))) {
      dropped.current = `${squareName(drag.from)}${squareName(target)}`;
      attempt(drag.from, target);
    }
    else if (!drag.moved && drag.wasSelected) setSelected(null);
    setDrag(null);
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, row: number, column: number) {
    let next: [number, number] | null = null;
    switch (event.key) {
      case "ArrowUp": next = [Math.max(0, row - 1), column]; break;
      case "ArrowDown": next = [Math.min(7, row + 1), column]; break;
      case "ArrowLeft": next = [row, Math.max(0, column - 1)]; break;
      case "ArrowRight": next = [row, Math.min(7, column + 1)]; break;
      case "Enter": case " ": event.preventDefault(); activate(order[row]![column]!); return;
      case "Escape": setSelected(null); setPromotion(null); return;
      default: return;
    }
    event.preventDefault();
    const square = order[next[0]]![next[1]]!;
    setFocused(square);
    buttons.current[square]?.focus();
  }

  const dragged = drag?.moved ? squares[drag.from] : null;

  return (
    <div className="relative w-full select-none">
      <div ref={board} role="grid" aria-label="Chess board" onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={() => setDrag(null)}
        className="grid aspect-square w-full touch-none grid-rows-8 overflow-hidden rounded-game-md border border-game-border">
        {order.map((row, rowIndex) => (
          <div key={rowIndex} role="row" className="grid min-h-0 grid-cols-8">
            {row.map((square, columnIndex) => {
              const piece = squares[square];
              const dark = (Math.floor(square / 8) + (square % 8)) % 2 === 0;
              const name = squareName(square);
              const label = piece ? `${name}, ${piece.side === Side.White ? "white" : "black"} ${pieceNames[piece.kind]}` : `${name}, empty`;
              const target = targets.has(square);
              const ghost = motion?.ghosts.get(square);
              return (
                <div key={square} role="gridcell" className="min-h-0 min-w-0">
                  <button ref={element => { buttons.current[square] = element; }} type="button" data-square={name}
                    tabIndex={square === focused ? 0 : -1} aria-label={label} aria-pressed={selected === square}
                    onFocus={() => setFocused(square)} onKeyDown={event => onKeyDown(event, rowIndex, columnIndex)}
                    onPointerDown={event => onPointerDown(event, square)}
                    className={cn("relative grid size-full place-items-center p-0 outline-none focus-visible:z-10 focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-game-primary",
                      dark ? "bg-chess-dark" : "bg-chess-light",
                      movesFrom(square).length && "cursor-grab")}>
                    {last.includes(square) ? <span aria-hidden className="absolute inset-0 bg-chess-last" /> : null}
                    {selected === square ? <span aria-hidden className="absolute inset-0 bg-chess-mark" /> : null}
                    {square === kingInCheck ? <span aria-hidden className="absolute inset-0 bg-[radial-gradient(circle,var(--color-chess-check)_35%,transparent_72%)]" /> : null}
                    {ghost ? <ChessPiece key={motion?.fen} kind={ghost.kind} side={ghost.side} className="absolute size-[86%] motion-safe:animate-game-vanish motion-reduce:hidden" /> : null}
                    {piece ? <ChessPiece live kind={piece.kind} side={piece.side} className={cn("relative size-[86%]", drag?.moved && drag.from === square && "opacity-30")} /> : null}
                    {target ? <span aria-hidden className={cn("absolute rounded-full", piece ? "inset-[6%] border-[5px] border-chess-mark" : "size-[28%] bg-chess-mark")} /> : null}
                    {columnIndex === 0 ? <span aria-hidden className={cn("absolute top-0.5 left-1 text-[10px] font-semibold", dark ? "text-chess-light" : "text-chess-dark")}>{name[1]}</span> : null}
                    {rowIndex === 7 ? <span aria-hidden className={cn("absolute right-1 bottom-0 text-[10px] font-semibold", dark ? "text-chess-light" : "text-chess-dark")}>{name[0]}</span> : null}
                  </button>
                </div>
              );
            })}
          </div>
        ))}
      </div>
      {dragged && drag ? <span aria-hidden className="pointer-events-none absolute z-20 aspect-square w-[12.5%] -translate-x-1/2 -translate-y-1/2 drop-shadow-lg" style={{ left: drag.x, top: drag.y }}><ChessPiece kind={dragged.kind} side={dragged.side} className="size-full" /></span> : null}
      {promotion ? (
        <div role="dialog" aria-label="Choose a piece" className="absolute inset-0 z-30 grid place-items-center bg-game-scrim">
          <div className="flex gap-2 rounded-game-lg border border-game-border bg-game-card p-2 shadow-game-dialog">
            {promotionKinds.map(kind => (
              <button key={kind} type="button" aria-label={`Promote to ${pieceNames[kind]}`} onClick={() => { onMove(`${squareName(promotion.from)}${squareName(promotion.to)}${kind}`); setPromotion(null); }}
                className="grid size-14 place-items-center rounded-game-sm hover:bg-game-accent focus-visible:outline-2 focus-visible:outline-game-primary">
                <ChessPiece kind={kind} side={turn} className="size-11" />
              </button>
            ))}
            <button type="button" aria-label="Cancel promotion" onClick={() => setPromotion(null)} className="grid size-14 place-items-center rounded-game-sm text-game-text2 hover:bg-game-accent">✕</button>
          </div>
        </div>
      ) : null}
    </div>
  );
}
