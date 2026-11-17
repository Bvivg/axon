"use client";

import { Code, ConnectError } from "@connectrpc/connect";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, Users } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { ticTacToeLobbyHref } from "@/components/games/catalogue";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools, SettingsIcon, arenaClass } from "@/components/games/tictactoe/arena";
import { GameSettings, GameStatistics, StatisticsIcon } from "@/components/games/tictactoe/preferences";
import { useArenaShortcuts } from "@/components/games/tictactoe/shortcuts";
import { ticTacToeProfiles } from "@/lib/games/tictactoe-profiles";
import { Board } from "@/components/games/tictactoe/board";
import { Button, buttonVariants } from "@/components/ui/button";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Mark, SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { useSession } from "@/lib/auth/session";
import { gameClient } from "@/lib/connect/clients";
import { profileName } from "@/lib/format";
import { gameHref, sessionKey, sessionsKey } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
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
    <ScrollArea className="min-h-0 flex-1 bg-game-workspace" viewportProps={{ className: "[&>div]:min-h-full" }}>
      <main className="flex min-h-full w-full flex-col px-[max(16px,env(safe-area-inset-left))] pt-[max(16px,env(safe-area-inset-top))] pb-[max(16px,env(safe-area-inset-bottom))]">
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
  const [modal, setModal] = useState<"stats" | "settings" | null>(null);
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

  const canUndo = connected && !pending && !complete && game.ply > 0;
  const makeMove = (cell: number) => { if (!modal && connected && !pending && !waiting && !complete && myTurn) move(cell); };
  useArenaShortcuts({ enabled: !modal, move: makeMove, undo: () => { if (canUndo) undo(); }, restart: () => { if (!playAgain.isPending && !playAgain.isSuccess) playAgain.mutate(); } });
  return (
    <div className={arenaClass}>
      <ArenaHeader><ArenaTools pending={playAgain.isPending || playAgain.isSuccess} restart={() => playAgain.mutate()} undo={undo} canUndo={canUndo}><StatisticsIcon onClick={() => setModal("stats")} /><SettingsIcon onClick={() => setModal("settings")} /></ArenaTools></ArenaHeader>
      <section aria-labelledby="game-board-title" className="grid min-w-0 gap-3">
        <h2 id="game-board-title" className="sr-only">Game board</h2>
        <div role="status" aria-atomic="true" data-game-status className="sr-only">
          <span>{status}</span>
          {!complete && !connected ? <span>Connecting…</span> : null}
          {!complete && pending ? <span>Sending move…</span> : null}
        </div>
        <ArenaPlayer mark={user?.id === game.playerIds[1] ? Mark.X : Mark.O} name={displayNames[user?.id === game.playerIds[1] ? 0 : 1] ?? "Open seat"} active={!complete && !waiting && !myTurn} time={remaining[1 - mySeat]} rating={ticTacToeProfiles.opponent.rating} />
        <div className="relative">
          <Board board={game.ticTacToe} turn={mySeat === 0 ? Mark.X : Mark.O} playable={!modal && connected && game.status === SessionStatus.PLAYING && myTurn && !pending && remaining[mySeat] > 0} move={makeMove} />
          {complete && dismissedRevision !== revision ? <ArenaResult title={status} kind={game.status === SessionStatus.DRAW ? "draw" : won ? "win" : "lose"} close={() => setDismissedRevision(revision)} restart={() => playAgain.mutate()} pending={playAgain.isPending || playAgain.isSuccess} /> : null}
        </div>
        <ArenaPlayer mark={user?.id === game.playerIds[1] ? Mark.O : Mark.X} name={displayNames[mySeat] ?? "You"} rating={ticTacToeProfiles.me.rating} active={!complete && !waiting && myTurn} time={remaining[mySeat]} />
        {game.timedOut ? <span className="sr-only">Время вышло</span> : null}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {!connected ? <Button variant="outline" className="min-h-11" onClick={retry}>Reconnect</Button> : null}
      </section>
      <aside className="flex min-w-0 flex-col gap-3 empty:hidden">
        {waiting ? <div className="space-y-2">
          <Button variant="outline" className="min-h-11 w-full" onClick={shareLink}><Copy />Copy invite link</Button>
          {copy === "copied" ? <p role="status" className="text-sm text-muted-foreground">Link copied</p> : null}
          {copy === "manual" ? <input aria-label="Invite link" readOnly value={window.location.href} onFocus={event => event.target.select()} className="min-h-11 w-full rounded-md border border-border bg-background px-2 text-sm" /> : null}
        </div> : null}
        {!complete && playAgain.isError ? <p role="alert" className="text-sm text-destructive">Could not start a new game. Try again.</p> : null}
      </aside>
      <GameSettings open={modal === "settings"} onOpenChange={open => setModal(open ? "settings" : null)} online preferences={{ mode: "local", duration: 300000, side: mySeat === 0 ? "X" : "O" }}><Link href={ticTacToeLobbyHref} className="flex min-h-[44px] items-center text-game-primary underline">Вернуться в лобби</Link></GameSettings>
      <GameStatistics open={modal === "stats"} onOpenChange={open => setModal(open ? "stats" : null)} winner={complete ? game.status === SessionStatus.DRAW ? "draw" : game.winnerUserId === game.playerIds[0] ? "X" : "O" : undefined} gameKey={game.id} storageKey="axon-ttt-online-stats-v1" />
    </div>
  );
}

function finished(status: SessionStatus) {
  return status === SessionStatus.WON || status === SessionStatus.DRAW;
}
