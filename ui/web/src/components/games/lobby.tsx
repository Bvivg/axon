"use client";

import Link from "next/link";

import { GameIcon } from "@/components/games/catalogue";
import { durakSettings } from "@/components/games/durak/match";
import { describeDurak } from "@/components/games/durak/table-settings";
import { StartGameDialog } from "@/components/games/start-dialog";
import { Button } from "@/components/ui/button";
import { PaneHeader } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { useSession } from "@/lib/auth/session";
import { fromTimestamp, profileName, shortWhen } from "@/lib/format";
import { GameKey, games, playersLabel } from "@/lib/games/catalog";
import { formatDelta, gameHref, gameStatusLabel, ratingDelta, useGameSessions, useLiveSessions, useOpenTables, useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import type { Session } from "@/gen/axon/game/v1/game_pb";
import { cn } from "@/lib/utils";

export function GameLobby({ game: kind, quick = false }: { game: GameKey; quick?: boolean }) {
  const { user } = useSession();
  const sessions = useGameSessions();
  const list = sessions.data?.filter(item => item.game === kind);
  const live = useLiveSessions(kind).data?.filter(item => !item.playerIds.includes(user?.id ?? ""));
  const tables = useOpenTables(kind, kind === GameKey.Durak).data;
  const humans = (game: Session) => game.playerIds.filter((_, seat) => !game.seating[seat]?.bot);
  const opponents = usePublicProfiles([...(list?.flatMap(game => humans(game).filter(id => id !== user?.id)) ?? []), ...(live?.flatMap(humans) ?? []), ...(tables?.flatMap(humans) ?? [])]).data;
  const nameOf = (game: Session, seat: number) => game.seating[seat]?.bot ? "Bot" : profileName(opponents?.get(game.playerIds[seat] ?? "")) || "Player";
  const mine = useRatings(kind, user?.id ? [user.id] : []).data?.get(user?.id ?? "");
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader back="/games" backLabel="Back to games" leading={<GameIcon game={kind} className="size-9" />} title={games[kind].title} subtitle={playersLabel(kind)} />
      <ScrollArea className="min-h-0 flex-1">
        <main className="mx-auto w-full max-w-[720px] space-y-4 px-4 py-5 sm:px-6">
          <div className="flex flex-wrap items-center gap-3 rounded-[14px] border border-line bg-surface p-4">
            <div className="min-w-0 flex-1">
              <p className="text-xs text-muted-foreground">Rating</p>
              <p className="font-mono text-xl font-semibold" data-testid="my-rating">{mine?.rating ?? 1200}</p>
              <p className="text-xs text-muted-foreground">{mine ? `${mine.games} rated · ${mine.wins}W ${mine.losses}L ${mine.draws}D` : "No rated games yet"}</p>
            </div>
            <StartGameDialog game={kind} rating={mine?.rating} autoQuick={quick} />
          </div>
          {tables?.length ? (
            <section aria-labelledby="open-tables-title" className="rounded-[14px] border border-line bg-surface p-4">
              <h2 id="open-tables-title" className="flex items-center justify-between pb-2 text-sm font-semibold">
                Open tables<span className="text-xs font-normal text-muted-foreground">{tables.length}</span>
              </h2>
              <ul className="-mx-2 flex flex-col">
                {tables.map(game => {
                  const settings = durakSettings(game);
                  const capacity = Math.max(game.seating.length, settings.seats);
                  const mine = game.playerIds.includes(user?.id ?? "");
                  return (
                    <li key={game.id}>
                      <Link href={gameHref(game.id, game.game)} data-open-table={game.id} className="flex min-h-11 items-center gap-3 rounded-[10px] px-2 py-1.5 text-sm transition-colors hover:bg-muted">
                        <span className="min-w-0 flex-1 truncate">{nameOf(game, 0)}<span className="text-muted-foreground">{` · ${describeDurak({ ...settings, seats: capacity })}`}</span></span>
                        <span className="shrink-0 font-mono text-xs text-muted-foreground">{`${game.playerIds.length}/${capacity}`}</span>
                        <span className="shrink-0 text-xs font-medium text-signal">{mine ? "Open" : "Join"}</span>
                      </Link>
                    </li>
                  );
                })}
              </ul>
            </section>
          ) : null}
          {live?.length ? (
            <section aria-labelledby="live-games-title" className="rounded-[14px] border border-line bg-surface p-4">
              <h2 id="live-games-title" className="flex items-center justify-between pb-2 text-sm font-semibold">
                Live games<span className="text-xs font-normal text-muted-foreground">{live.length}</span>
              </h2>
              <ul className="-mx-2 flex flex-col">
                {live.map(game => (
                  <li key={game.id}>
                    <Link href={gameHref(game.id, game.game)} data-live-game={game.id} className="flex min-h-11 items-center gap-3 rounded-[10px] px-2 py-1.5 text-sm transition-colors hover:bg-muted">
                      <span className="min-w-0 flex-1 truncate">{game.playerIds.map((_, seat) => nameOf(game, seat)).join(game.playerIds.length > 2 ? " · " : " vs ")}</span>
                      <span className="shrink-0 text-xs text-muted-foreground">Move {game.ply}</span>
                      <span className="shrink-0 text-xs font-medium text-signal">Watch</span>
                    </Link>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
          <section id="game-history" aria-labelledby="your-games-title" className="rounded-[14px] border border-line bg-surface p-4">
            <h2 id="your-games-title" className="flex items-center justify-between pb-2 text-sm font-semibold">
              Your games<span className="text-xs font-normal text-muted-foreground">{list?.length ?? "—"}</span>
            </h2>
            {sessions.isPending ? <p role="status" className="text-sm text-muted-foreground">Loading games…</p> : null}
            {sessions.isError ? <div className="flex flex-wrap items-center gap-3"><p role="alert" className="text-sm text-destructive">Could not load games.</p><Button variant="outline" onClick={() => void sessions.refetch()}>Try again</Button></div> : null}
            {sessions.isSuccess && !list?.length ? <p className="py-2 text-sm text-muted-foreground">No games yet</p> : null}
            {list?.length ? (
              <ul className="-mx-2 flex flex-col">
                {list.map(game => {
                  const others = game.playerIds.flatMap((id, seat) => id === user?.id ? [] : [nameOf(game, seat)]);
                  const name = others.length > 1 ? `${others.length + 1} players` : others[0] && others[0] !== "Player" ? others[0] : "";
                  const date = fromTimestamp(game.updatedAt);
                  const delta = ratingDelta(game, user?.id);
                  return (
                    <li key={game.id}>
                      <Link href={gameHref(game.id, game.game)} className="flex min-h-11 items-center gap-3 rounded-[10px] px-2 py-1.5 text-sm transition-colors hover:bg-muted">
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
