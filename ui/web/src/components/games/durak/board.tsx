"use client";

import { useLayoutEffect, useRef, useState, type KeyboardEvent, type PointerEvent, type ReactNode } from "react";

import { CardBack, CardFace } from "@/components/games/durak/card";
import { actionClass } from "@/components/games/tictactoe/arena";
import { cardMove, cardName, suitNames, suitOf, suitSymbols, type DurakHandView, type DurakView } from "@/lib/games/durak";
import { flyFrom, flyTo, reducedMotion } from "@/lib/games/motion";
import { cn } from "@/lib/utils";

export const seatNames = ["North", "South"];

type Pair = DurakView["table"][number];
type Drag = { card: string; x: number; y: number; startX: number; startY: number; moved: boolean };
type Drop = { kind: "table" } | { kind: "pair"; attack: string };
type Snapshot = { key: string; table: Pair[]; discard: number; counts: number[]; cards: string[][] };
type Leaving = { pairs: Pair[]; to: string; id: string };
type Flights = { id: string; played: string[]; drawn: string[][]; backs: number[]; deal: boolean; attacker: number; defender: number; by: Record<string, number> };

const seatHand = (seat: number) => `[data-seat-hand="${seat}"]`;

function snapshotOf(view: DurakView | undefined): Snapshot {
  const table = view?.table ?? [];
  const counts = view?.hands.map(hand => hand.count) ?? [];
  const cards = view?.hands.map(hand => hand.hidden ? [] : hand.cards) ?? [];
  return {
    key: [table.map(pair => `${pair.attack}${pair.defense ?? ""}`).join(","), view?.discard, view?.stock, counts.join(","), cards.map(hand => hand.join("")).join("|")].join(";"),
    table, discard: view?.discard ?? 0, counts, cards,
  };
}

function freshDeal(view: DurakView): boolean {
  return view.discard === 0 && view.table.length === 0 && view.hands.length > 0 && view.hands.every(hand => hand.count === 6);
}

function defenderOf(view: DurakView): number {
  return view.defenderSeat ?? (view.attackerSeat + 1) % Math.max(view.hands.length, 1);
}

function dropAt(x: number, y: number): Drop | null {
  const element = document.elementFromPoint(x, y)?.closest<HTMLElement>("[data-drop]");
  if (!element) return null;
  const attack = element.dataset.drop;
  return attack === "table" ? { kind: "table" } : attack ? { kind: "pair", attack } : null;
}

function moveFor(card: string, drop: Drop, legal: string[], cheat: "attack" | "defend" | null): string | undefined {
  if (drop.kind === "pair") {
    const fair = legal.find(move => move === `defend ${drop.attack} ${card}`);
    return fair ?? (cheat === "defend" ? `defend ${drop.attack} ${card}` : undefined);
  }
  const options = legal.filter(move => move === `attack ${card}` || move === `transfer ${card}` || (move.startsWith("defend ") && move.endsWith(` ${card}`)));
  const fair = options.length === 1 ? options[0] : options.find(move => !move.startsWith("defend "));
  return fair ?? (cheat === "attack" ? `attack ${card}` : undefined);
}

function Fan({ hand, label, testId, seat, compact = false }: { hand?: DurakHandView; label: string; testId: string; seat: number; compact?: boolean }) {
  const count = hand?.count ?? 0;
  const faces = hand && !hand.hidden ? hand.cards : [];
  const shown = compact ? Math.min(count, 6) : Math.min(count, 12);
  return (
    <div data-testid={testId} data-seat-hand={seat} data-count={count} data-hidden={hand?.hidden || undefined}
      className={cn("flex items-center justify-center gap-3", compact ? "min-h-[clamp(44px,11vw,60px)]" : "min-h-[clamp(56px,15vw,84px)]")}>
      <span className="sr-only">{hand?.hidden ? `${label}: ${count} cards, hidden` : `${label}: ${faces.map(cardName).join(", ") || "no cards"}`}</span>
      <div aria-hidden className={cn("flex items-center", compact && "origin-center scale-[.72]")}>
        {faces.length ? faces.map((card, at) => <CardFace key={card} card={card} className={cn(at > 0 && "-ml-[clamp(18px,5vw,28px)]")} />)
          : Array.from({ length: shown }, (_, at) => <CardBack key={at} className={cn("relative", at > 0 && (compact ? "-ml-[clamp(32px,8.5vw,48px)]" : "-ml-[clamp(26px,7vw,40px)]"))} />)}
      </div>
      {hand?.hidden && !compact ? <span aria-hidden className="rounded-full bg-game-card px-2 py-0.5 font-mono text-[12px] text-game-text2 tabular-nums">{count}</span> : null}
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

function Catchable({ card, catching, onCatch, className }: { card: string; catching: boolean; onCatch: (card: string) => void; className?: string }) {
  if (!catching) return <CardFace card={card} className={className} />;
  return (
    <button type="button" data-catch={card} aria-label={`Catch the ${cardName(card)}`} onClick={() => onCatch(card)}
      className={cn("rounded-[14%] outline-none [&>span]:ring-2 [&>span]:ring-game-error/70 hover:[&>span]:ring-game-error focus-visible:[&>span]:ring-game-error", className)}>
      <CardFace card={card} />
    </button>
  );
}

function Table({ view, leaving, drag, drop, legal, cheat, catching, onCatch }: {
  view: DurakView;
  leaving: Leaving | null;
  drag: Drag | null;
  drop: Drop | null;
  legal: string[];
  cheat: "attack" | "defend" | null;
  catching: boolean;
  onCatch: (card: string) => void;
}) {
  const canAttack = drag?.moved ? cheat === "attack" || legal.some(move => move === `attack ${drag.card}` || move === `transfer ${drag.card}`) : false;
  const canDefend = (attack: string) => drag?.moved ? cheat === "defend" || legal.includes(`defend ${attack} ${drag.card}`) : false;
  const catchable = (card: string) => catching && legal.includes(`catch ${card}`);
  const gone = leaving?.pairs ?? [];
  return (
    <ul aria-label="Table" data-testid="durak-table" data-drop="table"
      className={cn("flex min-h-[clamp(80px,22vw,120px)] min-w-0 flex-1 flex-wrap content-center items-center justify-center gap-x-3 gap-y-3 rounded-game-md pt-2 transition-shadow duration-150",
        canAttack && "ring-2 ring-game-primary/50", canAttack && drop?.kind === "table" && "ring-game-primary bg-game-primary/10")}>
      {view.table.map(pair => (
        <li key={pair.attack} data-attack={pair.attack} data-defense={pair.defense} data-by={pair.by} data-drop={pair.defense ? undefined : pair.attack}
          aria-label={pair.defense ? `${cardName(pair.attack)}, beaten by the ${cardName(pair.defense)}` : `${cardName(pair.attack)}, not beaten`}
          className="relative rounded-[14%] pr-[clamp(10px,3vw,16px)] pb-[clamp(10px,3vw,16px)]">
          {catchable(pair.attack) ? <Catchable card={pair.attack} catching onCatch={onCatch} className="block" />
            : <CardFace card={pair.attack} className={cn(canDefend(pair.attack) && "ring-2 ring-game-primary/60", canDefend(pair.attack) && drop?.kind === "pair" && drop.attack === pair.attack && "ring-game-primary")} />}
          {pair.defense ? <Catchable card={pair.defense} catching={catchable(pair.defense)} onCatch={onCatch} className="absolute top-[clamp(10px,3vw,16px)] left-[clamp(10px,3vw,16px)] rotate-[8deg]" /> : null}
        </li>
      ))}
      {gone.map(pair => (
        <li key={`${leaving?.id}-${pair.attack}`} aria-hidden data-leaving className="relative pr-[clamp(10px,3vw,16px)] pb-[clamp(10px,3vw,16px)]">
          <CardFace card={pair.attack} />
          {pair.defense ? <CardFace card={pair.defense} className="absolute top-[clamp(10px,3vw,16px)] left-[clamp(10px,3vw,16px)] rotate-[8deg]" /> : null}
        </li>
      ))}
      {!view.table.length && !gone.length ? <li className="text-[13px] text-game-text2">{view.taking ? "" : "Empty table"}</li> : null}
    </ul>
  );
}

function Hand({ hand, seat, legal, playable, free, dragged, onMove, onGrab, onPlay }: {
  hand?: DurakHandView;
  seat: number;
  legal: string[];
  playable: boolean;
  free: boolean;
  dragged?: string;
  onMove: (move: string) => void;
  onGrab: (event: PointerEvent<HTMLButtonElement>, card: string) => void;
  onPlay: (card: string, from: DOMRect) => void;
}) {
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
  if (hand?.hidden) return <Fan hand={hand} label="Hand" testId="durak-bottom-hand" seat={seat} />;
  return (
    <div role="toolbar" aria-label="Your hand" data-testid="durak-hand" data-seat-hand={seat} className="flex min-h-[clamp(64px,18vw,96px)] flex-wrap items-end justify-center gap-y-1.5 pt-2">
      {cards.length ? cards.map((card, at) => {
        const move = playable ? cardMove(card, legal) : undefined;
        const draggable = Boolean(move) || (playable && free);
        const action = move?.startsWith("defend ") ? `beat the ${cardName(move.split(" ")[1]!)}` : move?.startsWith("transfer ") ? "transfer" : move ? "attack" : free && playable ? "drag to play" : "cannot be played now";
        return (
          <button key={card} ref={element => { buttons.current[at] = element; }} type="button" data-hand-card={card} data-legal={move ? "true" : undefined}
            aria-label={`${cardName(card)}, ${action}`} aria-disabled={!move} tabIndex={at === index ? 0 : -1}
            onFocus={() => setFocused(at)} onKeyDown={event => onKeyDown(event, at)} onClick={event => { if (!move) return; onPlay(card, event.currentTarget.getBoundingClientRect()); onMove(move); }}
            onPointerDown={event => { if (draggable) onGrab(event, card); }}
            className={cn("relative rounded-[14%] outline-none transition-transform duration-150 focus-visible:z-10 motion-reduce:transition-none focus-visible:ring-2 focus-visible:ring-game-primary focus-visible:ring-offset-2 focus-visible:ring-offset-durak-felt",
              at > 0 && (cards.length > 8 ? "-ml-[clamp(16px,4.5vw,30px)]" : "-ml-[clamp(8px,2.5vw,16px)]"),
              move ? "cursor-grab touch-none hover:z-10 hover:-translate-y-1.5 [&>span]:ring-2 [&>span]:ring-game-primary/70"
                : draggable ? "cursor-grab touch-none hover:z-10 hover:-translate-y-1"
                : playable ? "cursor-default brightness-[0.86] saturate-50" : "cursor-default",
              dragged === card && "opacity-30")}>
            <CardFace card={card} />
          </button>
        );
      }) : <p className="pb-4 text-[13px] text-game-text2">No cards</p>}
    </div>
  );
}

export function DurakTable({ view, seat, legal, playable, onMove, cheats = false, label }: {
  view?: DurakView;
  seat: number;
  legal: string[];
  playable: boolean;
  onMove: (move: string) => void;
  cheats?: boolean;
  label?: (seat: number) => ReactNode;
}) {
  const count = Math.max(view?.hands.length ?? 2, 2);
  const bottom = Math.min(Math.max(seat, 0), count - 1);
  const opponents = Array.from({ length: count - 1 }, (_, at) => (bottom + at + 1) % count);
  const wide = count > 2;
  const canTake = playable && legal.includes("take");
  const canPass = playable && legal.includes("pass");
  const canCatch = legal.some(move => move.startsWith("catch "));
  const [catchMode, setCatchMode] = useState(false);
  if (catchMode && !canCatch) setCatchMode(false);
  const catching = catchMode && canCatch;
  const waiting = view !== undefined && !view.hands.length;
  const defending = view ? defenderOf(view) === bottom : false;
  const cheat = cheats && playable ? defending ? "defend" : "attack" : null;
  const root = useRef<HTMLDivElement>(null);
  const origin = useRef<{ card: string; rect: DOMRect } | null>(null);
  const [drag, setDrag] = useState<Drag | null>(null);
  const [drop, setDrop] = useState<Drop | null>(null);
  const [shown, setShown] = useState(() => snapshotOf(view));
  const [leaving, setLeaving] = useState<Leaving | null>(null);
  const [flights, setFlights] = useState<Flights | null>(() => view && freshDeal(view)
    ? { id: "deal", played: [], drawn: view.hands.map(hand => hand.hidden ? [] : hand.cards), backs: view.hands.map(hand => hand.hidden ? hand.count : 0), deal: true, attacker: view.attackerSeat, defender: defenderOf(view), by: {} }
    : null);

  const next = snapshotOf(view);
  if (view && next.key !== shown.key) {
    const id = next.key;
    const before = new Set(shown.table.flatMap(pair => pair.defense ? [pair.attack, pair.defense] : [pair.attack]));
    const tableCards = shown.table.flatMap(pair => pair.defense ? [pair.attack, pair.defense] : [pair.attack]);
    const cleared = shown.table.length > 0 && next.table.length === 0;
    const grew = next.counts.map((amount, at) => amount - (shown.counts[at] ?? 0));
    const most = grew.reduce((best, amount, at) => amount > (grew[best] ?? 0) ? at : best, 0);
    const taker = cleared && next.discard === shown.discard && (grew[most] ?? 0) > 0 ? most : null;
    const destination = cleared ? taker === null ? `[data-testid="durak-discard"]` : seatHand(taker) : "";
    setLeaving(cleared && !reducedMotion() ? { pairs: shown.table, to: destination, id } : null);
    const played = next.table.flatMap(pair => pair.defense ? [pair.attack, pair.defense] : [pair.attack]).filter(card => !before.has(card));
    const by = Object.fromEntries(next.table.flatMap(pair => pair.by === undefined ? [] : [[pair.attack, pair.by]]));
    const drawn = next.cards.map((cards, at) => cards.filter(card => !(shown.cards[at] ?? []).includes(card) && !(taker === at && tableCards.includes(card))));
    const backs = next.counts.map((amount, at) => view.hands[at]?.hidden ? Math.max(0, amount - (shown.counts[at] ?? 0) - (taker === at ? tableCards.length : 0)) : 0);
    setFlights({ id, played, drawn, backs, deal: false, attacker: view.attackerSeat, defender: defenderOf(view), by });
    setShown(next);
  }

  useLayoutEffect(() => {
    const host = root.current;
    if (!host || !flights) return;
    const stock = host.querySelector(`[data-testid="durak-stock"]`);
    const handOf = (at: number) => host.querySelector(seatHand(at));
    for (const card of flights.played) {
      const pair = host.querySelector(`[data-attack="${card}"]`);
      const element = pair ? pair.querySelector(`:scope > [data-card="${card}"]`) : host.querySelector(`[data-defense="${card}"] > [data-card="${card}"]`);
      const mine = origin.current?.card === card ? origin.current.rect : null;
      const playedBy = pair ? flights.by[card] ?? flights.attacker : flights.defender;
      flyFrom(element, mine ?? handOf(playedBy));
    }
    origin.current = null;
    let delay = flights.deal ? 0 : 160;
    flights.drawn.forEach((cards, at) => {
      for (const card of cards) {
        const element = host.querySelector(`${seatHand(at)} [data-hand-card="${card}"]`) ?? host.querySelector(`${seatHand(at)} [data-card="${card}"]`);
        flyFrom(element, stock, delay);
        delay += 70;
      }
    });
    flights.backs.forEach((amount, at) => {
      if (!amount) return;
      const backs = [...(handOf(at)?.querySelectorAll("[data-card-back]") ?? [])].slice(-amount);
      for (const element of backs) {
        flyFrom(element, stock, delay);
        delay += 70;
      }
    });
  }, [flights]);

  useLayoutEffect(() => {
    const host = root.current;
    if (!host || !leaving) return;
    const target = host.querySelector(leaving.to);
    const cards = [...host.querySelectorAll("[data-leaving]")];
    let pending = cards.length;
    const done = () => { pending -= 1; if (pending <= 0) setLeaving(current => current?.id === leaving.id ? null : current); };
    if (!cards.length || !cards.map((element, at) => flyTo(element, target, at * 50, done)).every(Boolean)) setLeaving(null);
  }, [leaving]);

  const grab = (event: PointerEvent<HTMLButtonElement>, card: string) => {
    if (event.button !== 0) return;
    const rect = root.current?.getBoundingClientRect();
    const x = event.clientX - (rect?.left ?? 0), y = event.clientY - (rect?.top ?? 0);
    setDrag({ card, x, y, startX: x, startY: y, moved: false });
  };

  const onPointerMove = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag) return;
    const rect = root.current?.getBoundingClientRect();
    const x = event.clientX - (rect?.left ?? 0), y = event.clientY - (rect?.top ?? 0);
    const moved = drag.moved || Math.hypot(x - drag.startX, y - drag.startY) > 6;
    if (moved && !drag.moved) root.current?.setPointerCapture(event.pointerId);
    setDrag({ ...drag, x, y, moved });
    setDrop(moved ? dropAt(event.clientX, event.clientY) : null);
  };

  const onPointerUp = (event: PointerEvent<HTMLDivElement>) => {
    if (!drag) return;
    if (drag.moved) {
      const target = dropAt(event.clientX, event.clientY);
      const move = target && playable ? moveFor(drag.card, target, legal, cheat) : undefined;
      const ghost = root.current?.querySelector("[data-drag-ghost]")?.getBoundingClientRect();
      if (move) {
        if (ghost) origin.current = { card: drag.card, rect: ghost };
        onMove(move);
      }
    }
    setDrag(null);
    setDrop(null);
  };

  const cancel = () => { setDrag(null); setDrop(null); };

  return (
    <div ref={root} role="group" aria-label="Durak table" data-seats={count} onPointerMove={onPointerMove} onPointerUp={onPointerUp} onPointerCancel={cancel}
      className="relative grid gap-2 rounded-game-md border border-game-border bg-durak-felt p-[clamp(8px,2.5vw,16px)]">
      {waiting ? <p className="grid min-h-[clamp(200px,55vw,300px)] place-items-center text-[13px] text-game-text2">Waiting for opponent</p> : <>
        {wide ? (
          <ul aria-label="Opponents" className="grid gap-2" style={{ gridTemplateColumns: `repeat(${Math.min(opponents.length, 3)}, minmax(0, 1fr))` }}>
            {opponents.map(at => (
              <li key={at} data-seat={at} className="grid min-w-0 justify-items-center gap-1">
                {label?.(at)}
                <Fan hand={view?.hands[at]} label={`Seat ${at + 1} hand`} testId={`durak-seat-${at}`} seat={at} compact />
              </li>
            ))}
          </ul>
        ) : <Fan hand={view?.hands[opponents[0]!]} label={`${seatNames[opponents[0]!] ?? "Opponent"} hand`} testId="durak-top-hand" seat={opponents[0]!} />}
        {view ? <div className="flex items-center gap-2">
          <Stock view={view} />
          <Table view={view} leaving={leaving} drag={drag} drop={drop} legal={legal} cheat={cheat} catching={catching} onCatch={card => { setCatchMode(false); onMove(`catch ${card}`); }} />
          <Discard count={view.discard} />
        </div> : null}
        <Hand hand={view?.hands[bottom]} seat={bottom} legal={legal} playable={playable} free={cheats} dragged={drag?.moved ? drag.card : undefined} onMove={onMove} onGrab={grab}
          onPlay={(card, rect) => { origin.current = { card, rect }; }} />
      </>}
      {canTake || canPass || canCatch ? <div className="flex justify-center gap-2">
        {canCatch ? <button type="button" aria-pressed={catching} className={cn(actionClass, "min-w-28", catching && "border-game-error text-game-error")} onClick={() => setCatchMode(!catching)}>{catching ? "Pick a card" : "Catch"}</button> : null}
        {canTake ? <button type="button" className={cn(actionClass, "min-w-28")} onClick={() => onMove("take")}>Take</button> : null}
        {canPass ? <button type="button" className={cn(actionClass, "min-w-28 border-game-primary bg-game-primary text-game-on-primary hover:bg-game-primary hover:brightness-110")} onClick={() => onMove("pass")}>Pass</button> : null}
      </div> : null}
      {drag?.moved ? <span aria-hidden data-drag-ghost className="pointer-events-none absolute z-30 -translate-x-1/2 -translate-y-1/2 rotate-[-4deg] drop-shadow-xl" style={{ left: drag.x, top: drag.y }}>
        <CardFace card={drag.card} />
      </span> : null}
    </div>
  );
}
