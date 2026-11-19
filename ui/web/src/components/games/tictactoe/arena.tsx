"use client";

import { Dialog } from "@base-ui/react/dialog";
import { ArrowLeft, Bot, Check, Minus, Plus, SlidersHorizontal, Undo2, X } from "lucide-react";
import Image from "next/image";
import Link from "next/link";
import { type ReactNode } from "react";

import { GameMark } from "@/components/games/tictactoe/board";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Mark } from "@/gen/axon/game/v1/game_pb";
import { formatClock } from "@/lib/games/tictactoe";
import { cn } from "@/lib/utils";


export const arenaClass = "@container mx-auto flex w-full max-w-[380px] min-w-0 flex-col gap-3 font-sans text-[14px] leading-[1.45] text-game-text";
export const actionClass = "inline-flex min-h-[44px] items-center justify-center gap-2 rounded-game-md border border-game-border bg-game-card px-3 text-[14px] font-medium text-game-text transition-colors duration-200 motion-reduce:transition-none hover:bg-game-bg2 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-game-primary disabled:cursor-default disabled:opacity-40";
export const iconClass = "grid size-[44px] shrink-0 place-items-center rounded-game-sm text-game-text2 transition-colors duration-200 motion-reduce:transition-none hover:bg-game-bg2 hover:text-game-text focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-game-primary disabled:cursor-default disabled:opacity-40 [&_svg]:size-[18px] [&_svg]:stroke-[1.75]";

export function ArenaHeader({ children }: { children: ReactNode }) {
  return <header className="flex min-h-11 items-center justify-between gap-1">
    <div className="flex min-w-0 items-center gap-1"><Link href="/games/tic-tac-toe" aria-label="Back to lobby" title="Back to lobby" className={iconClass}><ArrowLeft aria-hidden /></Link><h1 className="min-w-0 truncate text-[14px] font-semibold tracking-[-.01em] @max-[350px]:sr-only">Tic-Tac-Toe</h1></div>
    {children}
  </header>;
}

export function ArenaTools({ restart, undo, canUndo, children, pending = false, undoHidden = false }: { restart: () => void; undo: () => void; canUndo: boolean; children: ReactNode; pending?: boolean; undoHidden?: boolean }) {
  return <div className="flex shrink-0 items-center">
    <button type="button" aria-label="New game" title="New game (N)" disabled={pending} className={iconClass} onClick={restart}><Plus aria-hidden /></button>
    {undoHidden ? null : <button type="button" aria-label="Undo move" title="Undo move (Z)" className={iconClass} disabled={!canUndo} onClick={undo}><Undo2 aria-hidden /></button>}
    <span aria-hidden className="mx-1 h-4 w-px bg-game-border" />
    {children}
  </div>;
}

function profileHash(name: string) {
  let hash = 2166136261;
  for (const character of name) hash = Math.imul(hash ^ character.charCodeAt(0), 16777619);
  return hash >>> 0;
}

function Identicon({ name, hue }: { name: string; hue: number }) {
  const hash = profileHash(name);
  const pixels = [];
  for (let x = 0; x < 3; x++) for (let y = 0; y < 5; y++) if ((hash >>> (x * 5 + y)) & 1) {
    pixels.push(<rect key={`${x}-${y}`} x={1.5 + x} y={1.5 + y} width="1" height="1" />);
    if (x < 2) pixels.push(<rect key={`${4 - x}-${y}`} x={5.5 - x} y={1.5 + y} width="1" height="1" />);
  }
  return <svg aria-hidden viewBox="0 0 8 8" className="size-full" style={{ fill: `light-dark(hsl(${hue} 42% 46%), hsl(${hue} 70% 76%))` }}>{pixels}</svg>;
}

export function ArenaPlayer({ mark, name, active, time, rating, difficulty, avatar = "", bot = false }: { mark: Mark; name: string; active: boolean; time?: number; rating?: number; difficulty?: string; avatar?: string; bot?: boolean }) {
  const hue = 235 + (profileHash(name) >>> 16) % 80;
  const symbol = mark === Mark.X ? "X" : "O";
  const low = time !== undefined && time <= 10000 && (active || time === 0);
  return <div data-player={symbol} data-active={active || undefined} className="flex h-[60px] min-w-0 items-center gap-3 rounded-game-md border border-game-border bg-game-card px-3">
    <span className="relative size-[38px] shrink-0"><span className={cn("grid size-full place-items-center overflow-hidden rounded-full bg-game-accent text-game-text2", bot && "bg-game-bg2")} style={!bot && !avatar ? { backgroundColor: `light-dark(hsl(${hue} 45% 91%), hsl(${hue} 28% 24%))` } : undefined}>
      {avatar ? <Image src={avatar} alt="" width={38} height={38} unoptimized className="size-full object-cover" /> : bot ? <Bot aria-hidden size={20} strokeWidth={1.75} /> : <Identicon name={name} hue={hue} />}
    </span><span className="absolute -right-1 -bottom-1 grid size-[18px] place-items-center rounded-full border border-game-border bg-game-card"><GameMark mark={mark} className="size-[11px]" /></span></span>
    <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2"><b className="min-w-0 truncate text-[15px] font-medium">{name}</b>{difficulty || rating !== undefined ? <span className={cn("text-[13px] text-game-text2", rating !== undefined && "font-mono")}>{difficulty ?? rating}</span> : null}</span>
    {time !== undefined ? <span data-clock data-low={low || undefined} aria-label={`${symbol === "X" ? "X clock" : "O clock"}, ${formatClock(time)}${active ? ", running" : ""}`} className={cn("grid h-[38px] min-w-[84px] shrink-0 place-items-center rounded-game-sm bg-game-bg2 px-3 font-mono text-[18px] leading-none font-medium text-game-text2 tabular-nums transition-colors duration-200 motion-reduce:transition-none @max-[320px]:min-w-[72px] @max-[320px]:text-[16px]", active && "bg-game-primary text-game-on-primary", low && "bg-game-error text-game-on-primary")}>{formatClock(time).replace(/^0/, "")}</span> : null}
  </div>;
}

export function ArenaDialog({ title, open, onOpenChange, children, footer, hideTitle = false }: { title: string; open: boolean; onOpenChange: (open: boolean) => void; children: ReactNode; footer?: ReactNode; hideTitle?: boolean }) {
  return <Dialog.Root open={open} onOpenChange={onOpenChange}>
    <Dialog.Portal>
      <Dialog.Backdrop className="fixed inset-0 z-50 bg-game-scrim backdrop-blur-[2px] transition-opacity duration-200 motion-reduce:transition-none data-[ending-style]:opacity-0 data-[starting-style]:opacity-0" />
      <Dialog.Popup aria-describedby={undefined} className="fixed top-1/2 left-1/2 z-50 flex max-h-[calc(100dvh-32px)] w-[min(360px,calc(100vw-32px))] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-game-lg border border-game-border bg-game-card font-sans text-[14px] leading-[1.45] text-game-text shadow-game-dialog outline-none transition-[opacity,scale] duration-200 motion-reduce:transition-none data-[ending-style]:opacity-0 data-[starting-style]:scale-[.98] data-[starting-style]:opacity-0">
        <div className={hideTitle ? "absolute top-2 right-2 z-10" : "flex items-center justify-between pt-2 pr-2 pl-5"}><Dialog.Title className={cn("text-[15px] font-semibold tracking-[-.01em]", hideTitle && "sr-only")}>{title}</Dialog.Title><Dialog.Close aria-label="Close" className={iconClass}><X aria-hidden /></Dialog.Close></div>
        <ScrollArea className="min-h-0"><div className="grid gap-4 px-5 pt-2 pb-5">{children}</div></ScrollArea>
        {footer ? <div className={cn("flex flex-wrap justify-end gap-2 border-t border-game-border bg-game-sidebar px-5 py-3", hideTitle && "max-[359px]:flex-col")}>{footer}</div> : null}
      </Dialog.Popup>
    </Dialog.Portal>
  </Dialog.Root>;
}

export function ArenaResult({ title, detail, kind, close, restart, pending }: { title: string; detail?: string; kind: "win" | "lose" | "draw"; close: () => void; restart: () => void; pending?: boolean }) {
  const Icon = kind === "draw" ? Minus : kind === "lose" ? X : Check;
  return <ArenaDialog title={title} hideTitle open onOpenChange={open => { if (!open) close(); }} footer={<><button type="button" className={actionClass} onClick={close}>View board</button><button type="button" disabled={pending} onClick={restart} className={cn(actionClass, "border-game-primary bg-game-primary text-game-on-primary hover:bg-game-primary hover:brightness-110")}>{pending ? "Creating…" : "Play again"}</button></>}>
    <div className="grid justify-items-center gap-3 pt-5 text-center"><span aria-hidden className={cn("grid size-12 place-items-center rounded-full bg-game-success/15 text-game-success", kind === "lose" && "bg-game-error/15 text-game-error", kind === "draw" && "bg-game-warning/15 text-game-warning")}><Icon size={24} /></span><span className="text-[18px] font-semibold">{title}</span>{detail ? <span data-rating-delta className="font-mono text-[14px] text-game-text2">{detail}</span> : null}</div>
  </ArenaDialog>;
}

export function SettingsIcon({ onClick }: { onClick: () => void }) {
  return <button type="button" aria-label="Settings" title="Settings" className={iconClass} onClick={onClick}><SlidersHorizontal aria-hidden /></button>;
}
