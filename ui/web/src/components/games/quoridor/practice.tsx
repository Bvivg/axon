"use client";

import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import { MoveList } from "@/components/games/chess/moves";
import { Pawn, QuoridorBoard, seatNames } from "@/components/games/quoridor/board";
import { WallsLeft } from "@/components/games/quoridor/walls-left";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools } from "@/components/games/tictactoe/arena";
import { ScrollArea } from "@/components/ui/scroll-area";
import { GameKey, lobbyHref } from "@/lib/games/catalog";
import { cellName, legalMoves, play, startPosition, winner, type QuoridorPosition } from "@/lib/games/quoridor";
import { QuoridorBotLevel } from "@/lib/games/quoridor-bot";
import type { QuoridorBotReply, QuoridorBotRequest } from "@/lib/games/quoridor-bot.worker";
import { cn } from "@/lib/utils";

enum Opponent {
  Easy = "easy",
  Medium = "medium",
  Hard = "hard",
  Local = "local",
}

const opponents: { value: Opponent; label: string }[] = [
  { value: Opponent.Easy, label: "Easy" },
  { value: Opponent.Medium, label: "Medium" },
  { value: Opponent.Hard, label: "Hard" },
  { value: Opponent.Local, label: "Two players" },
];

const levels: Record<Exclude<Opponent, Opponent.Local>, QuoridorBotLevel> = {
  [Opponent.Easy]: QuoridorBotLevel.Easy,
  [Opponent.Medium]: QuoridorBotLevel.Medium,
  [Opponent.Hard]: QuoridorBotLevel.Hard,
};

function replay(moves: string[]) {
  return moves.reduce<QuoridorPosition>((position, move) => play(position, move), startPosition());
}

const subscribeHydration = () => () => {};

export function QuoridorPractice() {
  const hydrated = useSyncExternalStore(subscribeHydration, () => true, () => false);
  return (
    <ScrollArea className="min-h-0 flex-1 bg-game-workspace" viewportProps={{ className: "[&>div]:min-h-full" }}>
      <main className="flex min-h-full w-full flex-col px-[max(16px,env(safe-area-inset-left))] pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
        {hydrated ? <Practice /> : <span role="status" className="sr-only">Loading game</span>}
      </main>
    </ScrollArea>
  );
}

function Practice() {
  const [opponent, setOpponent] = useState(Opponent.Medium);
  const [seat, setSeat] = useState<0 | 1>(0);
  const [moves, setMoves] = useState<string[]>([]);
  const [thinking, setThinking] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const worker = useRef<Worker | null>(null);
  const request = useRef(0);
  const position = useMemo(() => replay(moves), [moves]);
  const local = opponent === Opponent.Local;
  const won = winner(position);
  const over = won !== undefined;
  const botTurn = !local && position.turn !== seat && !over;
  const legal = useMemo(() => over || botTurn ? [] : legalMoves(position), [over, botTurn, position]);
  const pawns = position.pawns.map(cellName);
  const walls = [...position.walls];

  useEffect(() => {
    const instance = new Worker(new URL("../../../lib/games/quoridor-bot.worker.ts", import.meta.url), { type: "module" });
    instance.onmessage = (event: MessageEvent<QuoridorBotReply>) => {
      if (event.data.id !== request.current) return;
      setThinking(false);
      const move = event.data.move;
      if (move) setMoves(previous => [...previous, move]);
    };
    worker.current = instance;
    return () => instance.terminate();
  }, []);

  useEffect(() => {
    if (!botTurn || local) return;
    const id = ++request.current;
    const timer = setTimeout(() => {
      setThinking(true);
      worker.current?.postMessage({ id, pawns: position.pawns.map(cellName), walls: [...position.walls], left: [...position.left], turn: position.turn, level: levels[opponent as Exclude<Opponent, Opponent.Local>] } satisfies QuoridorBotRequest);
    }, 250);
    return () => clearTimeout(timer);
  }, [botTurn, local, position, opponent]);

  const restart = (next?: { opponent?: Opponent; seat?: 0 | 1 }) => {
    request.current++;
    setThinking(false);
    setDismissed(false);
    if (next?.opponent) setOpponent(next.opponent);
    if (next?.seat !== undefined) setSeat(next.seat);
    setMoves([]);
  };
  const undo = () => {
    request.current++;
    setThinking(false);
    setDismissed(false);
    setMoves(previous => {
      if (local) return previous.slice(0, -1);
      const drop = (previous.length % 2 === 0) === (seat === 0) ? 2 : 1;
      return previous.slice(0, Math.max(0, previous.length - drop));
    });
  };
  const canUndo = moves.length > (local || seat === 0 ? 0 : 1) && !thinking;
  const move = (next: string) => { if (!botTurn && !over) setMoves(previous => [...previous, next]); };
  const bottom = local ? position.turn : seat;
  const top = bottom === 0 ? 1 : 0;
  const nameOf = (s: number) => local ? seatNames[s]! : s === seat ? "You" : "Computer";
  const title = won === undefined ? "" : local ? `${seatNames[won]} wins` : won === seat ? "You won" : "You lost";
  const status = over ? `${title} · Reached the far side` : thinking ? "Computer is thinking" : `${seatNames[position.turn]} to move`;
  const player = (s: 0 | 1) => (
    <ArenaPlayer badge={<Pawn seat={s} className="size-[12px] border" />} side={seatNames[s]} name={nameOf(s)} bot={!local && s !== seat}
      difficulty={!local && s !== seat ? opponents.find(item => item.value === opponent)?.label : undefined} active={!over && position.turn === s}
      extra={<WallsLeft count={position.left[s]} seat={s} />} />
  );

  return (
    <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
      <section aria-label="Practice game" className="grid min-w-0 gap-3">
        <ArenaHeader title="Quoridor" lobby={lobbyHref(GameKey.Quoridor)}><ArenaTools restart={() => restart()} undo={undo} canUndo={canUndo}>{null}</ArenaTools></ArenaHeader>
        <span role="status" aria-live="polite" aria-atomic="true" data-game-status className="sr-only">{status}</span>
        {player(top)}
        <div className="relative">
          <QuoridorBoard pawns={pawns} walls={walls} seat={bottom} legal={legal} lastMove={moves.at(-1)} playable={!over && !botTurn} onMove={move} />
          {over && !dismissed ? <ArenaResult title={title} detail="Reached the far side" kind={local || won === seat ? "win" : "lose"} close={() => setDismissed(true)} restart={() => restart()} /> : null}
        </div>
        {player(bottom)}
      </section>
      <aside className="flex min-w-0 flex-col gap-3 lg:pt-14">
        <fieldset className="space-y-2 rounded-game-md border border-game-border bg-game-card p-3">
          <legend className="px-1 text-[13px] font-semibold">Opponent</legend>
          <div className="grid grid-cols-2 gap-1.5">
            {opponents.map(item => (
              <button key={item.value} type="button" aria-pressed={opponent === item.value} onClick={() => restart({ opponent: item.value })}
                className={cn("min-h-10 rounded-game-sm border border-game-border px-2 text-[13px] font-medium", opponent === item.value ? "border-game-primary bg-game-accent text-game-primary" : "hover:bg-game-bg2")}>{item.label}</button>
            ))}
          </div>
          {!local ? <div className="grid grid-cols-2 gap-1.5 pt-1">
            {([0, 1] as const).map(value => (
              <button key={value} type="button" aria-pressed={seat === value} onClick={() => restart({ seat: value })}
                className={cn("flex min-h-10 items-center justify-center gap-2 rounded-game-sm border border-game-border px-2 text-[13px] font-medium", seat === value ? "border-game-primary bg-game-accent text-game-primary" : "hover:bg-game-bg2")}>
                <Pawn seat={value} className="size-3.5 border" />{value === 0 ? "Move first" : "Move second"}
              </button>
            ))}
          </div> : null}
        </fieldset>
        <MoveList san={moves} />
      </aside>
    </div>
  );
}
