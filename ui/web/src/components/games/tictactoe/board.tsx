import { useRef, useState, type KeyboardEvent } from "react";

import { Mark, type TicTacToeBoard } from "@/gen/axon/game/v1/game_pb";
import { cn } from "@/lib/utils";

const rows = ["Top", "Middle", "Bottom"];
const columns = ["left", "center", "right"];

export function GameMark({ mark, className }: { mark: Mark; className?: string }) {
  if (mark === Mark.UNSPECIFIED) return null;
  return <svg viewBox="0 0 100 100" aria-hidden className={cn(mark === Mark.X ? "text-signal" : "text-[color:light-dark(#50688b,#9db8dc)]", className)} fill="none" stroke="currentColor" strokeWidth={6} strokeLinecap="round">
    {mark === Mark.X ? <><line x1="24" y1="24" x2="76" y2="76" /><line x1="76" y1="24" x2="24" y2="76" /></> : <circle cx="50" cy="50" r="30" />}
  </svg>;
}

export function Board({ board, playable, move }: { board?: TicTacToeBoard; playable: boolean; move: (cell: number) => void }) {
  const [focused, setFocused] = useState(0);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>, row: number, column: number) {
    let next = row * 3 + column;
    switch (event.key) {
      case "ArrowUp": next = Math.max(0, row - 1) * 3 + column; break;
      case "ArrowDown": next = Math.min(2, row + 1) * 3 + column; break;
      case "ArrowLeft": next = row * 3 + Math.max(0, column - 1); break;
      case "ArrowRight": next = row * 3 + Math.min(2, column + 1); break;
      case "Home": next = event.ctrlKey ? 0 : row * 3; break;
      case "End": next = event.ctrlKey ? 8 : row * 3 + 2; break;
      default: return;
    }
    event.preventDefault();
    buttons.current[next]?.focus();
  }

  return (
    <div role="grid" aria-label="Tic-Tac-Toe board" className="mx-auto grid aspect-square w-full grid-rows-3 gap-px overflow-hidden rounded-[9px] border border-border bg-border shadow-lg shadow-black/5">
      {rows.map((row, rowIndex) => (
        <div key={row} role="row" className="grid min-h-0 min-w-0 grid-cols-3 gap-px">
          {columns.map((column, columnIndex) => {
            const index = rowIndex * 3 + columnIndex;
            const mark = board?.cells[index] ?? Mark.UNSPECIFIED;
            const label = mark === Mark.X ? "X" : mark === Mark.O ? "O" : "Empty";
            const winning = board?.winningCells.includes(index);
            const available = playable && mark === Mark.UNSPECIFIED;
            return (
              <div key={column} role="gridcell" className="min-h-0 min-w-0">
                <button ref={(element) => { buttons.current[index] = element; }} type="button" tabIndex={index === focused ? 0 : -1}
                  aria-disabled={!available} aria-label={`${row} ${column}, ${label}`} data-winning={winning || undefined}
                  onFocus={() => setFocused(index)} onKeyDown={(event) => onKeyDown(event, rowIndex, columnIndex)}
                  onClick={() => { if (available) move(index); }}
                  className={cn("group grid h-full min-h-11 w-full place-items-center bg-card transition-colors focus-visible:outline-2 focus-visible:-outline-offset-3 focus-visible:outline-primary",
                    index % 2 === 1 && "bg-[color-mix(in_srgb,var(--color-card)_93%,var(--color-muted))]",
                    available && "cursor-pointer hover:bg-primary/15 active:bg-muted",
                    winning && "bg-success/15 [&_svg]:text-success")}>
                  <GameMark mark={mark} className="size-[57%] motion-safe:animate-fade" />
                </button>
              </div>
            );
          })}
        </div>
      ))}
    </div>
  );
}
