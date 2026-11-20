"use client";

import { Eye, History } from "lucide-react";
import Link from "next/link";

import { buttonVariants } from "@/components/ui/button";
import { SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { replayHref } from "@/lib/games/catalog";
import { cn } from "@/lib/utils";

export function isSpectator(game: Session, userId?: string) {
  return Boolean(userId) && game.playerIds.length === 2 && !game.playerIds.includes(userId ?? "");
}

export function spectatorStatus(game: Session, names: string[], sides: string[]) {
  if (game.status === SessionStatus.DRAW) return "Draw";
  if (game.status === SessionStatus.WON) {
    const seat = game.playerIds.indexOf(game.winnerUserId ?? "");
    return `${names[seat] || sides[seat] || "Player"} won`;
  }
  const seat = game.playerIds.indexOf(game.turnUserId ?? "");
  return seat < 0 ? "Waiting" : `${sides[seat]} to move`;
}

export function WatchingTag() {
  return <span data-watching className="inline-flex min-h-11 items-center gap-1.5 px-2 text-[13px] text-game-text2"><Eye aria-hidden className="size-4" />Watching</span>;
}

export function SpectatorCount({ count }: { count?: number }) {
  if (!count) return null;
  return <p data-spectators={count} className="flex items-center gap-1.5 text-sm text-muted-foreground"><Eye aria-hidden className="size-4" />{count} watching</p>;
}

export function SpectatorStatus({ text }: { text: string }) {
  return <p data-spectator-status className="rounded-game-md border border-game-border bg-game-card px-3 py-2 text-sm font-medium">{text}</p>;
}

export function ReplayLink({ game }: { game: Session }) {
  return <Link href={replayHref(game.id, game.game)} className={cn(buttonVariants({ variant: "outline" }), "min-h-11 w-full")}><History />Replay</Link>;
}
