"use client";

import { Circle, Gamepad2, X } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useState } from "react";

import { Avatar } from "@/components/ui/avatar";
import { buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ListColumnHeader } from "@/components/ui/list-column";
import { ListBadge, ListItem, ListLabel } from "@/components/ui/list-item";
import { ListSearchField } from "@/components/ui/list-search-field";
import { PaneHeader } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { SessionStatus } from "@/gen/axon/game/v1/game_pb";
import { useSession } from "@/lib/auth/session";
import { fromTimestamp, initials, profileName, shortWhen } from "@/lib/format";
import { gameHref, gameStatusLabel, useGameSessions, useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";

export const ticTacToeLobbyHref = "/games/tic-tac-toe";

export function TicTacToeIcon({ className = "size-8" }: { className?: string }) {
  return (
    <span aria-hidden className={`flex items-center justify-center gap-px rounded-[10px] bg-accent text-signal ${className}`}>
      <X className="size-3.5" strokeWidth={2.75} />
      <Circle className="size-2.5 text-success" strokeWidth={3} />
    </span>
  );
}

export function GamesSidebar() {
  const [query, setQuery] = useState("");
  const pathname = usePathname();
  const { user } = useSession();
  const rating = useRatings("tictactoe", user?.id ? [user.id] : []).data?.get(user?.id ?? "")?.rating;
  const sessions = useGameSessions();
  const active = (sessions.data ?? []).filter((game) => game.status === SessionStatus.WAITING || game.status === SessionStatus.PLAYING);
  const opponents = usePublicProfiles(active.flatMap((game) => game.playerIds.filter((id) => id !== user?.id))).data;
  const needle = query.trim().toLowerCase();
  const matches = "tic-tac-toe".includes(needle);

  return (
    <>
      <ListColumnHeader title="Games" />
      <ListSearchField label="Find a game" value={query} onChange={setQuery} />
      <ScrollArea className="min-h-0 flex-1">
        <ListLabel>Catalogue</ListLabel>
        <nav aria-label="Game catalogue" className="flex flex-col gap-0.5">
          {matches ? (
            <ListItem
              href={ticTacToeLobbyHref}
              aria-label="Tic-Tac-Toe lobby"
              active={pathname === ticTacToeLobbyHref || pathname.startsWith(`${ticTacToeLobbyHref}/practice`)}
              leading={<TicTacToeIcon />}
              title="Tic-Tac-Toe"
              subtitle={`2 players · Rating ${rating ?? 1200}`}
            />
          ) : (
            <p className="px-4 py-2 text-xs text-muted-foreground">No games found</p>
          )}
        </nav>
        {active.length && !needle ? (
          <>
            <ListLabel>In progress</ListLabel>
            <nav aria-label="Games in progress" className="flex flex-col gap-0.5 pb-2">
              {active.map((game) => {
                const opponent = game.playerIds.find((id) => id !== user?.id);
                const profile = opponent ? opponents?.get(opponent) : undefined;
                const name = profileName(profile) || "Tic-Tac-Toe";
                const date = fromTimestamp(game.updatedAt);
                const myTurn = game.status === SessionStatus.PLAYING && game.turnUserId === user?.id;
                return (
                  <ListItem
                    key={game.id}
                    href={gameHref(game.id)}
                    active={pathname === gameHref(game.id)}
                    leading={profile ? <Avatar src={profile.avatarUrls?.small || profile.avatarUrl} alt="" fallback={initials(name)} sizes="32px" className="size-8 rounded-[10px] text-[11px]" /> : <TicTacToeIcon />}
                    title={name}
                    meta={date ? <time dateTime={date.toISOString()}>{shortWhen(date)}</time> : null}
                    subtitle={<span className={myTurn ? "truncate text-signal" : "truncate"}>{myTurn ? "Your move" : gameStatusLabel(game.status, game.winnerUserId, user?.id)}</span>}
                    trailing={myTurn ? <ListBadge>1</ListBadge> : null}
                  />
                );
              })}
            </nav>
          </>
        ) : null}
      </ScrollArea>
    </>
  );
}

export function GamesCatalogue() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PaneHeader title="Games" />
      <div className="grid flex-1 place-items-center">
        <EmptyState icon={Gamepad2} title="Pick a game to play">
          <Link href={ticTacToeLobbyHref} className={buttonVariants({ variant: "signal", className: "min-h-10" })}>Open Tic-Tac-Toe</Link>
        </EmptyState>
      </div>
    </div>
  );
}
