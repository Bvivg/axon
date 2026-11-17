"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ArrowUpRight, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useRef } from "react";

import { Button, buttonVariants } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { SessionStatus } from "@/gen/axon/game/v1/game_pb";
import { useSession } from "@/lib/auth/session";
import { gameClient } from "@/lib/connect/clients";
import { fromTimestamp, profileName, shortWhen } from "@/lib/format";
import { gameHref, sessionKey, sessionsKey } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";

function label(status: SessionStatus, winner?: string, user?: string) {
  switch (status) {
    case SessionStatus.WAITING: return "Waiting for opponent";
    case SessionStatus.PLAYING: return "In progress";
    case SessionStatus.DRAW: return "Draw";
    case SessionStatus.WON: return winner === user ? "You won" : "You lost";
    default: return "Unavailable";
  }
}

export function GameLobby() {
  const router = useRouter();
  const client = useQueryClient();
  const { user } = useSession();
  const requestID = useRef<string | undefined>(undefined);
  const sessions = useQuery({
    queryKey: sessionsKey,
    queryFn: async ({ signal }) => (await gameClient.listSessions({}, { signal })).sessions,
    refetchOnMount: "always",
  });
  const opponents = usePublicProfiles(sessions.data?.flatMap(game => game.playerIds.filter(id => id !== user?.id)) ?? []).data;
  const create = useMutation({
    mutationFn: async () => {
      requestID.current ??= crypto.randomUUID();
      const { session } = await gameClient.createSession({ game: "tictactoe", requestId: requestID.current });
      if (!session) throw new Error("Could not create the game.");
      return session;
    },
    onSuccess: session => {
      requestID.current = undefined;
      client.setQueryData(sessionKey(session.id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
      router.push(gameHref(session.id));
    },
  });

  return (
    <ScrollArea className="min-h-0 flex-1">
      <main className="workspace-page game-lobby">
      <Link href="/games" className="game-back-link"><ArrowLeft aria-hidden size={17} />Games</Link>
      <section aria-labelledby="games-hero" className="space-y-3 py-3">
        <h1 id="games-hero" className="text-lg font-semibold tracking-tight">Tic-Tac-Toe</h1>
        <div className="flex flex-wrap items-center gap-2">
          <Button variant="signal" className="min-h-11" disabled={create.isPending} onClick={() => create.mutate()}>
            <Plus />{create.isPending ? "Creating game…" : "Create game"}
          </Button>
          <Link href="/games/tic-tac-toe/practice" className={cn(buttonVariants({ variant: "outline" }), "min-h-11")}>Компьютер / Игра вдвоём</Link>
        </div>
        {create.isError ? <p role="alert" className="text-sm text-destructive">Could not create the game. Try again.</p> : null}
      </section>
        <section id="game-history" aria-labelledby="your-games-title" className="space-y-3 pt-2">
          <h2 id="your-games-title" className="game-section-title">Your games<span>{sessions.data?.length ?? "—"}</span></h2>
          {sessions.isPending ? <p role="status" className="text-sm text-muted-foreground">Loading games…</p> : null}
          {sessions.isError ? <div className="flex flex-wrap items-center gap-3"><p role="alert" className="text-sm text-destructive">Could not load games.</p><Button variant="outline" className="min-h-11" onClick={() => void sessions.refetch()}>Try again</Button></div> : null}
          {sessions.data?.length ? (
            <ul className="divide-y divide-border">
              {sessions.data.map(game => {
                const opponent = game.playerIds.find(id => id !== user?.id);
                const name = opponent ? profileName(opponents?.get(opponent)) : "";
                const date = fromTimestamp(game.updatedAt);
                return (
                  <li key={game.id}>
                    <Link href={gameHref(game.id)} className={cn(buttonVariants({ variant: "ghost" }), "h-auto min-h-14 w-full justify-between gap-3 rounded-none px-4 py-2")}>
                      <span className="min-w-0 whitespace-normal [overflow-wrap:anywhere]">
                        Tic-Tac-Toe <span className="ml-1.5 text-muted-foreground">· {label(game.status, game.winnerUserId, user?.id)}</span>
                        {name || date ? (
                          <span className="block text-xs text-muted-foreground">
                            {name}{name && date ? " · " : null}
                            {date ? <time dateTime={date.toISOString()}>{shortWhen(date)}</time> : null}
                          </span>
                        ) : null}
                      </span>
                      <ArrowUpRight aria-hidden className="shrink-0" />
                    </Link>
                  </li>
                );
              })}
            </ul>
          ) : null}
        </section>
      </main>
    </ScrollArea>
  );
}
