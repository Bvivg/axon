"use client";

import { useEffect, useMemo, useState, useSyncExternalStore } from "react";

import { DurakTable } from "@/components/games/durak/board";
import { DurakMoves, RoleBadge } from "@/components/games/durak/moves";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools } from "@/components/games/tictactoe/arena";
import { ScrollArea } from "@/components/ui/scroll-area";
import { GameKey, lobbyHref } from "@/lib/games/catalog";
import { cardName, deal, legalMoves, openAttack, outcome, play, turn, viewOf, type DurakPosition } from "@/lib/games/durak";
import { botMove } from "@/lib/games/durak-bot";

const subscribeHydration = () => () => {};

export function DurakPractice() {
  const hydrated = useSyncExternalStore(subscribeHydration, () => true, () => false);
  return (
    <ScrollArea className="min-h-0 flex-1 bg-game-workspace" viewportProps={{ className: "[&>div]:min-h-full" }}>
      <main className="flex min-h-full w-full flex-col px-[max(16px,env(safe-area-inset-left))] pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
        {hydrated ? <Practice /> : <span role="status" className="sr-only">Loading game</span>}
      </main>
    </ScrollArea>
  );
}

function fresh() {
  const random = new Uint32Array(64);
  crypto.getRandomValues(random);
  let at = 0;
  return deal(() => random[at++ % random.length]! / 2 ** 32);
}

function Practice() {
  const [position, setPosition] = useState<DurakPosition>(fresh);
  const [dismissed, setDismissed] = useState(false);
  const result = outcome(position);
  const toAct = turn(position);
  const botTurn = !result.over && toAct === 1;
  const legal = useMemo(() => result.over || botTurn ? [] : legalMoves(position), [result.over, botTurn, position]);

  useEffect(() => {
    if (!botTurn) return;
    const timer = setTimeout(() => {
      const move = botMove(position);
      if (move) setPosition(current => current === position ? play(current, move) : current);
    }, 450);
    return () => clearTimeout(timer);
  }, [botTurn, position]);

  const restart = () => {
    setDismissed(false);
    setPosition(fresh());
  };
  const move = (next: string) => { if (!botTurn && legal.includes(next)) setPosition(current => play(current, next)); };
  const title = !result.over ? "" : result.winner === undefined ? "Draw" : result.winner === 0 ? "You won" : "You lost";
  const why = !result.over ? "" : result.winner === undefined ? "Both out together" : "Last one holding cards";
  const open = openAttack(position);
  const task = position.attacker !== 0 ? (open ? `defend the ${cardName(open)}` : "defend") : !position.table.length ? "attack" : "throw in or pass";
  const status = result.over ? `${title} · ${why}` : botTurn ? "Computer is thinking" : `Your move · ${task}`;
  const player = (seat: 0 | 1) => (
    <ArenaPlayer badge={<RoleBadge attacking={position.attacker === seat} />} side={seat === 0 ? "You" : "Computer"} name={seat === 0 ? "You" : "Computer"} bot={seat === 1}
      active={!result.over && toAct === seat} extra={<span data-hand-count={position.hands[seat].length} className="text-[13px] text-game-text2">{`${position.hands[seat].length} cards`}</span>} />
  );

  return (
    <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
      <section aria-label="Practice game" className="grid min-w-0 gap-3">
        <ArenaHeader title="Durak" lobby={lobbyHref(GameKey.Durak)}><ArenaTools restart={restart} undo={() => undefined} canUndo={false} undoHidden>{null}</ArenaTools></ArenaHeader>
        <span role="status" aria-live="polite" aria-atomic="true" data-game-status className="sr-only">{status}</span>
        {player(1)}
        <div className="relative">
          <DurakTable view={viewOf(position, 0, result.over)} seat={0} legal={legal} playable={!botTurn && !result.over} onMove={move} />
          {result.over && !dismissed ? <ArenaResult title={title} detail={why} kind={result.winner === undefined ? "draw" : result.winner === 0 ? "win" : "lose"} close={() => setDismissed(true)} restart={restart} /> : null}
        </div>
        {player(0)}
        {!result.over ? <p aria-hidden className="text-center text-[13px] text-game-text2">{status}</p> : null}
      </section>
      <aside className="flex min-w-0 flex-col gap-3 lg:pt-14">
        <DurakMoves moves={position.moves} />
      </aside>
    </div>
  );
}
