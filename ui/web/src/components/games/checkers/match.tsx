"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Copy, Flag, Handshake, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { CheckersBoard, CheckersPiece, PiecesLeft, seatNames } from "@/components/games/checkers/board";
import { MoveList } from "@/components/games/chess/moves";
import { SessionLoader } from "@/components/games/session-loader";
import { ReplayLink, SpectatorCount, SpectatorStatus, WatchingTag, isSpectator, spectatorStatus } from "@/components/games/watch";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools } from "@/components/games/tictactoe/arena";
import { Button } from "@/components/ui/button";
import { CheckersEnd, DrawAction, SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { useSession } from "@/lib/auth/session";
import { gameClient } from "@/lib/connect/clients";
import { profileName } from "@/lib/format";
import { GameKey, lobbyHref } from "@/lib/games/catalog";
import { boardFrom, Piece, sideOf } from "@/lib/games/checkers";
import { formatDelta, gameHref, ratingDelta, ratingsKey, sessionKey, sessionsKey, useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import { useGameSocket } from "@/lib/ws/use-game-socket";

const lobby = lobbyHref(GameKey.Checkers);
const limit = 600000;

export const checkersEndings: Partial<Record<CheckersEnd, string>> = {
  [CheckersEnd.NO_MOVES]: "No moves left",
  [CheckersEnd.FORTY_MOVES]: "40-move rule",
  [CheckersEnd.MOVE_LIMIT]: "Move limit",
};

export function CheckersGame({ id }: { id: string }) {
  return <SessionLoader id={id}>{session => <CheckersMatch key={id} initial={session} />}</SessionLoader>;
}

export function checkersReason(game: Session) {
  if (game.timedOut) return "Time's up";
  if (game.resigned) return "Resignation";
  if (game.agreedDraw) return "Draw agreed";
  return checkersEndings[game.checkers?.end ?? CheckersEnd.UNSPECIFIED] ?? "";
}

function CheckersMatch({ initial }: { initial: Session }) {
  const router = useRouter();
  const client = useQueryClient();
  const request = useRef<string | undefined>(undefined);
  const { user } = useSession();
  const { game, connected, pending, error, retry, checkersMove, draw, undo, receivedAt } = useGameSocket(initial);
  const profiles = usePublicProfiles(game.playerIds);
  const ratings = useRatings(game.game, game.rated ? game.playerIds : []);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [copy, setCopy] = useState<"idle" | "copied" | "manual">("idle");
  const [dismissedRevision, setDismissedRevision] = useState<number>();
  const [clockNow, setClockNow] = useState(() => performance.now());
  const playAgain = useMutation({
    mutationFn: async () => {
      if (initial.rated) return undefined;
      request.current ??= crypto.randomUUID();
      const { session } = await gameClient.createSession({ game: GameKey.Checkers, requestId: request.current });
      if (!session) throw new Error("Could not start a new game.");
      return session;
    },
    onSuccess: session => {
      if (!session) { router.push(`${lobby}?play=quick`); return; }
      client.setQueryData(sessionKey(session.id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
      router.push(gameHref(session.id, session.game));
    },
  });
  const leave = useMutation({
    mutationFn: () => gameClient.leaveSession({ sessionId: initial.id }),
    onSuccess: ({ session, cancelled }) => {
      void client.invalidateQueries({ queryKey: sessionsKey });
      void client.invalidateQueries({ queryKey: ratingsKey() });
      setConfirmLeave(false);
      if (cancelled || !session) { client.removeQueries({ queryKey: sessionKey(initial.id) }); router.push(lobby); return; }
      client.setQueryData(sessionKey(initial.id), session);
    },
  });
  useEffect(() => {
    if (game.rated && game.ratingChanges.length) void client.invalidateQueries({ queryKey: ratingsKey() });
  }, [client, game.rated, game.ratingChanges.length]);
  useEffect(() => {
    void client.invalidateQueries({ queryKey: sessionsKey });
  }, [client, game.status]);
  useEffect(() => {
    if (game.status !== SessionStatus.PLAYING) return;
    const timer = setInterval(() => setClockNow(performance.now()), 100);
    return () => clearInterval(timer);
  }, [game.status]);

  const squares = boardFrom(game.checkers?.squares);
  const waiting = game.status === SessionStatus.WAITING;
  const complete = game.status === SessionStatus.WON || game.status === SessionStatus.DRAW;
  const mySeat = user?.id === game.playerIds[1] ? 1 : 0;
  const myTurn = game.turnUserId === user?.id;
  const won = complete && game.winnerUserId === user?.id;
  const revision = game.revision ?? game.ply;
  const elapsed = Math.max(0, clockNow - receivedAt);
  const remaining = [game.clock?.xRemainingMs ?? limit, game.clock?.oRemainingMs ?? limit].map((time, index) =>
    Math.max(0, time - (!complete && !waiting && game.turnUserId === game.playerIds[index] ? elapsed : 0)));
  const spectating = isSpectator(game, user?.id);
  const names = game.playerIds.map((id, seat) => id === user?.id ? "You" : profileName(profiles.data?.get(id)) || (spectating ? seatNames[seat] ?? "Player" : "Opponent"));
  const ratingOf = (seat: number) => {
    const id = game.playerIds[seat];
    const change = game.ratingChanges.find(item => item.userId === id);
    return game.rated && id ? change?.after ?? ratings.data?.get(id)?.rating ?? 1200 : undefined;
  };
  const count = (seat: number) => squares.filter(piece => sideOf(piece) === seat).length;
  const delta = ratingDelta(game, user?.id);
  const why = checkersReason(game);
  const legal = game.checkers?.legalMoves ?? [];
  const forced = myTurn && legal.some(move => move.includes("x"));
  const status = spectating ? spectatorStatus(game, names, seatNames)
    : game.status === SessionStatus.DRAW ? "Draw"
    : game.status === SessionStatus.WON ? (won ? "You won" : "You lost")
    : waiting ? "Waiting for opponent"
    : myTurn ? (forced ? "Capture · Your move" : "Your move") : "Opponent’s move";
  const offer = game.drawOfferUserId;
  const offeredByMe = offer === user?.id;
  const offeredToMe = Boolean(offer) && !offeredByMe;
  const playable = myTurn && connected && !pending && !complete && remaining[mySeat]! > 0;
  const canUndo = !game.rated && connected && !pending && !complete && game.ply > 0;
  const shareLink = () => {
    if (!navigator.clipboard) { setCopy("manual"); return; }
    void navigator.clipboard.writeText(window.location.href).then(() => setCopy("copied"), () => setCopy("manual"));
  };
  const player = (seat: number, mine: boolean) => (
    <ArenaPlayer badge={<CheckersPiece piece={seat === 0 ? Piece.BlackMan : Piece.WhiteMan} className="size-[13px]" />} side={seatNames[seat]} name={names[seat] ?? (mine ? "You" : "Open seat")}
      active={!complete && !waiting && game.turnUserId === game.playerIds[seat]} time={remaining[seat]} rating={ratingOf(seat)} extra={<PiecesLeft count={count(seat)} />} />
  );

  return (
    <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
      <section aria-labelledby="checkers-board-title" className="grid min-w-0 gap-3">
        <ArenaHeader title="Checkers" lobby={lobby}>{spectating ? <WatchingTag /> : <ArenaTools pending={playAgain.isPending || playAgain.isSuccess} restart={() => playAgain.mutate()} undo={undo} canUndo={canUndo} undoHidden={game.rated}>{null}</ArenaTools>}</ArenaHeader>
        <h2 id="checkers-board-title" className="sr-only">Checkers board</h2>
        <div role="status" aria-atomic="true" data-game-status className="sr-only">
          <span>{status}</span>
          {complete && why ? <span>{` · ${why}`}</span> : null}
          {!complete && !connected ? <span>Connecting…</span> : null}
        </div>
        {player(1 - mySeat, false)}
        <div className="relative">
          <CheckersBoard squares={squares} seat={mySeat} legal={playable ? legal : []} lastMove={game.checkers?.lastMove} playable={playable} onMove={checkersMove} />
          {complete && !spectating && dismissedRevision !== revision ? <ArenaResult title={status} detail={[why, delta !== undefined ? `Rating ${formatDelta(delta)}` : ""].filter(Boolean).join(" · ")} kind={game.status === SessionStatus.DRAW ? "draw" : won ? "win" : "lose"} close={() => setDismissedRevision(revision)} restart={() => playAgain.mutate()} pending={playAgain.isPending || playAgain.isSuccess} /> : null}
        </div>
        {player(mySeat, true)}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {!connected ? <Button variant="outline" className="min-h-11" onClick={retry}>Reconnect</Button> : null}
      </section>
      <aside className="flex min-w-0 flex-col gap-3 lg:pt-14">
        <SpectatorCount count={game.spectators} />
        {spectating ? <SpectatorStatus text={[status, complete ? why : ""].filter(Boolean).join(" · ")} /> : null}
        {complete ? <ReplayLink game={game} /> : null}
        {waiting ? <div className="space-y-2">
          <Button variant="outline" className="min-h-11 w-full" onClick={shareLink}><Copy />Copy invite link</Button>
          {copy === "copied" ? <p role="status" className="text-sm text-muted-foreground">Link copied</p> : null}
          {copy === "manual" ? <input aria-label="Invite link" readOnly value={window.location.href} onFocus={event => event.target.select()} className="min-h-11 w-full rounded-md border border-border bg-background px-2 text-sm" /> : null}
        </div> : null}
        {!spectating && !complete && !waiting && offeredToMe ? <div className="space-y-2 rounded-game-md border border-game-border bg-game-card p-3">
          <p className="text-sm">Your opponent offers a draw</p>
          <div className="flex gap-2">
            <Button variant="signal" className="min-h-11 flex-1" disabled={pending} onClick={() => draw(DrawAction.ACCEPT)}>Accept draw</Button>
            <Button variant="outline" className="min-h-11 flex-1" disabled={pending} onClick={() => draw(DrawAction.DECLINE)}>Decline</Button>
          </div>
        </div> : null}
        {!spectating && !complete && !waiting && !offeredToMe ? <Button variant="outline" className="min-h-11 w-full" disabled={offeredByMe || pending || !connected} onClick={() => draw(DrawAction.OFFER)}><Handshake />{offeredByMe ? "Draw offered" : "Offer draw"}</Button> : null}
        {!spectating && !complete ? (confirmLeave ? <div className="space-y-2 rounded-game-md border border-game-border bg-game-card p-3">
          <p className="text-sm">{waiting ? "Cancel this game?" : game.rated ? "Resign? You will lose rating." : "Resign this game?"}</p>
          <div className="flex gap-2">
            <Button variant="destructive" className="min-h-11 flex-1" disabled={leave.isPending} onClick={() => leave.mutate()}>{waiting ? "Cancel game" : "Resign"}</Button>
            <Button variant="outline" className="min-h-11 flex-1" disabled={leave.isPending} onClick={() => setConfirmLeave(false)}>Keep playing</Button>
          </div>
        </div> : <Button variant="outline" className="min-h-11 w-full" onClick={() => setConfirmLeave(true)}>{waiting ? <><X />Cancel game</> : <><Flag />Resign</>}</Button>) : null}
        {leave.isError ? <p role="alert" className="text-sm text-destructive">Could not leave the game. Try again.</p> : null}
        {playAgain.isError ? <p role="alert" className="text-sm text-destructive">Could not start a new game. Try again.</p> : null}
        <MoveList san={game.checkers?.moves ?? []} />
      </aside>
    </div>
  );
}
