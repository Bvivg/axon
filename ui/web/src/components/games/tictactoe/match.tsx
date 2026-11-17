"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Copy, Plus, Undo2, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { ticTacToeLobbyHref } from "@/components/games/catalogue";
import { ArenaHeader, ArenaPlayer, ArenaResult, actionClass, arenaClass } from "@/components/games/tictactoe/arena";
import { Board } from "@/components/games/tictactoe/board";
import { Button, buttonVariants } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Mark, SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { useSession } from "@/lib/auth/session";
import { gameClient } from "@/lib/connect/clients";
import { profileName } from "@/lib/format";
import { gameHref, sessionKey, sessionsKey } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";
import { useGameSocket } from "@/lib/ws/use-game-socket";

export function GameMatch({ id }: { id: string }) {
  const client = useQueryClient();
  const query = useQuery({
    queryKey: sessionKey(id),
    queryFn: async ({ signal }) => {
      const { session } = await gameClient.getSession({ sessionId: id }, { signal });
      if (!session) throw new Error("Game unavailable.");
      return session;
    },
    retry: (count, error) => ![Code.NotFound, Code.InvalidArgument, Code.PermissionDenied].includes(ConnectError.from(error).code) && count < 1,
    staleTime: 10_000,
  });
  const join = useMutation({
    mutationFn: async () => {
      const { session } = await gameClient.joinSession({ sessionId: id });
      if (!session) throw new Error("Game unavailable.");
      return session;
    },
    onSuccess: session => {
      client.setQueryData(sessionKey(id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
    },
  });
  const unknown = ConnectError.from(query.error).code === Code.NotFound;
  const invalid = ConnectError.from(query.error).code === Code.InvalidArgument;
  const joinCode = ConnectError.from(join.error).code;

  return (
    <ScrollArea className="min-h-0 flex-1">
      <main className="workspace-page game-match-page">
        <header className="game-match-heading">
        <Link href={ticTacToeLobbyHref} className={buttonVariants({ variant: "ghost", className: "-ml-2 min-h-11 px-2 text-muted-foreground" })}><ArrowLeft />Back to lobby</Link>
        </header>
        {query.data ? <Match key={id} initial={query.data} /> : (
          <section className="max-w-md space-y-4 rounded-2xl border border-border bg-card p-5">
            {query.isPending ? <p role="status">Loading game…</p> : unknown ? <>
              <h2 className="flex items-center gap-2 font-medium"><Users className="size-5" /> Game invitation</h2>
              <Button variant="signal" className="min-h-11" disabled={join.isPending} onClick={() => join.mutate()}>
                {join.isPending ? "Joining…" : "Join game"}
              </Button>
              {join.isError ? <p role="alert" className="text-sm text-destructive">
                {joinCode === Code.NotFound ? "This game link is unavailable." : joinCode === Code.FailedPrecondition ? "Both seats are already taken." : "Could not join the game. Try again."}
              </p> : null}
            </> : <>
              <p role="alert">{invalid ? "This game link is invalid." : "Could not load the game."}</p>
              {!invalid ? <Button variant="outline" className="min-h-11" onClick={() => void query.refetch()}>Try again</Button> : null}
            </>}
          </section>
        )}
      </main>
    </ScrollArea>
  );
}

function Match({ initial }: { initial: Session }) {
  const router = useRouter();
  const client = useQueryClient();
  const request = useRef<string | undefined>(undefined);
  const playAgain = useMutation({
    mutationFn: async () => {
      request.current ??= crypto.randomUUID();
      const { session } = await gameClient.createSession({ game: initial.game, requestId: request.current });
      if (!session) throw new Error("Could not start a new game.");
      return session;
    },
    onSuccess: session => {
      client.setQueryData(sessionKey(session.id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
      router.push(gameHref(session.id));
    },
  });
  const { user } = useSession();
  const { game, connected, pending, error, retry, move, undo, receivedAt } = useGameSocket(initial);
  const profiles = usePublicProfiles(game.playerIds);
  const [copy, setCopy] = useState<"idle" | "copied" | "manual">("idle");
  const waiting = game.status === SessionStatus.WAITING;
  const complete = finished(game.status);
  const won = complete && game.winnerUserId === user?.id;
  const [dismissedRevision, setDismissedRevision] = useState<number>();
  const [clockNow, setClockNow] = useState(() => performance.now());
  useEffect(() => {
    if (game.status !== SessionStatus.PLAYING) return;
    const timer = setInterval(() => setClockNow(performance.now()), 100);
    return () => clearInterval(timer);
  }, [game.status]);
  const revision = game.revision ?? game.ply;
  const elapsed = Math.max(0, clockNow - receivedAt);
  const remaining = [game.clock?.xRemainingMs ?? 300000, game.clock?.oRemainingMs ?? 300000].map((time, index) =>
    Math.max(0, time - (!complete && !waiting && game.turnUserId === game.playerIds[index] ? elapsed : 0)));
  const mySeat = user?.id === game.playerIds[1] ? 1 : 0;
  const myTurn = game.turnUserId === user?.id;
  const status = game.status === SessionStatus.DRAW ? "Draw"
    : game.status === SessionStatus.WON ? (game.winnerUserId === user?.id ? "You won" : "You lost")
    : waiting ? "Waiting for opponent"
    : myTurn ? "Your turn" : "Opponent’s turn";
  const displayNames = game.playerIds.map(id => id === user?.id ? "You" : profileName(profiles.data?.get(id)) || "Opponent");
  const shareLink = () => {
    if (!navigator.clipboard) { setCopy("manual"); return; }
    void navigator.clipboard.writeText(window.location.href).then(() => setCopy("copied"), () => setCopy("manual"));
  };

  return (
    <div className={arenaClass}>
      <ArenaHeader><span className="rounded-[9px] border border-border bg-card px-3 py-2 text-xs font-semibold text-muted-foreground">Online</span></ArenaHeader>
      <section aria-labelledby="game-board-title" className="min-w-0">
        <h2 id="game-board-title" className="sr-only">Game board</h2>
        <div role="status" aria-atomic="true" data-game-status className="flex min-h-7 flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>{status}</span>
          {!complete && !connected ? <span>Connecting…</span> : null}
          {!complete && pending ? <span>Sending move…</span> : null}
        </div>
        <ArenaPlayer mark={user?.id === game.playerIds[1] ? Mark.X : Mark.O} name={displayNames[user?.id === game.playerIds[1] ? 0 : 1] ?? "Open seat"} active={!complete && !waiting && !myTurn} time={remaining[1 - mySeat]} />
        <div className="relative">
          <Board board={game.ticTacToe} playable={connected && game.status === SessionStatus.PLAYING && myTurn && !pending && remaining[mySeat] > 0} move={move} />
          {complete && dismissedRevision !== revision ? <ArenaResult title={status} kind={game.status === SessionStatus.DRAW ? "draw" : won ? "win" : "lose"} close={() => setDismissedRevision(revision)} restart={() => playAgain.mutate()} pending={playAgain.isPending || playAgain.isSuccess} /> : null}
        </div>
        <ArenaPlayer mark={user?.id === game.playerIds[1] ? Mark.O : Mark.X} name="You" active={!complete && !waiting && myTurn} time={remaining[mySeat]} />
        {game.timedOut ? <p className="text-xs text-muted-foreground">Время вышло</p> : null}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {!connected ? <Button variant="outline" className="min-h-11" onClick={retry}>Reconnect</Button> : null}
      </section>
      <aside className="mt-3 flex min-w-0 flex-col gap-3">
        {waiting ? <div className="space-y-2">
          <Button variant="outline" className="min-h-11 w-full" onClick={shareLink}><Copy />Copy invite link</Button>
          {copy === "copied" ? <p role="status" className="text-sm text-muted-foreground">Link copied</p> : null}
          {copy === "manual" ? <input aria-label="Invite link" readOnly value={window.location.href} onFocus={event => event.target.select()} className="min-h-11 w-full rounded-md border border-border bg-background px-2 text-sm" /> : null}
        </div> : null}
        <div className="flex flex-wrap justify-end gap-2">
          <button type="button" className={actionClass} disabled={!connected || pending || game.ply === 0} onClick={undo}><Undo2 aria-hidden size={15} />Отменить</button>
          {!complete ? <button type="button" className={actionClass} disabled={playAgain.isPending || playAgain.isSuccess} onClick={() => playAgain.mutate()}><Plus aria-hidden size={15} />Новая партия</button> : null}
        </div>
        {!complete && playAgain.isError ? <p role="alert" className="text-sm text-destructive">Could not start a new game. Try again.</p> : null}
        {complete ? <div className="space-y-2">
          <Button variant="outline" className="min-h-9 w-full" disabled={playAgain.isPending || playAgain.isSuccess} onClick={() => playAgain.mutate()}>
            {playAgain.isPending ? "Creating game…" : "Play again"}
          </Button>
          {playAgain.isError ? <p role="alert" className="text-sm text-destructive">Could not start a new game. Try again.</p> : null}
          <Link href={ticTacToeLobbyHref} className={cn(buttonVariants({ variant: "outline" }), "min-h-11 w-full")}>Return to lobby</Link>
        </div> : null}
      </aside>
    </div>
  );
}

function finished(status: SessionStatus) {
  return status === SessionStatus.WON || status === SessionStatus.DRAW;
}
