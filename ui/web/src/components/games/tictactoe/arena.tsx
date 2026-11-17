import { ArrowUpRight, Timer, X } from "lucide-react";
import type { ReactNode } from "react";

import { GameMark } from "@/components/games/tictactoe/board";
import { Mark } from "@/gen/axon/game/v1/game_pb";
import { formatClock } from "@/lib/games/tictactoe";
import { cn } from "@/lib/utils";

export const arenaClass = "mx-auto flex w-full max-w-[550px] min-w-0 flex-col text-foreground";
export const actionClass = "inline-flex min-h-9 items-center justify-center gap-2 rounded-lg border border-border bg-card px-3 text-xs font-semibold text-muted-foreground transition-colors hover:border-primary hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:cursor-not-allowed disabled:opacity-35";

export function ArenaHeader({ children }: { children: ReactNode }) {
  return <div className="flex min-h-[52px] items-center justify-between gap-3 pb-3.5">
    <h1 className="flex min-w-0 items-center gap-2 text-sm font-semibold tracking-tight sm:text-[15px]"><span aria-hidden className="grid size-[27px] shrink-0 place-items-center rounded-lg bg-primary/15 text-[17px] text-primary">×○</span><span>Крестики-нолики</span></h1>
    {children}
  </div>;
}

export function ArenaPlayer({ mark, name, active, time }: { mark: Mark; name: string; active: boolean; time?: number }) {
  return <div data-player={mark === Mark.X ? "X" : "O"} data-active={active || undefined} className="flex h-[57px] min-w-0 items-center gap-2.5">
    <span className={cn("grid size-[35px] shrink-0 place-items-center rounded-[9px] bg-muted", mark === Mark.X && "bg-primary/15")}><GameMark mark={mark} className="size-5" /></span>
    <span className="truncate text-sm font-semibold">{name}</span>
    {time !== undefined ? <span aria-label={`${mark === Mark.X ? "Время крестиков" : "Время ноликов"}, ${formatClock(time)}${active ? ", таймер идёт" : ""}`} className={cn("ml-auto flex h-[37px] min-w-[101px] shrink-0 items-center justify-center gap-2 rounded-lg border border-border bg-card px-3 font-mono text-[19px] leading-none font-semibold tracking-[-.06em] text-muted-foreground tabular-nums", active && "border-primary/40 bg-primary/15 text-foreground shadow-[inset_3px_0_0_var(--color-primary)]", time === 0 && "border-destructive text-destructive")}><Timer aria-hidden size={15} /><span>{formatClock(time)}</span></span>
      : active ? <span className="ml-auto size-2 shrink-0 rounded-full bg-primary" aria-label="Current turn" /> : null}
  </div>;
}

export function ArenaResult({ title, kind, close, restart, pending }: { title: string; kind: "win" | "lose" | "draw"; close: () => void; restart: () => void; pending?: boolean }) {
  return <div className="absolute inset-0 z-10 grid place-items-center rounded-[9px] bg-black/35 backdrop-blur-[1.5px] motion-safe:animate-fade">
    <section aria-label={title} className="relative flex w-[252px] max-w-[calc(100%-42px)] flex-col items-center gap-2.5 rounded-[13px] border border-border bg-card px-[18px] pt-6 pb-[18px] text-center shadow-2xl">
      <button type="button" aria-label="Посмотреть поле" title="Посмотреть поле" onClick={close} className="absolute top-1.5 right-2 grid size-[30px] place-items-center rounded-lg text-muted-foreground hover:bg-muted focus-visible:outline-2 focus-visible:outline-primary"><X aria-hidden size={19} /></button>
      <span aria-hidden className={cn("grid size-11 place-items-center rounded-full bg-success/15 text-[27px] font-bold text-success", kind === "lose" && "bg-destructive/15 text-destructive", kind === "draw" && "bg-muted text-muted-foreground")}>{kind === "draw" ? "=" : kind === "lose" ? "×" : "✓"}</span>
      <div className="text-[21px] leading-tight font-bold tracking-tight">{title}</div>
      <button type="button" disabled={pending} onClick={restart} className="mt-1 flex min-h-[41px] w-full items-center justify-center gap-2 rounded-lg bg-primary px-3 text-[13px] font-bold text-primary-foreground hover:brightness-110 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary disabled:opacity-35">{pending ? "Создание…" : "Новая партия"}<ArrowUpRight aria-hidden size={16} /></button>
    </section>
  </div>;
}
