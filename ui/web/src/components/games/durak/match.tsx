"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Bot, Copy, Flag, LogOut, UserPlus, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";

import { DurakTable, seatNames } from "@/components/games/durak/board";
import { DurakMoves, RoleBadge } from "@/components/games/durak/moves";
import { describeDurak } from "@/components/games/durak/table-settings";
import { SessionLoader } from "@/components/games/session-loader";
import { ReplayLink, SpectatorCount, SpectatorStatus, WatchingTag, isSpectator, spectatorStatus } from "@/components/games/watch";
import { ArenaHeader, ArenaPlayer, ArenaResult, ArenaTools } from "@/components/games/tictactoe/arena";
import { Button } from "@/components/ui/button";
import { DurakEnd, DurakMode, SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { useSession } from "@/lib/auth/session";
import { gameClient } from "@/lib/connect/clients";
import { profileName } from "@/lib/format";
import { GameKey, lobbyHref } from "@/lib/games/catalog";
import { cardName, openAttack } from "@/lib/games/durak";
import { formatClock } from "@/lib/games/tictactoe";
import { formatDelta, gameHref, ratingDelta, ratingsKey, sessionKey, sessionsKey, useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";
import { useGameSocket } from "@/lib/ws/use-game-socket";

const lobby = lobbyHref(GameKey.Durak);
const limit = 600000;
const ordinals = ["1st", "2nd", "3rd", "4th", "5th", "6th"];

export function DurakGame({ id }: { id: string }) {
  const { user } = useSession();
  return <SessionLoader id={id}>{session => <DurakMatch key={`${id}:${session.playerIds.includes(user?.id ?? "")}`} initial={session} />}</SessionLoader>;
}

export function durakReason(game: Session) {
  if (game.timedOut) return "Time's up";
  if (game.resigned) return "Resignation";
  if (game.agreedDraw) return "Agreed draw";
  if (game.moveLimit) return "Move limit";
  switch (game.durak?.end) {
    case DurakEnd.DURAK: return "Last one holding cards";
    case DurakEnd.DRAW: return game.durak.hands.length > 2 ? "No one left holding cards" : "Both out together";
    default: return "";
  }
}

export function durakSettings(game: Session) {
  const options = game.options?.game.case === "durak" ? game.options.game.value : undefined;
  return {
    seats: game.seats ?? 2,
    mode: options?.mode ?? DurakMode.THROW_IN,
    cheats: options?.cheats ?? false,
    deck: options?.deck || 36,
  };
}

function placeTitle(place: number, seats: number) {
  if (place === seats) return "You are the durak";
  return place === 1 ? "You won · 1st place" : `${ordinals[place - 1] ?? `${place}th`} place`;
}

function DurakMatch({ initial }: { initial: Session }) {
  const router = useRouter();
  const client = useQueryClient();
  const request = useRef<string | undefined>(undefined);
  const { user } = useSession();
  const { game, connected, pending, error, retry, durakMove, receivedAt } = useGameSocket(initial);
  const people = game.playerIds.filter((_, seat) => !game.seating[seat]?.bot);
  const profiles = usePublicProfiles(people);
  const ratings = useRatings(game.game, game.rated ? people : []);
  const [confirmLeave, setConfirmLeave] = useState(false);
  const [copy, setCopy] = useState<"idle" | "copied" | "manual">("idle");
  const [dismissedRevision, setDismissedRevision] = useState<number>();
  const [clockNow, setClockNow] = useState(() => performance.now());
  const settings = durakSettings(game);
  const capacity = Math.max(game.seating.length, settings.seats, 2);
  const table = capacity > 2;
  const playAgain = useMutation({
    mutationFn: async () => {
      if (initial.rated) return undefined;
      request.current ??= crypto.randomUUID();
      const bots = game.seating.filter(seat => seat.bot).length;
      const { session } = await gameClient.createSession({ game: GameKey.Durak, requestId: request.current, seats: game.seats, options: game.options, bots: bots || undefined });
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
      if (session.status === SessionStatus.WAITING && !session.playerIds.includes(user?.id ?? "")) router.push(lobby);
    },
  });
  const join = useMutation({
    mutationFn: () => gameClient.joinSession({ sessionId: initial.id }),
    onSuccess: ({ session }) => {
      if (session) client.setQueryData(sessionKey(initial.id), session);
      void client.invalidateQueries({ queryKey: sessionsKey });
    },
  });
  const fill = useMutation({
    mutationFn: () => gameClient.fillWithBots({ sessionId: initial.id }),
    onSuccess: ({ session }) => {
      if (session) client.setQueryData(sessionKey(initial.id), session);
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

  const board = game.durak;
  const waiting = game.status === SessionStatus.WAITING;
  const complete = game.status === SessionStatus.WON || game.status === SessionStatus.DRAW;
  const seated = game.playerIds.includes(user?.id ?? "");
  const visitor = waiting && !seated;
  const spectating = isSpectator(game, user?.id);
  const mySeat = Math.max(0, game.playerIds.indexOf(user?.id ?? ""));
  const myTurn = game.turnUserId === user?.id;
  const host = game.playerIds[0] === user?.id;
  const left = game.seating[mySeat]?.left ?? false;
  const place = game.seating[mySeat]?.place;
  const won = complete && (table ? place === 1 : game.winnerUserId === user?.id);
  const revision = game.revision ?? game.ply;
  const elapsed = Math.max(0, clockNow - receivedAt);
  const clocks = game.clock?.remainingMs.length ? game.clock.remainingMs : [game.clock?.xRemainingMs ?? limit, game.clock?.oRemainingMs ?? limit];
  const remaining = Array.from({ length: capacity }, (_, seat) => Math.max(0, (clocks[seat] ?? limit) - (!complete && !waiting && game.turnUserId === game.playerIds[seat] ? elapsed : 0)));
  const nameOf = (seat: number) => {
    const id = game.playerIds[seat];
    if (!id) return "Open seat";
    if (game.seating[seat]?.bot) return `Bot ${seat}`;
    if (id === user?.id) return "You";
    return profileName(profiles.data?.get(id)) || (spectating ? seatNames[seat] ?? `Seat ${seat + 1}` : table ? `Player ${seat + 1}` : "Opponent");
  };
  const names = Array.from({ length: capacity }, (_, seat) => nameOf(seat));
  const ratingOf = (seat: number) => {
    const id = game.playerIds[seat];
    const change = game.ratingChanges.find(item => item.userId === id);
    return game.rated && id && !game.seating[seat]?.bot ? change?.after ?? ratings.data?.get(id)?.rating ?? 1200 : undefined;
  };
  const delta = ratingDelta(game, user?.id);
  const why = durakReason(game);
  const defender = board?.defenderSeat ?? (board ? (board.attackerSeat + 1) % Math.max(board.hands.length, 2) : 1);
  const defending = defender === mySeat;
  const open = board ? openAttack(board) : undefined;
  const transfer = board?.legalMoves.some(move => move.startsWith("transfer ")) ?? false;
  const task = !board ? "" : defending ? (open ? `defend the ${cardName(open)}${transfer ? " or transfer" : ""}` : "defend")
    : board.attackerSeat === mySeat && !board.table.length ? "attack" : board.taking ? "throw in or pass, they take" : "throw in or pass";
  const turnSeat = game.playerIds.indexOf(game.turnUserId ?? "");
  const status = spectating ? spectatorStatus(game, names, names)
    : complete && table && place ? placeTitle(place, capacity)
    : game.status === SessionStatus.DRAW ? "Draw"
    : game.status === SessionStatus.WON ? (won ? "You won" : "You lost")
    : waiting ? (table ? `Waiting for players · ${game.playerIds.length} of ${capacity}` : "Waiting for opponent")
    : left ? "You left · a bot plays your seat"
    : myTurn ? `Your move · ${task}` : table && turnSeat >= 0 ? `${names[turnSeat]} to move` : "Opponent’s move";
  const live = connected && !pending && !complete && !waiting && !left && !spectating;
  const playable = myTurn && live && remaining[mySeat]! > 0;
  const legal = live ? board?.legalMoves ?? [] : [];
  const shareLink = () => {
    if (!navigator.clipboard) { setCopy("manual"); return; }
    void navigator.clipboard.writeText(window.location.href).then(() => setCopy("copied"), () => setCopy("manual"));
  };
  const viewSeat = spectating ? 0 : mySeat;
  const roleOf = (seat: number) => board?.attackerSeat === seat;
  const player = (seat: number, mine: boolean) => {
    const hand = board?.hands[seat];
    return (
      <ArenaPlayer badge={<RoleBadge attacking={roleOf(seat)} />} side={seatNames[seat]} name={names[seat] ?? (mine ? "You" : "Open seat")} bot={game.seating[seat]?.bot}
        active={!complete && !waiting && game.turnUserId === game.playerIds[seat]} time={remaining[seat]} rating={ratingOf(seat)}
        extra={hand ? <span data-hand-count={hand.count} className="text-[13px] text-game-text2">{`${hand.count} ${hand.count === 1 ? "card" : "cards"}`}</span> : undefined} />
    );
  };
  const chip = (seat: number) => {
    const active = !complete && !waiting && game.turnUserId === game.playerIds[seat];
    const seatPlace = game.seating[seat]?.place;
    return (
      <div data-seat-label={seat} data-active={active || undefined} className={cn("flex max-w-full min-w-0 items-center gap-1.5 rounded-full border border-game-border bg-game-card px-2 py-1 text-[12px]", active && "border-game-primary")}>
        {game.seating[seat]?.bot ? <Bot aria-hidden className="size-3.5 shrink-0 text-game-text2" /> : null}
        <span className="min-w-0 truncate font-medium">{names[seat]}</span>
        {seatPlace ? <span data-place={seatPlace} className="font-mono text-game-text2">{seatPlace === capacity ? "durak" : ordinals[seatPlace - 1]}</span>
          : <RoleBadge attacking={roleOf(seat)} className={cn(defender === seat && "text-game-primary")} />}
        {game.seating[seat]?.left ? <span className="text-game-text2">left</span> : null}
        {!seatPlace && !game.seating[seat]?.bot ? <span data-clock className={cn("font-mono tabular-nums text-game-text2", active && "text-game-primary")}>{formatClock(remaining[seat] ?? 0).replace(/^0/, "")}</span> : null}
      </div>
    );
  };
  const resultTitle = table && place ? placeTitle(place, capacity) : status;
  const resultKind = game.status === SessionStatus.DRAW && !table ? "draw" : won ? "win" : table && place !== capacity ? "draw" : "lose";

  return (
    <div className="mx-auto grid w-full max-w-[1040px] min-w-0 gap-4 font-sans text-[14px] leading-[1.45] text-game-text lg:grid-cols-[minmax(0,620px)_minmax(260px,1fr)]">
      <section aria-labelledby="durak-table-title" className="grid min-w-0 gap-3">
        <ArenaHeader title="Durak" lobby={lobby}>{spectating ? <WatchingTag /> : <ArenaTools pending={playAgain.isPending || playAgain.isSuccess} restart={() => playAgain.mutate()} undo={() => undefined} canUndo={false} undoHidden>{null}</ArenaTools>}</ArenaHeader>
        <h2 id="durak-table-title" className="sr-only">Durak table</h2>
        <div role="status" aria-atomic="true" data-game-status className="sr-only">
          <span>{status}</span>
          {complete && why ? <span>{` · ${why}`}</span> : null}
          {!complete && !connected ? <span>Connecting…</span> : null}
        </div>
        {waiting && table ? <WaitingRoom game={game} capacity={capacity} names={names} /> : <>
          {!table ? player(1 - viewSeat, false) : null}
          <div className="relative">
            <DurakTable view={board} seat={viewSeat} legal={legal} playable={playable} onMove={durakMove} cheats={settings.cheats} label={table ? chip : undefined} />
            {complete && !spectating && dismissedRevision !== revision ? <ArenaResult title={resultTitle} detail={[why, delta !== undefined ? `Rating ${formatDelta(delta)}` : ""].filter(Boolean).join(" · ")} kind={resultKind} close={() => setDismissedRevision(revision)} restart={() => playAgain.mutate()} pending={playAgain.isPending || playAgain.isSuccess} /> : null}
          </div>
          {player(viewSeat, true)}
        </>}
        {!spectating && !complete && !waiting ? <p aria-hidden className="text-center text-[13px] text-game-text2">{status}</p> : null}
        {error ? <p role="alert" className="text-sm text-destructive">{error}</p> : null}
        {!connected ? <Button variant="outline" className="min-h-11" onClick={retry}>Reconnect</Button> : null}
      </section>
      <aside className="flex min-w-0 flex-col gap-3 lg:pt-14">
        {table || settings.cheats || settings.mode === DurakMode.TRANSFER ? <p data-table-settings className="text-sm text-muted-foreground">{describeDurak({ ...settings, seats: capacity })}</p> : null}
        <SpectatorCount count={game.spectators} />
        {spectating ? <SpectatorStatus text={[status, complete ? why : ""].filter(Boolean).join(" · ")} /> : null}
        {complete ? <ReplayLink game={game} /> : null}
        {visitor ? <div className="space-y-2">
          <Button variant="signal" className="min-h-11 w-full" disabled={join.isPending} onClick={() => join.mutate()}><UserPlus />{join.isPending ? "Joining…" : "Join game"}</Button>
          {join.isError ? <p role="alert" className="text-sm text-destructive">Could not join. The table may be full.</p> : null}
        </div> : null}
        {waiting && seated ? <div className="space-y-2">
          {host && table ? <Button variant="signal" className="min-h-11 w-full" disabled={fill.isPending} onClick={() => fill.mutate()}><Bot />{fill.isPending ? "Seating bots…" : "Fill with bots"}</Button> : null}
          {fill.isError ? <p role="alert" className="text-sm text-destructive">Could not seat the bots. Try again.</p> : null}
          <Button variant="outline" className="min-h-11 w-full" onClick={shareLink}><Copy />Copy invite link</Button>
          {copy === "copied" ? <p role="status" className="text-sm text-muted-foreground">Link copied</p> : null}
          {copy === "manual" ? <input aria-label="Invite link" readOnly value={window.location.href} onFocus={event => event.target.select()} className="min-h-11 w-full rounded-md border border-border bg-background px-2 text-sm" /> : null}
        </div> : null}
        {seated && !complete && !left ? (confirmLeave ? <div className="space-y-2 rounded-game-md border border-game-border bg-game-card p-3">
          <p className="text-sm">{waiting ? (host ? "Cancel this table?" : "Leave this table?") : table ? "Leave the table? A bot plays your cards and you finish last." : game.rated ? "Resign? You will lose rating." : "Resign this game?"}</p>
          <div className="flex gap-2">
            <Button variant="destructive" className="min-h-11 flex-1" disabled={leave.isPending} onClick={() => leave.mutate()}>{waiting ? (host ? "Cancel game" : "Leave") : table ? "Leave" : "Resign"}</Button>
            <Button variant="outline" className="min-h-11 flex-1" disabled={leave.isPending} onClick={() => setConfirmLeave(false)}>Keep playing</Button>
          </div>
        </div> : <Button variant="outline" className="min-h-11 w-full" onClick={() => setConfirmLeave(true)}>{waiting ? (host ? <><X />Cancel game</> : <><LogOut />Leave table</>) : table ? <><LogOut />Leave table</> : <><Flag />Resign</>}</Button>) : null}
        {leave.isError ? <p role="alert" className="text-sm text-destructive">Could not leave the game. Try again.</p> : null}
        {playAgain.isError ? <p role="alert" className="text-sm text-destructive">Could not start a new game. Try again.</p> : null}
        <DurakMoves moves={board?.moves ?? []} />
      </aside>
    </div>
  );
}

function WaitingRoom({ game, capacity, names }: { game: Session; capacity: number; names: string[] }) {
  return (
    <ol aria-label="Seats" data-testid="durak-seats" className="grid gap-2 rounded-game-md border border-game-border bg-durak-felt p-3 sm:grid-cols-2">
      {Array.from({ length: capacity }, (_, seat) => {
        const taken = Boolean(game.playerIds[seat]);
        return (
          <li key={seat} data-seat={seat} data-taken={taken || undefined} className={cn("flex min-h-12 items-center gap-2 rounded-game-sm border border-game-border bg-game-card px-3 text-sm", !taken && "border-dashed bg-transparent text-game-text2")}>
            <span className="font-mono text-[12px] text-game-text2">{seat + 1}</span>
            {game.seating[seat]?.bot ? <Bot aria-hidden className="size-4 text-game-text2" /> : null}
            <span className="min-w-0 truncate font-medium">{taken ? names[seat] : "Open seat"}</span>
            {seat === 0 ? <span className="ml-auto text-[12px] text-game-text2">host</span> : null}
          </li>
        );
      })}
    </ol>
  );
}
