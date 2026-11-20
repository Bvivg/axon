import { useRef, useState, type KeyboardEvent } from "react";

import { Mark, type TicTacToeBoard } from "@/gen/axon/game/v1/game_pb";
import { cn } from "@/lib/utils";

const rows = ["Top", "Middle", "Bottom"];
const columns = ["left", "center", "right"];

export function GameMark({ mark, className }: { mark: Mark; className?: string }) {
  if (mark === Mark.UNSPECIFIED) return null;
  return <svg viewBox="0 0 100 100" aria-hidden className={cn(mark === Mark.X ? "text-game-primary" : "text-game-text", className)} fill="none" stroke="currentColor" strokeWidth={9} strokeLinecap="round">
    {mark === Mark.X ? <><line pathLength="1" x1="24" y1="24" x2="76" y2="76" /><line pathLength="1" x1="76" y1="24" x2="24" y2="76" /></> : <circle pathLength="1" cx="50" cy="50" r="27" transform="rotate(-90 50 50)" />}
  </svg>;
}

export function Board({ board, playable, move, turn = Mark.X, lastMove }: { board?: TicTacToeBoard; playable: boolean; move: (cell: number) => void; turn?: Mark; lastMove?: number }) {
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
    <div className="relative"><div role="grid" aria-label="Tic-Tac-Toe board" className="mx-auto grid aspect-square w-full grid-rows-3 gap-2">
      {rows.map((row, rowIndex) => (
        <div key={row} role="row" className="grid min-h-0 min-w-0 grid-cols-3 gap-2">
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
                  className={cn("group relative grid h-full min-h-11 w-full place-items-center rounded-game-lg border border-game-border bg-game-card p-0 transition-[background,border-color,transform] duration-200 motion-reduce:transition-none focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-game-primary",
                    available && "cursor-pointer hover:border-game-primary/45 hover:bg-game-accent active:scale-[.97] motion-reduce:active:scale-100",
                    index === lastMove && !board?.winningCells.length && "bg-game-accent",
                    winning && "border-game-success/55 bg-game-success/11 [&_svg]:text-game-success")}>
                  <GameMark mark={mark} className="size-[46%] [&_line]:[stroke-dasharray:1] [&_circle]:[stroke-dasharray:1] motion-safe:[&_line]:animate-game-draw motion-safe:[&_line:nth-child(2)]:[animation-delay:90ms] motion-safe:[&_circle]:animate-game-draw" />
                  {available ? <GameMark mark={turn} className="absolute size-[46%] opacity-0 transition-opacity duration-200 motion-reduce:transition-none group-hover:opacity-30 group-focus-visible:opacity-30" /> : null}
                </button>
              </div>
            );
          })}
        </div>
      ))}
    </div>
    {board?.winningCells.length === 3 ? <svg aria-hidden viewBox="0 0 300 300" className="pointer-events-none absolute inset-0 size-full overflow-visible text-game-success"><line x1={(board.winningCells[0] % 3) * 100 + 50} y1={Math.floor(board.winningCells[0] / 3) * 100 + 50} x2={(board.winningCells[2] % 3) * 100 + 50} y2={Math.floor(board.winningCells[2] / 3) * 100 + 50} pathLength="1" stroke="currentColor" strokeWidth="4" strokeLinecap="round" className="[stroke-dasharray:1] motion-safe:animate-game-draw" /></svg> : null}
    </div>
  );
}
