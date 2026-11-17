"use client";

import { create } from "@bufbuild/protobuf";
import { ArrowLeft, Plus, Undo2 } from "lucide-react";
import Link from "next/link";
import { useEffect, useReducer, useState } from "react";

import { ArenaHeader, ArenaPlayer, ArenaResult, actionClass, arenaClass } from "@/components/games/tictactoe/arena";
import { Board } from "@/components/games/tictactoe/board";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Mark, TicTacToeBoardSchema } from "@/gen/axon/game/v1/game_pb";
import { aiMove, localGameReducer, newGame, type LocalMode } from "@/lib/games/tictactoe";

export function PracticeGame() {
  const [game, dispatch] = useReducer(localGameReducer, undefined, () => newGame("easy", 0));
  const [dismissed, setDismissed] = useState(false);
  useEffect(() => {
    dispatch({ type: "reset", mode: "easy", now: Date.now() });
    const timer = setInterval(() => dispatch({ type: "tick", now: Date.now() }), 200);
    return () => clearInterval(timer);
  }, []);
  const { cells, turn, mode, result } = game;
  useEffect(() => {
    if (mode === "local" || result || turn !== "O") return;
    const timer = setTimeout(() => {
      const cell = aiMove(cells, mode);
      if (cell !== null) dispatch({ type: "move", cell, mark: "O", now: Date.now() });
    }, 360);
    return () => clearTimeout(timer);
  }, [cells, mode, turn, result]);
  const reset = (nextMode = mode) => {
    setDismissed(false);
    dispatch({ type: "reset", mode: nextMode, now: Date.now() });
  };
  const title = result?.winner === "draw" ? "Ничья" : mode === "local" ? `Победа ${result?.winner === "X" ? "крестиков" : "ноликов"}` : result?.winner === "X" ? "Победа" : "Поражение";
  const board = create(TicTacToeBoardSchema, { cells: cells.map(cell => cell === "X" ? Mark.X : cell === "O" ? Mark.O : Mark.UNSPECIFIED), winningCells: result?.line ?? [] });
  return <ScrollArea className="min-h-0 flex-1"><main className="workspace-page game-match-page">
    <Link href="/games/tic-tac-toe" className="game-back-link"><ArrowLeft aria-hidden size={17} />В лобби</Link>
    <div className={arenaClass}>
      <ArenaHeader><select aria-label="Режим игры" value={mode} onChange={event => reset(event.target.value as LocalMode)} className="h-[35px] max-w-[150px] min-w-0 rounded-[9px] border border-border bg-card px-2 text-xs font-semibold text-muted-foreground focus-visible:outline-2 focus-visible:outline-primary"><option value="easy">Компьютер</option><option value="hard">Сильный ИИ</option><option value="local">Игра вдвоём</option></select></ArenaHeader>
      <ArenaPlayer mark={Mark.O} name={mode === "local" ? "Игрок O" : "Компьютер"} active={!result && turn === "O"} time={game.left.O} />
      <section aria-label="Игровое поле" className="relative">
        <Board board={board} playable={!result && (mode === "local" || turn === "X")} move={cell => dispatch({ type: "move", cell, mark: turn, now: Date.now() })} />
        {result && !dismissed ? <ArenaResult title={title} kind={result.winner === "draw" ? "draw" : mode !== "local" && result.winner === "O" ? "lose" : "win"} close={() => setDismissed(true)} restart={() => reset()} /> : null}
      </section>
      <ArenaPlayer mark={Mark.X} name={mode === "local" ? "Игрок X" : "Вы"} active={!result && turn === "X"} time={game.left.X} />
      <div className="mt-3 flex flex-wrap justify-end gap-2">
        <button type="button" className={actionClass} disabled={!game.history.length} onClick={() => { setDismissed(false); dispatch({ type: "undo", now: Date.now() }); }}><Undo2 aria-hidden size={15} />Отменить</button>
        <button type="button" className={actionClass} onClick={() => reset()}><Plus aria-hidden size={15} />Новая партия</button>
      </div>
      <span role="status" data-game-status className="sr-only">{result ? title : turn === "X" ? "Ход крестиков" : "Ход ноликов"}</span>
    </div>
  </main></ScrollArea>;
}
