"use client";

import { create } from "@bufbuild/protobuf";

import { DurakMode, DurakOptionsSchema, DurakThrowIn, GameOptionsSchema, type GameOptions } from "@/gen/axon/game/v1/game_pb";
import { cn } from "@/lib/utils";

export type DurakSettings = {
  seats: number;
  mode: DurakMode;
  deck: number;
  throwIn: DurakThrowIn;
  cheats: boolean;
  draw: boolean;
  firstBoutFive: boolean;
  bots: number;
};

export const defaultDurakSettings: DurakSettings = {
  seats: 2,
  mode: DurakMode.THROW_IN,
  deck: 36,
  throwIn: DurakThrowIn.ALL,
  cheats: false,
  draw: true,
  firstBoutFive: false,
  bots: 0,
};

const decks = [24, 36, 52];
const seatChoices = [2, 3, 4, 5, 6];

export function deckFits(seats: number, deck: number): boolean {
  return seats * 6 <= deck;
}

export function durakOptions(settings: DurakSettings): GameOptions {
  return create(GameOptionsSchema, {
    game: {
      case: "durak",
      value: create(DurakOptionsSchema, {
        mode: settings.mode,
        cheats: settings.cheats,
        deck: settings.deck,
        throwIn: settings.throwIn,
        draw: settings.draw,
        firstBoutLimit: settings.firstBoutFive ? 5 : 6,
      }),
    },
  });
}

export function describeDurak(settings: Pick<DurakSettings, "seats" | "mode" | "cheats" | "deck">): string {
  return [
    `${settings.seats} players`,
    settings.mode === DurakMode.TRANSFER ? "transfer" : "throw-in",
    `${settings.deck} cards`,
    settings.cheats ? "cheating" : "",
  ].filter(Boolean).join(" · ");
}

function Segmented<T extends string | number>({ label, value, choices, onChange, disabled }: {
  label: string;
  value: T;
  choices: { value: T; label: string }[];
  onChange: (value: T) => void;
  disabled?: (value: T) => boolean;
}) {
  return (
    <div role="group" aria-label={label} className="flex items-center justify-between gap-3">
      <span className="text-sm text-muted-foreground">{label}</span>
      <div className="flex rounded-[10px] border border-line bg-surface p-0.5">
        {choices.map(choice => (
          <button key={choice.value} type="button" aria-pressed={value === choice.value} disabled={disabled?.(choice.value)}
            onClick={() => onChange(choice.value)}
            className={cn("min-h-8 min-w-9 rounded-[8px] px-2.5 text-sm font-medium transition-colors disabled:opacity-35",
              value === choice.value ? "bg-signal text-signal-foreground" : "text-muted-foreground hover:text-foreground")}>
            {choice.label}
          </button>
        ))}
      </div>
    </div>
  );
}

function Toggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (checked: boolean) => void }) {
  return (
    <label className="flex min-h-9 cursor-pointer items-center justify-between gap-3 text-sm">
      <span className="text-muted-foreground">{label}</span>
      <input type="checkbox" checked={checked} onChange={event => onChange(event.target.checked)} className="size-4 accent-[var(--color-signal)]" />
    </label>
  );
}

export function DurakTableSettings({ value, onChange, withBots }: { value: DurakSettings; onChange: (next: DurakSettings) => void; withBots: boolean }) {
  const set = (patch: Partial<DurakSettings>) => {
    const next = { ...value, ...patch };
    if (!deckFits(next.seats, next.deck)) next.deck = decks.find(deck => deckFits(next.seats, deck)) ?? 52;
    next.bots = Math.min(next.bots, next.seats - 1);
    onChange(next);
  };
  return (
    <fieldset className="space-y-2 rounded-[14px] border border-line bg-surface p-3">
      <legend className="px-1 text-xs font-semibold text-muted-foreground">Table</legend>
      <Segmented label="Players" value={value.seats} choices={seatChoices.map(seats => ({ value: seats, label: String(seats) }))} onChange={seats => set({ seats })} />
      <Segmented label="Mode" value={value.mode} choices={[{ value: DurakMode.THROW_IN, label: "Throw-in" }, { value: DurakMode.TRANSFER, label: "Transfer" }]} onChange={mode => set({ mode })} />
      <Segmented label="Deck" value={value.deck} choices={decks.map(deck => ({ value: deck, label: String(deck) }))} onChange={deck => set({ deck })} disabled={deck => !deckFits(value.seats, deck)} />
      {value.seats > 2 ? (
        <Segmented label="Who throws in" value={value.throwIn} choices={[{ value: DurakThrowIn.ALL, label: "Everyone" }, { value: DurakThrowIn.NEIGHBORS, label: "Neighbours" }]} onChange={throwIn => set({ throwIn })} />
      ) : null}
      <Toggle label="Cheating allowed (unrated)" checked={value.cheats} onChange={cheats => set({ cheats })} />
      <Toggle label="Draws" checked={value.draw} onChange={draw => set({ draw })} />
      <Toggle label="First bout: 5 cards" checked={value.firstBoutFive} onChange={firstBoutFive => set({ firstBoutFive })} />
      {withBots ? (
        <Segmented label="Bots" value={value.bots} choices={Array.from({ length: value.seats }, (_, bots) => ({ value: bots, label: String(bots) }))} onChange={bots => set({ bots })} />
      ) : null}
    </fieldset>
  );
}
