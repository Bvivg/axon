"use client";

import { create } from "@bufbuild/protobuf";
import { useEffect, useReducer, useState, useSyncExternalStore } from "react";

import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools, SettingsIcon, arenaClass } from "@/components/games/tictactoe/arena";
import { Board } from "@/components/games/tictactoe/board";
import { difficultyLabels, GameSettings, GameStatistics, readPreferences, StatisticsIcon, storePreferences, type GamePreferences } from "@/components/games/tictactoe/preferences";
import { useArenaShortcuts } from "@/components/games/tictactoe/shortcuts";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Mark, TicTacToeBoardSchema } from "@/gen/axon/game/v1/game_pb";
import { aiMove, localGameReducer, newGame } from "@/lib/games/tictactoe";
import { ticTacToeProfiles as profiles } from "@/lib/games/tictactoe-profiles";

const options = (settings: GamePreferences) => ({ duration: settings.duration, human: settings.side });

const subscribeHydration = () => () => {};
export function PracticeGame() {
  const hydrated = useSyncExternalStore(subscribeHydration, () => true, () => false);
  return hydrated ? <PracticeArena /> : <span role="status" className="sr-only">Загрузка игры</span>;
}

function PracticeArena() {
  const [settings, setSettings] = useState(readPreferences);
  const [game, dispatch] = useReducer(localGameReducer, undefined, () => newGame(settings.mode, Date.now(), options(settings)));
  const [modal, setModal] = useState<"settings" | "stats" | null>(null);
  const [hidden, setHidden] = useState(() => document.hidden);
  const [dismissed, setDismissed] = useState(false);
  const [gameKey, setGameKey] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => dispatch({ type: "tick", now: Date.now() }), 100);
    const visibility = () => { setHidden(document.hidden); dispatch({ type: "pause", paused: document.hidden || modal !== null, now: Date.now() }); };
    document.addEventListener("visibilitychange", visibility);
    return () => { clearInterval(timer); document.removeEventListener("visibilitychange", visibility); };
  }, [modal]);
  useEffect(() => { dispatch({ type: "pause", paused: modal !== null || hidden, now: Date.now() }); }, [modal, hidden]);
  const { cells, turn, mode, result } = game;
  const human = settings.side;
  const paused = modal !== null || hidden;
  useEffect(() => {
    if (mode === "local" || result || turn === human || paused) return;
    const timer = setTimeout(() => {
      const cell = aiMove(cells, mode, Math.random, turn);
      if (cell !== null) dispatch({ type: "move", cell, mark: turn, now: Date.now() });
    }, 360);
    return () => clearTimeout(timer);
  }, [cells, mode, turn, result, human, paused]);
  const reset = (next = settings) => {
    setDismissed(false);
    setGameKey(value => Math.max(Date.now(), value + 1));
    dispatch({ type: "reset", mode: next.mode, now: Date.now(), options: options(next) });
    dispatch({ type: "pause", paused, now: Date.now() });
  };
  const canUndo = !result && game.history.some(snapshot => mode === "local" || snapshot.turn === human);
  const undo = () => { if (canUndo) dispatch({ type: "undo", now: Date.now() }); };
  const playable = !result && !paused && (mode === "local" || turn === human);
  const move = (cell: number) => { if (playable) dispatch({ type: "move", cell, mark: turn, now: Date.now() }); };
  useArenaShortcuts({ enabled: !paused, move, undo, restart: () => reset() });
  const title = result?.winner === "draw" ? "Ничья" : mode === "local" ? `Победа ${result?.winner === "X" ? "крестиков" : "ноликов"}` : result?.winner === human ? "Победа" : "Поражение";
  const board = create(TicTacToeBoardSchema, { cells: cells.map(cell => cell === "X" ? Mark.X : cell === "O" ? Mark.O : Mark.UNSPECIFIED), winningCells: result?.line ?? [] });
  const bottom = mode === "local" ? "X" : human;
  const top = bottom === "X" ? "O" : "X";
  return <ScrollArea className="min-h-0 flex-1 bg-game-workspace" viewportProps={{ className: "[&>div]:min-h-full" }}><main lang="ru" className="flex min-h-full w-full flex-col px-[max(16px,env(safe-area-inset-left))] pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))] text-game-text">
    <div className={arenaClass}>
      <ArenaHeader><ArenaTools restart={() => reset()} undo={undo} canUndo={canUndo}><StatisticsIcon onClick={() => setModal("stats")} /><SettingsIcon onClick={() => setModal("settings")} /></ArenaTools></ArenaHeader>
      <ArenaPlayer mark={top === "X" ? Mark.X : Mark.O} name={mode === "local" ? profiles.opponent.nick : profiles.bot.nick} rating={mode === "local" ? profiles.opponent.rating : undefined} difficulty={mode === "local" ? undefined : difficultyLabels[mode]} avatar={mode === "local" ? profiles.opponent.avatar : profiles.bot.avatar} bot={mode !== "local"} active={!result && turn === top} time={game.left[top]} />
      <section aria-label="Игровое поле" className="relative"><Board board={board} lastMove={game.history.length ? cells.findIndex((cell, index) => cell !== game.history.at(-1)?.cells[index]) : undefined} turn={turn === "X" ? Mark.X : Mark.O} playable={playable} move={move} /></section>
      <ArenaPlayer mark={bottom === "X" ? Mark.X : Mark.O} name={profiles.me.nick} rating={mode === "local" ? profiles.me.rating : undefined} avatar={profiles.me.avatar} active={!result && turn === bottom} time={game.left[bottom]} />
      <span role="status" aria-live="polite" aria-atomic="true" data-game-status className="sr-only">{result ? title : turn === "X" ? "Ход крестиков" : "Ход ноликов"}</span>
    </div>
    {result && !dismissed ? <ArenaResult title={title} kind={result.winner === "draw" ? "draw" : mode !== "local" && result.winner !== human ? "lose" : "win"} close={() => setDismissed(true)} restart={() => reset()} /> : null}
    <GameSettings open={modal === "settings"} onOpenChange={open => setModal(open ? "settings" : null)} preferences={settings} change={next => { setSettings(next); storePreferences(next); reset(next); }} />
    <GameStatistics open={modal === "stats"} onOpenChange={open => setModal(open ? "stats" : null)} winner={result?.winner} gameKey={gameKey} />
  </main></ScrollArea>;
}
