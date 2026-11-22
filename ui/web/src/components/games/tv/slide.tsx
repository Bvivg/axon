"use client";

import { ChevronDown, Eye, Maximize2 } from "lucide-react";
import Link from "next/link";
import { useEffect, useState, type ReactNode } from "react";

import { GameIcon } from "@/components/games/catalogue";
import { CheckersPiece } from "@/components/games/checkers/board";
import { checkersReason } from "@/components/games/checkers/match";
import { chessReason } from "@/components/games/chess/match";
import { ChessPiece, PieceKind, Side } from "@/components/games/chess/pieces";
import { durakReason } from "@/components/games/durak/match";
import { RoleBadge } from "@/components/games/durak/moves";
import { Position, sides } from "@/components/games/position";
import { Pawn } from "@/components/games/quoridor/board";
import { quoridorReason } from "@/components/games/quoridor/match";
import { ArenaPlayer } from "@/components/games/tictactoe/arena";
import { GameMark } from "@/components/games/tictactoe/board";
import { spectatorStatus } from "@/components/games/watch";
import { Button, buttonVariants } from "@/components/ui/button";
import { Mark, SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { profileName } from "@/lib/format";
import { Piece } from "@/lib/games/checkers";
import { GameKey, gameHref, gameKey, games } from "@/lib/games/catalog";
import { finished } from "@/lib/games/tv";
import { useRatings } from "@/lib/query/games";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";

const timeLimit = 600_000;

function endReason(kind: GameKey, game: Session) {
  switch (kind) {
    case GameKey.Chess: return chessReason(game);
    case GameKey.Checkers: return checkersReason(game);
    case GameKey.Quoridor: return quoridorReason(game);
    case GameKey.Durak: return durakReason(game);
    default:
      if (game.timedOut) return "Time's up";
      if (game.resigned) return "Resignation";
      if (game.agreedDraw) return "Draw agreed";
      return game.status === SessionStatus.WON ? "Three in a row" : "";
  }
}

function SeatBadge({ kind, seat, game }: { kind: GameKey; seat: number; game: Session }): ReactNode {
  switch (kind) {
    case GameKey.Chess: return <ChessPiece kind={PieceKind.King} side={seat === 0 ? Side.White : Side.Black} className="size-[13px]" />;
    case GameKey.Checkers: return <CheckersPiece piece={seat === 0 ? Piece.BlackMan : Piece.WhiteMan} className="size-[13px]" />;
    case GameKey.Quoridor: return <Pawn seat={seat} className="size-[11px]" />;
    case GameKey.Durak: return <RoleBadge attacking={game.durak?.attackerSeat === seat} />;
    default: return <GameMark mark={seat === 0 ? Mark.X : Mark.O} className="size-[11px]" />;
  }
}

function useNow(running: boolean) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const timer = setInterval(() => setNow(Date.now()), 250);
    return () => clearInterval(timer);
  }, [running]);
  return now;
}

export function TvSlide({ game, previous, receivedAt, active, embedded, next }: {
  game: Session;
  previous?: Session;
  receivedAt: number;
  active: boolean;
  embedded: boolean;
  next?: () => void;
}) {
  const kind = gameKey(game.game);
  const profiles = usePublicProfiles(game.playerIds);
  const ratings = useRatings(game.game, game.playerIds);
  const complete = finished(game);
  const playing = game.status === SessionStatus.PLAYING;
  const now = useNow(active && playing);
  const elapsed = Math.max(0, now - receivedAt);
  const names = game.playerIds.map((id, seat) => profileName(profiles.data?.get(id)) || sides[kind][seat] || "Player");
  const remaining = [game.clock?.xRemainingMs ?? timeLimit, game.clock?.oRemainingMs ?? timeLimit].map((time, seat) =>
    Math.max(0, time - (playing && game.turnUserId === game.playerIds[seat] ? elapsed : 0)));
  const status = spectatorStatus(game, names, sides[kind]);
  const why = complete ? endReason(kind, game) : "";
  const bottom = kind === GameKey.Checkers ? 1 : 0;
  const ratingOf = (seat: number) => {
    const id = game.playerIds[seat];
    if (!id) return undefined;
    return game.ratingChanges.find(item => item.userId === id)?.after ?? ratings.data?.get(id)?.rating ?? 1200;
  };
  const player = (seat: number) => {
    const hand = kind === GameKey.Durak ? game.durak?.hands[seat] : undefined;
    return (
      <ArenaPlayer badge={<SeatBadge kind={kind} seat={seat} game={game} />} side={sides[kind][seat]} name={names[seat] ?? "Open seat"}
        active={playing && game.turnUserId === game.playerIds[seat]} time={remaining[seat]} rating={ratingOf(seat)}
        extra={hand ? <span data-hand-count={hand.count} className="text-[13px] text-game-text2">{`${hand.count} ${hand.count === 1 ? "card" : "cards"}`}</span> : undefined} />
    );
  };
  const target = embedded ? "_blank" : undefined;
  const open = (
    <Link href={gameHref(game.id, game.game)} target={target} rel={target ? "noopener" : undefined} className={cn(buttonVariants({ variant: "outline" }), "min-h-11 shrink-0")}>
      <Maximize2 aria-hidden />Open game
    </Link>
  );

  return (
    <div className="@container mx-auto flex h-full w-full max-w-[624px] min-w-0 flex-col gap-2.5 px-4 pt-3 pb-4 md:px-16 font-sans text-[14px] leading-[1.45] text-game-text">
      <div className="flex min-h-11 items-center gap-3">
        <GameIcon game={kind} className="size-9" />
        <div className="min-w-0 flex-1">
          <p className="truncate font-semibold">{games[kind].title}</p>
          <p className="truncate text-[13px] text-game-text2"><span data-tv-ply={game.ply}>{`Move ${game.ply}`}</span>{game.rated ? " · Rated" : ""}</p>
        </div>
        {game.spectators ? <span data-tv-spectators={game.spectators} className="flex items-center gap-1 text-[13px] text-game-text2"><Eye aria-hidden className="size-4" />{game.spectators}<span className="sr-only"> watching</span></span> : null}
        {complete ? (
          <span data-tv-ended className="rounded-full border border-game-border px-2 py-0.5 text-[12px] font-medium text-game-text2">Ended</span>
        ) : (
          <span data-tv-live className="inline-flex items-center gap-1.5 rounded-full bg-destructive/12 px-2 py-0.5 text-[12px] font-semibold tracking-wide text-destructive">
            <span aria-hidden className="size-1.5 rounded-full bg-destructive motion-safe:animate-pulse" />LIVE
          </span>
        )}
      </div>
      {player(1 - bottom)}
      <div className="grid min-h-0 flex-1 place-items-center [container-type:size]">
        <div data-tv-board={kind} inert className={kind === GameKey.Durak ? "w-full max-h-full" : "w-[min(100cqw,100cqh)]"}>
          <Position game={kind} frame={game} previous={previous} />
        </div>
      </div>
      {player(bottom)}
      {complete ? (
        <div data-tv-result className="grid gap-2.5 rounded-game-md border border-game-border bg-game-card p-3">
          <p data-tv-status role={active ? "status" : undefined} aria-atomic="true" className="min-w-0 text-[15px] font-semibold">{[status, why].filter(Boolean).join(" · ")}</p>
          <div className="flex items-center gap-2">
            {next ? <Button variant="signal" className="min-h-11 flex-1" onClick={next}><ChevronDown aria-hidden />Next game</Button>
              : <p className="min-w-0 flex-1 text-[13px] text-game-text2">Waiting for the next live game</p>}
            {open}
          </div>
        </div>
      ) : (
        <div className="flex min-h-11 items-center gap-3">
          <p data-tv-status role={active ? "status" : undefined} aria-atomic="true" className="min-w-0 flex-1 truncate text-[13px] text-game-text2">{status}</p>
          {open}
        </div>
      )}
    </div>
  );
}
