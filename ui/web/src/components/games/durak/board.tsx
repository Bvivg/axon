"use client";

import { useRef, useState, type KeyboardEvent } from "react";

import { CardBack, CardFace } from "@/components/games/durak/card";
import { actionClass } from "@/components/games/tictactoe/arena";
import { cardMove, cardName, suitNames, suitOf, suitSymbols, type DurakHandView, type DurakView } from "@/lib/games/durak";
import { cn } from "@/lib/utils";

export const seatNames = ["North", "South"];

function Fan({ hand, label, testId }: { hand?: DurakHandView; label: string; testId: string }) {
  const count = hand?.count ?? 0;
  const faces = hand && !hand.hidden ? hand.cards : [];
  return (
    <div data-testid={testId} data-count={count} data-hidden={hand?.hidden || undefined} className="flex min-h-[clamp(56px,15vw,84px)] items-center justify-center gap-3">
      <span className="sr-only">{hand?.hidden ? `${label}: ${count} cards, hidden` : `${label}: ${faces.map(cardName).join(", ") || "no cards"}`}</span>
      <div aria-hidden className="flex items-center">
        {faces.length ? faces.map((card, at) => <CardFace key={card} card={card} className={cn(at > 0 && "-ml-[clamp(18px,5vw,28px)]")} />)
          : Array.from({ length: Math.min(count, 12) }, (_, at) => <CardBack key={at} className={cn(at > 0 && "-ml-[clamp(26px,7vw,40px)]")} />)}
      </div>
      {hand?.hidden ? <span aria-hidden className="rounded-full bg-game-card px-2 py-0.5 font-mono text-[12px] text-game-text2 tabular-nums">{count}</span> : null}
    </div>
  );
}

function Stock({ view }: { view: DurakView }) {
  const suit = suitOf(view.trump);
  return (
    <div data-testid="durak-stock" data-stock={view.stock} className="relative flex w-[clamp(64px,17vw,96px)] shrink-0 flex-col items-center gap-1">
      <span className="sr-only">{`Stock: ${view.stock} cards. Trump: ${view.stock > 0 ? cardName(view.trump) : suitNames[suit]}`}</span>
      <div aria-hidden className="relative w-[clamp(58px,16vw,87px)]">
        {view.stock > 0 ? <>
          <CardFace card={view.trump} />
          {view.stock > 1 ? <CardBack className="absolute top-0 right-0" /> : null}
        </> : <span className="mx-auto grid aspect-[5/7] w-[clamp(40px,11vw,60px)] place-items-center rounded-[14%] border border-dashed border-game-border text-[clamp(18px,5vw,28px)] text-game-text2">{suitSymbols[suit]}</span>}
      </div>
      <span aria-hidden className="font-mono text-[12px] text-game-text2 tabular-nums">{view.stock}</span>
    </div>
  );
}

function Discard({ count }: { count: number }) {
  return (
    <div data-testid="durak-discard" data-discard={count} className="flex w-[clamp(48px,13vw,72px)] shrink-0 flex-col items-center gap-1">
      <span className="sr-only">{`Discard: ${count} cards`}</span>
      <div aria-hidden className="relative grid place-items-center">
        {count > 0 ? <><CardBack className="rotate-[8deg]" /><CardBack className="absolute -rotate-[6deg]" /></>
          : <span className="aspect-[5/7] w-[clamp(40px,11vw,60px)] rounded-[14%] border border-dashed border-game-border" />}
      </div>
      <span aria-hidden className="font-mono text-[12px] text-game-text2 tabular-nums">{count}</span>
    </div>
  );
}

function Table({ view }: { view: DurakView }) {
  return (
    <ul aria-label="Table" data-testid="durak-table" className="flex min-h-[clamp(80px,22vw,120px)] min-w-0 flex-1 flex-wrap content-center items-center justify-center gap-x-3 gap-y-2">
      {view.table.map(pair => (
        <li key={pair.attack} data-attack={pair.attack} data-defense={pair.defense} aria-label={pair.defense ? `${cardName(pair.attack)}, beaten by the ${cardName(pair.defense)}` : `${cardName(pair.attack)}, not beaten`}
          className="relative pr-[clamp(10px,3vw,16px)] pb-[clamp(10px,3vw,16px)]">
          <CardFace card={pair.attack} />
          {pair.defense ? <CardFace card={pair.defense} className="absolute top-[clamp(10px,3vw,16px)] left-[clamp(10px,3vw,16px)] rotate-[8deg]" /> : null}
        </li>
      ))}
      {!view.table.length ? <li className="text-[13px] text-game-text2">{view.taking ? "" : "Empty table"}</li> : null}
    </ul>
  );
}

function Hand({ hand, legal, playable, onMove }: { hand?: DurakHandView; legal: string[]; playable: boolean; onMove: (move: string) => void }) {
  const cards = hand?.cards ?? [];
  const [focused, setFocused] = useState(0);
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const index = Math.min(focused, Math.max(0, cards.length - 1));
  const focus = (at: number) => {
    const next = Math.max(0, Math.min(cards.length - 1, at));
    setFocused(next);
    buttons.current[next]?.focus();
  };
  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>, at: number) => {
    const next = { ArrowLeft: at - 1, ArrowRight: at + 1, ArrowUp: at - 1, ArrowDown: at + 1, Home: 0, End: cards.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    focus(next);
  };
  if (hand?.hidden) return <Fan hand={hand} label="Hand" testId="durak-bottom-hand" />;
  return (
    <div role="toolbar" aria-label="Your hand" data-testid="durak-hand" className="flex min-h-[clamp(64px,18vw,96px)] flex-wrap items-end justify-center gap-y-1.5 pt-2">
      {cards.length ? cards.map((card, at) => {
        const move = playable ? cardMove(card, legal) : undefined;
        const action = move?.startsWith("defend ") ? `beat the ${cardName(move.split(" ")[1]!)}` : move ? "attack" : "cannot be played now";
        return (
          <button key={card} ref={element => { buttons.current[at] = element; }} type="button" data-hand-card={card} data-legal={move ? "true" : undefined}
            aria-label={`${cardName(card)}, ${action}`} aria-disabled={!move} tabIndex={at === index ? 0 : -1}
            onFocus={() => setFocused(at)} onKeyDown={event => onKeyDown(event, at)} onClick={() => { if (move) onMove(move); }}
            className={cn("relative rounded-[14%] outline-none transition-transform duration-150 hover:z-10 focus-visible:z-10 motion-reduce:transition-none focus-visible:ring-2 focus-visible:ring-game-primary focus-visible:ring-offset-2 focus-visible:ring-offset-durak-felt",
              at > 0 && (cards.length > 8 ? "-ml-[clamp(16px,4.5vw,30px)]" : "-ml-[clamp(8px,2.5vw,16px)]"),
              move ? "cursor-pointer hover:-translate-y-1.5 [&>span]:ring-2 [&>span]:ring-game-primary/70" : playable ? "cursor-default brightness-[0.86] saturate-50" : "cursor-default")}>
            <CardFace card={card} />
          </button>
        );
      }) : <p className="pb-4 text-[13px] text-game-text2">No cards</p>}
    </div>
  );
}

export function DurakTable({ view, seat, legal, playable, onMove }: {
  view?: DurakView;
  seat: number;
  legal: string[];
  playable: boolean;
  onMove: (move: string) => void;
}) {
  const bottom = seat === 1 ? 1 : 0;
  const top = 1 - bottom;
  const canTake = playable && legal.includes("take");
  const canPass = playable && legal.includes("pass");
  const waiting = view !== undefined && !view.hands.length;
  return (
    <div role="group" aria-label="Durak table" className="grid gap-2 rounded-game-md border border-game-border bg-durak-felt p-[clamp(8px,2.5vw,16px)]">
      {waiting ? <p className="grid min-h-[clamp(200px,55vw,300px)] place-items-center text-[13px] text-game-text2">Waiting for opponent</p> : <>
        <Fan hand={view?.hands[top]} label={`${seatNames[top]} hand`} testId="durak-top-hand" />
        {view ? <div className="flex items-center gap-2">
          <Stock view={view} />
          <Table view={view} />
          <Discard count={view.discard} />
        </div> : null}
        <Hand hand={view?.hands[bottom]} legal={legal} playable={playable} onMove={onMove} />
      </>}
      {canTake || canPass ? <div className="flex justify-center gap-2">
        {canTake ? <button type="button" className={cn(actionClass, "min-w-28")} onClick={() => onMove("take")}>Take</button> : null}
        {canPass ? <button type="button" className={cn(actionClass, "min-w-28 border-game-primary bg-game-primary text-game-on-primary hover:bg-game-primary hover:brightness-110")} onClick={() => onMove("pass")}>Pass</button> : null}
      </div> : null}
    </div>
  );
}
