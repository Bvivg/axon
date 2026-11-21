"use client";

import { useLayoutEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";

import { slideThrough } from "@/lib/games/motion";
import { cellAt, cellName, parseCell, parseWall, size, WallDir } from "@/lib/games/quoridor";
import { cn } from "@/lib/utils";

type Step = { from: string; to: string };

const tracks = "repeat(8, minmax(0, 1fr) 1.9%) minmax(0, 1fr)";
const last = size - 1;

export const seatNames = ["Violet", "Green"];

export function Pawn({ seat, className }: { seat: number; className?: string }) {
  return <span aria-hidden data-pawn={seat} className={cn("block rounded-full border-2 border-game-card shadow-[0_1px_3px_rgb(0_0_0/.35)]", seat === 0 ? "bg-game-primary" : "bg-game-success", className)} />;
}

export function QuoridorBoard({ pawns, walls, seat, legal, lastMove, playable, onMove }: {
  pawns: string[];
  walls: string[];
  seat: number;
  legal: string[];
  lastMove?: string;
  playable: boolean;
  onMove: (move: string) => void;
}) {
  const flipped = seat === 1;
  const [hover, setHover] = useState<string>();
  const [focused, setFocused] = useState(() => parseCell(pawns[seat] ?? "e1") ?? 4);
  const touch = useRef(false);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const allowed = new Set(playable ? legal : []);
  const pawnCells = pawns.map(name => parseCell(name));
  const [placed, setPlaced] = useState(pawns);
  const [steps, setSteps] = useState<Step[]>([]);
  const board = useRef<HTMLDivElement>(null);
  if (placed.join() !== pawns.join()) {
    setSteps(pawns.flatMap((to, at) => placed[at] && placed[at] !== to ? [{ from: placed[at]!, to }] : []));
    setPlaced(pawns);
  }

  useLayoutEffect(() => {
    const cell = (name: string) => board.current?.querySelector(`[data-cell="${name}"]`);
    for (const step of steps) slideThrough(cell(step.to)?.querySelector(":scope > [data-pawn]"), [cell(step.from), cell(step.to)]);
  }, [steps]);

  const toReal = (dr: number, dc: number) => flipped ? cellAt(dr, last - dc) : cellAt(last - dr, dc);
  const toDisplay = (cell: number) => {
    const row = Math.floor(cell / size), col = cell % size;
    return flipped ? [row, last - col] as const : [last - row, col] as const;
  };

  const anchorFor = (rows: [number, number], cols: [number, number], dir: WallDir) => {
    const cells = rows.flatMap(dr => cols.map(dc => toReal(dr, dc)));
    const row = Math.min(...cells.map(cell => Math.floor(cell / size)));
    const col = Math.min(...cells.map(cell => cell % size));
    return `${cellName(cellAt(row, col))}${dir}`;
  };

  const wallStyle = (name: string): CSSProperties | undefined => {
    const wall = parseWall(name);
    if (!wall) return undefined;
    const dr0 = flipped ? wall.row : last - 1 - wall.row;
    const dc0 = flipped ? last - 1 - wall.col : wall.col;
    return wall.dir === WallDir.Horizontal
      ? { gridRow: 2 * dr0 + 2, gridColumn: `${2 * dc0 + 1} / span 3` }
      : { gridColumn: 2 * dc0 + 2, gridRow: `${2 * dr0 + 1} / span 3` };
  };

  const placeWall = (name: string) => {
    if (!allowed.has(name)) return;
    if (touch.current && hover !== name) { setHover(name); return; }
    setHover(undefined);
    onMove(name);
  };

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, cell: number) {
    const [dr, dc] = toDisplay(cell);
    let next: [number, number] | null = null;
    switch (event.key) {
      case "ArrowUp": next = [Math.max(0, dr - 1), dc]; break;
      case "ArrowDown": next = [Math.min(last, dr + 1), dc]; break;
      case "ArrowLeft": next = [dr, Math.max(0, dc - 1)]; break;
      case "ArrowRight": next = [dr, Math.min(last, dc + 1)]; break;
      default: return;
    }
    event.preventDefault();
    const target = toReal(next[0], next[1]);
    setFocused(target);
    buttons.current[target]?.focus();
  }

  const cells = Array.from({ length: size * size }, (_, display) => {
    const dr = Math.floor(display / size), dc = display % size;
    const cell = toReal(dr, dc);
    const name = cellName(cell);
    const owner = pawnCells.indexOf(cell);
    const target = allowed.has(name);
    const label = owner === seat ? `${name}, your pawn` : owner >= 0 ? `${name}, opponent's pawn` : target ? `${name}, move here` : `${name}, empty`;
    return (
      <button key={name} ref={element => { buttons.current[cell] = element; }} type="button" data-cell={name} aria-label={label}
        tabIndex={cell === focused ? 0 : -1} onFocus={() => setFocused(cell)} onKeyDown={event => onKeyDown(event, cell)}
        onClick={() => { if (target) onMove(name); }}
        style={{ gridRow: 2 * dr + 1, gridColumn: 2 * dc + 1 }}
        className={cn("relative grid min-h-0 min-w-0 place-items-center rounded-[18%] bg-quoridor-cell p-0 outline-none focus-visible:z-20 focus-visible:ring-2 focus-visible:ring-game-primary",
          target ? "cursor-pointer" : "cursor-default", lastMove === name && "ring-2 ring-inset ring-quoridor-wall-last/60")}>
        {owner >= 0 ? <Pawn seat={owner} className="size-[64%]" /> : null}
        {target ? <span aria-hidden className="size-[30%] rounded-full bg-quoridor-target" /> : null}
        {dc === 0 ? <span aria-hidden className="absolute top-0.5 left-1 text-[9px] font-semibold text-game-text2/70">{name[1]}</span> : null}
        {dr === last ? <span aria-hidden className="absolute right-1 bottom-0 text-[9px] font-semibold text-game-text2/70">{name[0]}</span> : null}
      </button>
    );
  });

  const slots = playable ? Array.from({ length: (size - 1) * size }, (_, index) => {
    const g = Math.floor(index / size), k = index % size, k0 = Math.min(k, last - 1);
    const horizontal = anchorFor([g, g + 1], [k0, k0 + 1], WallDir.Horizontal);
    const vertical = anchorFor([k0, k0 + 1], [g, g + 1], WallDir.Vertical);
    return [
      <button key={`h${g}-${k}`} type="button" tabIndex={-1} data-wall={horizontal} aria-label={`Wall ${horizontal}`} disabled={!allowed.has(horizontal)}
        onPointerDown={event => { touch.current = event.pointerType === "touch"; }}
        onPointerEnter={() => setHover(horizontal)} onPointerLeave={() => setHover(value => value === horizontal ? undefined : value)} onClick={() => placeWall(horizontal)}
        style={{ gridRow: 2 * g + 2, gridColumn: 2 * k + 1 }} className="z-10 -my-[7px] cursor-pointer disabled:cursor-not-allowed" />,
      <button key={`v${g}-${k}`} type="button" tabIndex={-1} data-wall={vertical} aria-label={`Wall ${vertical}`} disabled={!allowed.has(vertical)}
        onPointerDown={event => { touch.current = event.pointerType === "touch"; }}
        onPointerEnter={() => setHover(vertical)} onPointerLeave={() => setHover(value => value === vertical ? undefined : value)} onClick={() => placeWall(vertical)}
        style={{ gridColumn: 2 * g + 2, gridRow: 2 * k + 1 }} className="z-10 -mx-[7px] cursor-pointer disabled:cursor-not-allowed" />,
    ];
  }).flat() : [];

  return (
    <div ref={board} role="group" aria-label="Quoridor board" onPointerLeave={() => setHover(undefined)}
      className="grid aspect-square w-full touch-manipulation select-none rounded-game-md border border-game-border bg-quoridor-groove p-[1.6%]"
      style={{ gridTemplateColumns: tracks, gridTemplateRows: tracks }}>
      {cells}
      {walls.map(name => (
        <span key={name} aria-hidden data-placed-wall={name} style={wallStyle(name)}
          className={cn("pointer-events-none z-[5] rounded-full", name === lastMove ? "bg-quoridor-wall-last motion-safe:animate-game-pop" : "bg-quoridor-wall")} />
      ))}
      {hover && allowed.has(hover) ? <span aria-hidden style={wallStyle(hover)} className="pointer-events-none z-[5] rounded-full bg-quoridor-wall/55" /> : null}
      {slots}
    </div>
  );
}
