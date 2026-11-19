"use client";

import { Chess } from "chess.js";
import { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";

import { ChessBoard } from "@/components/games/chess/board";
import { MoveList } from "@/components/games/chess/moves";
import { ChessPiece, PieceKind, Side } from "@/components/games/chess/pieces";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools } from "@/components/games/tictactoe/arena";
import { ScrollArea } from "@/components/ui/scroll-area";
import { GameKey, lobbyHref } from "@/lib/games/catalog";
import { BotLevel, uciOf } from "@/lib/games/chess-bot";
import type { BotReply, BotRequest } from "@/lib/games/chess-bot.worker";
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

const levels: Record<Exclude<Opponent, Opponent.Local>, BotLevel> = {
  [Opponent.Easy]: BotLevel.Easy,
  [Opponent.Medium]: BotLevel.Medium,
  [Opponent.Hard]: BotLevel.Hard,
};

function replay(moves: string[]) {
  const chess = new Chess();
  for (const uci of moves) chess.move({ from: uci.slice(0, 2), to: uci.slice(2, 4), promotion: uci[4] });
  return chess;
}

function ending(chess: Chess) {
  if (chess.isCheckmate()) return "Checkmate";
  if (chess.isStalemate()) return "Stalemate";
  if (chess.isInsufficientMaterial()) return "Insufficient material";
  if (chess.isThreefoldRepetition()) return "Threefold repetition";
  if (chess.isDrawByFiftyMoves()) return "50-move rule";
  return "";
}

const subscribeHydration = () => () => {};

export function ChessPractice() {
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
  const [side, setSide] = useState(Side.White);
  const [moves, setMoves] = useState<string[]>([]);
  const [thinking, setThinking] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const worker = useRef<Worker | null>(null);
  const request = useRef(0);
  const chess = useMemo(() => replay(moves), [moves]);
  const fen = chess.fen();
  const turn = chess.turn() === "w" ? Side.White : Side.Black;
  const local = opponent === Opponent.Local;
  const botTurn = !local && turn !== side && !chess.isGameOver();
  const over = chess.isGameOver();
  const why = ending(chess);
  const history = chess.history({ verbose: true });
  const last = history.at(-1);
  const legal = over || botTurn ? [] : chess.moves({ verbose: true }).map(uciOf);

  useEffect(() => {
    const instance = new Worker(new URL("../../../lib/games/chess-bot.worker.ts", import.meta.url), { type: "module" });
    instance.onmessage = (event: MessageEvent<BotReply>) => {
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
      worker.current?.postMessage({ id, fen, level: levels[opponent as Exclude<Opponent, Opponent.Local>] } satisfies BotRequest);
    }, 250);
    return () => clearTimeout(timer);
  }, [botTurn, local, fen, opponent]);

  const restart = (next?: { opponent?: Opponent; side?: Side }) => {
    request.current++;
    setThinking(false);
    setDismissed(false);
    if (next?.opponent) setOpponent(next.opponent);
    if (next?.side) setSide(next.side);
    setMoves([]);
  };
  const undo = () => {
    request.current++;
    setThinking(false);
    setDismissed(false);
    setMoves(previous => {
      if (local) return previous.slice(0, -1);
      const drop = (previous.length % 2 === 0) === (side === Side.White) ? 2 : 1;
      return previous.slice(0, Math.max(0, previous.length - drop));
    });
  };
  const canUndo = moves.length > (local || side === Side.White ? 0 : 1) && !thinking;
  const play = (uci: string) => { if (!botTurn) setMoves(previous => [...previous, uci]); };
  const orientation = local ? turn : side;
  const bottom = local ? turn : side;
  const top = bottom === Side.White ? Side.Black : Side.White;
  const nameOf = (s: Side) => local ? (s === Side.White ? "White" : "Black") : s === side ? "You" : "Computer";
  const winner = chess.isCheckmate() ? (turn === Side.White ? Side.Black : Side.White) : null;
  const title = !winner ? "Draw" : local ? `${winner === Side.White ? "White" : "Black"} wins` : winner === side ? "You won" : "You lost";
  const status = over ? `${title} · ${why}` : thinking ? "Computer is thinking" : chess.inCheck() ? `Check · ${turn === Side.White ? "White" : "Black"} to move` : `${turn === Side.White ? "White" : "Black"} to move`;

  return (
    <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
      <section aria-label="Practice game" className="grid min-w-0 gap-3">
        <ArenaHeader title="Chess" lobby={lobbyHref(GameKey.Chess)}><ArenaTools restart={() => restart()} undo={undo} canUndo={canUndo}>{null}</ArenaTools></ArenaHeader>
        <span role="status" aria-live="polite" aria-atomic="true" data-game-status className="sr-only">{status}</span>
        <ArenaPlayer badge={<ChessPiece kind={PieceKind.King} side={top} className="size-[13px]" />} side={top === Side.White ? "White" : "Black"} name={nameOf(top)} bot={!local && top !== side} difficulty={!local && top !== side ? opponents.find(item => item.value === opponent)?.label : undefined} active={!over && turn === top} />
        <div className="relative">
          <ChessBoard fen={fen} orientation={orientation} legal={legal} lastMove={last ? uciOf(last) : undefined} check={chess.inCheck()} playable={!over && !botTurn} onMove={play} />
          {over && !dismissed ? <ArenaResult title={title} detail={why} kind={!winner ? "draw" : local || winner === side ? "win" : "lose"} close={() => setDismissed(true)} restart={() => restart()} /> : null}
        </div>
        <ArenaPlayer badge={<ChessPiece kind={PieceKind.King} side={bottom} className="size-[13px]" />} side={bottom === Side.White ? "White" : "Black"} name={nameOf(bottom)} active={!over && turn === bottom} />
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
            {[Side.White, Side.Black].map(value => (
              <button key={value} type="button" aria-pressed={side === value} onClick={() => restart({ side: value })}
                className={cn("flex min-h-10 items-center justify-center gap-2 rounded-game-sm border border-game-border px-2 text-[13px] font-medium", side === value ? "border-game-primary bg-game-accent text-game-primary" : "hover:bg-game-bg2")}>
                <ChessPiece kind={PieceKind.King} side={value} className="size-4" />Play {value === Side.White ? "white" : "black"}
              </button>
            ))}
          </div> : null}
        </fieldset>
        <MoveList san={history.map(move => move.san)} />
      </aside>
    </div>
  );
}
