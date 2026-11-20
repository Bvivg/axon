"use client";

import { BrickWall, ChessKnight, Circle, Gamepad2, X } from "lucide-react";
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
import { GameKey, gameKey, gameKeys, games, lobbyHref } from "@/lib/games/catalog";
import { gameHref, gameStatusLabel, useGameSessions, useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";

export const ticTacToeLobbyHref = lobbyHref(GameKey.TicTacToe);

export function TicTacToeIcon({ className = "size-8" }: { className?: string }) {
  return (
    <span aria-hidden className={`flex items-center justify-center gap-px rounded-[10px] bg-accent text-signal ${className}`}>
      <X className="size-3.5" strokeWidth={2.75} />
      <Circle className="size-2.5 text-success" strokeWidth={3} />
    </span>
  );
}

export function ChessIcon({ className = "size-8" }: { className?: string }) {
  return (
    <span aria-hidden className={`flex items-center justify-center rounded-[10px] bg-accent text-signal ${className}`}>
      <ChessKnight className="size-[55%]" strokeWidth={2} />
    </span>
  );
}

export function QuoridorIcon({ className = "size-8" }: { className?: string }) {
  return (
    <span aria-hidden className={`flex items-center justify-center rounded-[10px] bg-accent text-signal ${className}`}>
      <BrickWall className="size-[55%]" strokeWidth={2} />
    </span>
  );
}

export function GameIcon({ game, className }: { game: GameKey; className?: string }) {
  if (game === GameKey.Chess) return <ChessIcon className={className} />;
  if (game === GameKey.Quoridor) return <QuoridorIcon className={className} />;
  return <TicTacToeIcon className={className} />;
}

function CatalogueItem({ game, pathname, rating }: { game: GameKey; pathname: string; rating?: number }) {
  const href = lobbyHref(game);
  return (
    <ListItem
      href={href}
      aria-label={`${games[game].title} lobby`}
      active={pathname === href || pathname.startsWith(`${href}/practice`)}
      leading={<GameIcon game={game} />}
      title={games[game].title}
      subtitle={`2 players · Rating ${rating ?? 1200}`}
    />
  );
}

export function GamesSidebar() {
  const [query, setQuery] = useState("");
  const pathname = usePathname();
  const { user } = useSession();
  const me = user?.id ? [user.id] : [];
  const ticTacToeRating = useRatings(GameKey.TicTacToe, me).data?.get(user?.id ?? "")?.rating;
  const chessRating = useRatings(GameKey.Chess, me).data?.get(user?.id ?? "")?.rating;
  const quoridorRating = useRatings(GameKey.Quoridor, me).data?.get(user?.id ?? "")?.rating;
  const ratings: Record<GameKey, number | undefined> = { [GameKey.TicTacToe]: ticTacToeRating, [GameKey.Chess]: chessRating, [GameKey.Quoridor]: quoridorRating };
  const sessions = useGameSessions();
  const active = (sessions.data ?? []).filter((game) => game.status === SessionStatus.WAITING || game.status === SessionStatus.PLAYING);
  const opponents = usePublicProfiles(active.flatMap((game) => game.playerIds.filter((id) => id !== user?.id))).data;
  const needle = query.trim().toLowerCase();
  const listed = gameKeys.filter(game => games[game].title.toLowerCase().includes(needle));

  return (
    <>
      <ListColumnHeader title="Games" />
      <ListSearchField label="Find a game" value={query} onChange={setQuery} />
      <ScrollArea className="min-h-0 flex-1">
        <ListLabel>Catalogue</ListLabel>
        <nav aria-label="Game catalogue" className="flex flex-col gap-0.5">
          {listed.length ? listed.map(game => <CatalogueItem key={game} game={game} pathname={pathname} rating={ratings[game]} />) : (
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
                const kind = gameKey(game.game);
                const name = profileName(profile) || games[kind].title;
                const date = fromTimestamp(game.updatedAt);
                const myTurn = game.status === SessionStatus.PLAYING && game.turnUserId === user?.id;
                return (
                  <ListItem
                    key={game.id}
                    href={gameHref(game.id, game.game)}
                    active={pathname === gameHref(game.id, game.game)}
                    leading={profile ? <Avatar src={profile.avatarUrls?.small || profile.avatarUrl} alt="" fallback={initials(name)} sizes="32px" className="size-8 rounded-[10px] text-[11px]" /> : <GameIcon game={kind} />}
                    title={name}
                    meta={date ? <time dateTime={date.toISOString()}>{shortWhen(date)}</time> : null}
                    subtitle={<span className={myTurn ? "truncate text-signal" : "truncate"}>{games[kind].title} · {myTurn ? "Your move" : gameStatusLabel(game.status, game.winnerUserId, user?.id)}</span>}
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
          <div className="flex flex-wrap justify-center gap-2">
            <Link href={lobbyHref(GameKey.Chess)} className={buttonVariants({ variant: "signal", className: "min-h-10" })}>Open Chess</Link>
            <Link href={lobbyHref(GameKey.Quoridor)} className={buttonVariants({ variant: "outline", className: "min-h-10" })}>Open Quoridor</Link>
            <Link href={ticTacToeLobbyHref} className={buttonVariants({ variant: "outline", className: "min-h-10" })}>Open Tic-Tac-Toe</Link>
          </div>
        </EmptyState>
      </div>
    </div>
  );
}
