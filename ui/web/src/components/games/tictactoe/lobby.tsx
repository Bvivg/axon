"use client";

import Link from "next/link";

import { TicTacToeIcon } from "@/components/games/catalogue";
import { StartGameDialog } from "@/components/games/tictactoe/start-dialog";
import { Button } from "@/components/ui/button";
import { PaneHeader } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useSession } from "@/lib/auth/session";
import { fromTimestamp, profileName, shortWhen } from "@/lib/format";
import { formatDelta, gameHref, gameStatusLabel, ratingDelta, useGameSessions, useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";

export function GameLobby({ quick = false }: { quick?: boolean }) {
  const { user } = useSession();
  const sessions = useGameSessions();
  const opponents = usePublicProfiles(sessions.data?.flatMap(game => game.playerIds.filter(id => id !== user?.id)) ?? []).data;
  const mine = useRatings("tictactoe", user?.id ? [user.id] : []).data?.get(user?.id ?? "");
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader back="/games" backLabel="Back to games" leading={<TicTacToeIcon className="size-9" />} title="Tic-Tac-Toe" subtitle="2 players" />
      <ScrollArea className="min-h-0 flex-1">
        <main className="mx-auto w-full max-w-[720px] space-y-4 px-4 py-5 sm:px-6">
          <div className="flex flex-wrap items-center gap-3 rounded-[14px] border border-line bg-surface p-4">
            <div className="min-w-0 flex-1">
              <p className="text-xs text-muted-foreground">Rating</p>
              <p className="font-mono text-xl font-semibold" data-testid="my-rating">{mine?.rating ?? 1200}</p>
              <p className="text-xs text-muted-foreground">{mine ? `${mine.games} rated · ${mine.wins}W ${mine.losses}L ${mine.draws}D` : "No rated games yet"}</p>
            </div>
            <StartGameDialog rating={mine?.rating} autoQuick={quick} />
          </div>
          <section id="game-history" aria-labelledby="your-games-title" className="rounded-[14px] border border-line bg-surface p-4">
            <h2 id="your-games-title" className="flex items-center justify-between pb-2 text-sm font-semibold">
              Your games<span className="text-xs font-normal text-muted-foreground">{sessions.data?.length ?? "—"}</span>
            </h2>
            {sessions.isPending ? <p role="status" className="text-sm text-muted-foreground">Loading games…</p> : null}
            {sessions.isError ? <div className="flex flex-wrap items-center gap-3"><p role="alert" className="text-sm text-destructive">Could not load games.</p><Button variant="outline" onClick={() => void sessions.refetch()}>Try again</Button></div> : null}
            {sessions.isSuccess && !sessions.data.length ? <p className="py-2 text-sm text-muted-foreground">No games yet</p> : null}
            {sessions.data?.length ? (
              <ul className="-mx-2 flex flex-col">
                {sessions.data.map(game => {
                  const opponent = game.playerIds.find(id => id !== user?.id);
                  const name = opponent ? profileName(opponents?.get(opponent)) : "";
                  const date = fromTimestamp(game.updatedAt);
                  const delta = ratingDelta(game, user?.id);
                  return (
                    <li key={game.id}>
                      <Link href={gameHref(game.id)} className="flex min-h-11 items-center gap-3 rounded-[10px] px-2 py-1.5 text-sm transition-colors hover:bg-muted">
                        <span className="min-w-0 flex-1 truncate">
                          {name ? <>{name}<span className="text-muted-foreground"> · </span></> : null}
                          <span className={name ? "text-muted-foreground" : undefined}>{gameStatusLabel(game.status, game.winnerUserId, user?.id)}</span>
                        </span>
                        {delta !== undefined ? <span className={cn("shrink-0 font-mono text-xs", delta > 0 ? "text-success" : delta < 0 ? "text-destructive" : "text-muted-foreground")}>{formatDelta(delta)}</span> : null}
                        {date ? <time className="shrink-0 text-xs text-muted-foreground" dateTime={date.toISOString()}>{shortWhen(date)}</time> : null}
                      </Link>
                    </li>
                  );
                })}
              </ul>
            ) : null}
          </section>
        </main>
      </ScrollArea>
    </div>
  );
}
