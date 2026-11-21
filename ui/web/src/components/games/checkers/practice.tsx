"use client";

import { useEffect, useMemo, useState, useSyncExternalStore } from "react";

import { CheckersBoard, CheckersPiece, PiecesLeft, seatNames } from "@/components/games/checkers/board";
import { MoveList } from "@/components/games/chess/moves";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools } from "@/components/games/tictactoe/arena";
import { ScrollArea } from "@/components/ui/scroll-area";
import { GameKey, lobbyHref } from "@/lib/games/catalog";
import { CheckersResult, legalMoves, outcome, Piece, play, sideOf, startPosition, type CheckersPosition, type Seat } from "@/lib/games/checkers";
import { chooseCheckersMove } from "@/lib/games/checkers-bot";
import { cn } from "@/lib/utils";

enum Opponent {
  Computer = "computer",
  Local = "local",
}

const opponents: { value: Opponent; label: string }[] = [
  { value: Opponent.Computer, label: "Computer" },
  { value: Opponent.Local, label: "Two players" },
];

const results: Record<CheckersResult, string> = {
  [CheckersResult.NoMoves]: "No moves left",
  [CheckersResult.FortyMoves]: "40-move rule",
  [CheckersResult.MoveLimit]: "Move limit",
};

function replay(moves: string[]) {
  return moves.reduce<CheckersPosition>((position, move) => play(position, move), startPosition());
}

const subscribeHydration = () => () => {};

export function CheckersPractice() {
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
  const [opponent, setOpponent] = useState(Opponent.Computer);
  const [seat, setSeat] = useState<Seat>(0);
  const [moves, setMoves] = useState<string[]>([]);
  const [dismissed, setDismissed] = useState(false);
  const position = useMemo(() => replay(moves), [moves]);
  const local = opponent === Opponent.Local;
  const ended = outcome(position);
  const over = ended !== undefined;
  const botTurn = !local && position.turn !== seat && !over;
  const legal = useMemo(() => over || botTurn ? [] : legalMoves(position), [over, botTurn, position]);

  useEffect(() => {
    if (!botTurn) return;
    const timer = setTimeout(() => {
      const move = chooseCheckersMove(position);
      if (move) setMoves(previous => [...previous, move]);
    }, 350);
    return () => clearTimeout(timer);
  }, [botTurn, position]);

  const restart = (next?: { opponent?: Opponent; seat?: Seat }) => {
    setDismissed(false);
    if (next?.opponent) setOpponent(next.opponent);
    if (next?.seat !== undefined) setSeat(next.seat);
    setMoves([]);
  };
  const undo = () => {
    setDismissed(false);
    setMoves(previous => {
      if (local) return previous.slice(0, -1);
      const drop = (previous.length % 2 === 0) === (seat === 0) ? 2 : 1;
      return previous.slice(0, Math.max(0, previous.length - drop));
    });
  };
  const canUndo = moves.length > (local || seat === 0 ? 0 : 1) && !botTurn;
  const move = (next: string) => { if (!botTurn && !over) setMoves(previous => [...previous, next]); };
  const bottom: Seat = local ? position.turn : seat;
  const top: Seat = bottom === 0 ? 1 : 0;
  const nameOf = (s: Seat) => local ? seatNames[s]! : s === seat ? "You" : "Computer";
  const winner = ended?.winner;
  const title = !ended ? "" : winner === undefined ? "Draw" : local ? `${seatNames[winner]} wins` : winner === seat ? "You won" : "You lost";
  const why = ended ? results[ended.result] : "";
  const status = over ? `${title} · ${why}` : botTurn ? "Computer is thinking" : `${seatNames[position.turn]} to move`;
  const player = (s: Seat) => (
    <ArenaPlayer badge={<CheckersPiece piece={s === 0 ? Piece.BlackMan : Piece.WhiteMan} className="size-[13px]" />} side={seatNames[s]} name={nameOf(s)} bot={!local && s !== seat}
      active={!over && position.turn === s} extra={<PiecesLeft count={position.board.filter(piece => sideOf(piece) === s).length} />} />
  );

  return (
    <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
      <section aria-label="Practice game" className="grid min-w-0 gap-3">
        <ArenaHeader title="Checkers" lobby={lobbyHref(GameKey.Checkers)}><ArenaTools restart={() => restart()} undo={undo} canUndo={canUndo}>{null}</ArenaTools></ArenaHeader>
        <span role="status" aria-live="polite" aria-atomic="true" data-game-status className="sr-only">{status}</span>
        {player(top)}
        <div className="relative">
          <CheckersBoard squares={position.board} seat={bottom} legal={legal} lastMove={moves.at(-1)} playable={!over && !botTurn} onMove={move} />
          {over && !dismissed ? <ArenaResult title={title} detail={why} kind={winner === undefined ? "draw" : local || winner === seat ? "win" : "lose"} close={() => setDismissed(true)} restart={() => restart()} /> : null}
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
                <CheckersPiece piece={value === 0 ? Piece.BlackMan : Piece.WhiteMan} className="size-3.5" />{value === 0 ? "Move first" : "Move second"}
              </button>
            ))}
          </div> : null}
        </fieldset>
        <MoveList san={moves} />
      </aside>
    </div>
  );
}
