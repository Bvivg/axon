"use client";

import { Crown, Eye, Play, Trophy } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

import { ConfirmAction } from "@/components/ui/confirm-action";
import { TournamentStatusBadge } from "@/components/games/tournaments/list";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { ErrorCard } from "@/components/ui/error-card";
import { Pane, PaneSection } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import {
  MatchResult,
  RoundStatus,
  TournamentFormat,
  TournamentStatus,
  type Match,
  type Tournament,
} from "@/gen/axon/tournament/v1/tournament_pb";
import { useSession } from "@/lib/auth/session";
import { profileName } from "@/lib/format";
import { gameHref, gameKey, games } from "@/lib/games/catalog";
import { usePublicProfiles } from "@/lib/query/people";
import { TournamentAction, tournamentFormatLabel, tournamentsHref, useTournament, useTournamentAction, useTournamentGame } from "@/lib/query/tournaments";
import { cn } from "@/lib/utils";

type Names = (id?: string) => string;

function points(half: number) {
  const whole = Math.floor(half / 2);
  return half % 2 ? (whole ? `${whole}½` : "½") : String(whole);
}

function roundTitle(round: number, total: number, format: TournamentFormat) {
  if (format === TournamentFormat.SWISS) return `Round ${round}`;
  if (round === total) return "Final";
  if (round === total - 1) return "Semifinals";
  if (round === total - 2) return "Quarterfinals";
  return `Round ${round}`;
}

function useNames(tournament: Tournament | undefined): Names {
  const ids = tournament?.registrations.map(r => r.userId) ?? [];
  const profiles = usePublicProfiles(ids).data;
  return (id?: string) => {
    if (!id) return "TBD";
    const profile: PublicProfile | undefined = profiles?.get(id);
    return profileName(profile) || "Player";
  };
}

function Actions({ tournament }: { tournament: Tournament }) {
  const { user } = useSession();
  const action = useTournamentAction(tournament.id);
  const me = user?.id ?? "";
  const admin = user?.roles.includes("admin") ?? false;
  const creator = tournament.creatorId === me;
  const registered = tournament.registrations.some(r => r.userId === me && !r.returned);
  const open = tournament.status === TournamentStatus.REGISTRATION;
  const live = tournament.status === TournamentStatus.RUNNING || tournament.status === TournamentStatus.SEEDING;
  const [asking, setAsking] = useState(TournamentAction.Start);
  const [confirming, setConfirming] = useState(false);
  const overrides = (next: TournamentAction) => admin && (!creator || (next === TournamentAction.Cancel && live));
  const run = (next: TournamentAction) => {
    if (!overrides(next)) {
      action.mutate({ action: next });
      return;
    }
    setAsking(next);
    setConfirming(true);
  };
  return (
    <div className="flex flex-wrap items-center gap-2">
      {open && !registered && tournament.playerCount < tournament.capacity ? (
        <Button variant="signal" size="sm" disabled={action.isPending} onClick={() => run(TournamentAction.Register)}>Register</Button>
      ) : null}
      {open && registered ? (
        <Button variant="outline" size="sm" disabled={action.isPending} onClick={() => run(TournamentAction.Withdraw)}>Withdraw</Button>
      ) : null}
      {open && (creator || admin) ? (
        <Button variant="signal" size="sm" className="gap-1.5" disabled={action.isPending || tournament.playerCount < 2} onClick={() => run(TournamentAction.Start)}>
          <Play aria-hidden />Start
        </Button>
      ) : null}
      {(open && (creator || admin)) || (live && admin) ? (
        <Button variant="destructive-outline" size="sm" disabled={action.isPending} onClick={() => run(TournamentAction.Cancel)}>Cancel tournament</Button>
      ) : null}
      {action.isError && !confirming ? <p role="alert" className="w-full text-sm text-destructive">That did not work. Try again.</p> : null}
      <ConfirmAction
        open={confirming}
        onOpenChange={setConfirming}
        title={asking === TournamentAction.Cancel ? `Cancel ${tournament.name}?` : `Start ${tournament.name} now?`}
        consequence={asking === TournamentAction.Cancel ? "Games in progress are stopped and the tournament ends without a winner." : "Registration closes and the first round is paired with the players already registered."}
        confirmLabel={asking === TournamentAction.Cancel ? "Cancel tournament" : "Start tournament"}
        destructive={asking === TournamentAction.Cancel}
        onConfirm={(reason) => action.mutateAsync({ action: asking, reason })}
      />
    </div>
  );
}

function Seat({ id, seed, won, names }: { id?: string; seed?: number; won: boolean; names: Names }) {
  return (
    <div className={cn("flex min-h-8 items-center gap-2 px-2.5 text-sm", won && "font-semibold text-signal")}>
      <span className="w-5 shrink-0 font-mono text-2xs text-muted-foreground">{seed ?? ""}</span>
      <span className="min-w-0 flex-1 truncate">{id ? names(id) : "TBD"}</span>
      {won ? <Crown aria-label="Winner" className="size-3.5 shrink-0" /> : null}
    </div>
  );
}

function resultLabel(match: Match) {
  if (match.result === MatchResult.BYE) return "Bye";
  if (match.result === MatchResult.ABORTED) return "Aborted";
  if (match.result === MatchResult.DRAW) return "Draw";
  if (match.result === MatchResult.WIN) return match.forfeit ? "No-show" : "Played";
  return "Playing";
}

function MatchCard({ match, tournament, names, seeds }: { match?: Match; tournament: Tournament; names: Names; seeds: Map<string, number> }) {
  const [first, second] = match?.playerIds ?? [];
  const pending = match?.result === MatchResult.PENDING;
  const ready = !!useTournamentGame(match?.sessionId, pending).data;
  const viewable = !!match?.sessionId && (pending ? ready : match.result !== MatchResult.ABORTED);
  const label = !match ? "Waiting" : pending && !ready ? "Starting…" : resultLabel(match);
  return (
    <article
      aria-label={match ? `Board ${match.board}: ${names(first)} vs ${second ? names(second) : "bye"}` : "Waiting for players"}
      data-match-result={match ? MatchResult[match.result].toLowerCase() : "waiting"}
      className="w-56 shrink-0 overflow-hidden rounded-[12px] border border-line bg-surface"
    >
      <Seat id={first} seed={first ? seeds.get(first) : undefined} won={!!first && match?.winnerId === first} names={names} />
      <div className="border-t border-line" />
      {match?.result === MatchResult.BYE ? (
        <div className="flex min-h-8 items-center px-2.5 text-sm text-muted-foreground"><span className="w-5" />Bye</div>
      ) : (
        <Seat id={second} seed={second ? seeds.get(second) : undefined} won={!!second && match?.winnerId === second} names={names} />
      )}
      <div className="flex min-h-8 items-center justify-between gap-2 border-t border-line bg-muted/40 px-2.5 text-xs text-muted-foreground">
        <span className={cn(pending && "text-signal")}>{label}</span>
        {viewable && match?.sessionId ? (
          <Link href={gameHref(match.sessionId, tournament.game)} className="inline-flex items-center gap-1 font-medium text-foreground hover:text-signal">
            <Eye aria-hidden className="size-3.5" />{pending ? "Open game" : "View game"}
          </Link>
        ) : null}
      </div>
    </article>
  );
}

function Bracket({ tournament, names, seeds }: { tournament: Tournament; names: Names; seeds: Map<string, number> }) {
  const total = tournament.totalRounds;
  const columns = Array.from({ length: total }, (_, i) => i + 1);
  return (
    <ScrollArea orientation="horizontal" className="-mx-1">
      <div className="flex gap-4 px-1 pb-3" role="list" aria-label="Bracket">
        {columns.map(number => {
          const round = tournament.rounds.find(r => r.number === number);
          const size = 2 ** (total - number);
          const matches = round ? round.matches.toSorted((a, b) => a.board - b.board) : [];
          return (
            <section key={number} role="listitem" aria-label={roundTitle(number, total, tournament.format)} className="flex flex-col gap-3">
              <h3 className="eyebrow">{roundTitle(number, total, tournament.format)}</h3>
              <div className="flex flex-1 flex-col justify-around gap-3">
                {Array.from({ length: size }, (_, i) => (
                  <MatchCard key={i} match={matches[i]} tournament={tournament} names={names} seeds={seeds} />
                ))}
              </div>
            </section>
          );
        })}
      </div>
    </ScrollArea>
  );
}

function SwissRounds({ tournament, names, seeds }: { tournament: Tournament; names: Names; seeds: Map<string, number> }) {
  return (
    <div className="space-y-4">
      {tournament.rounds.map(round => (
        <section key={round.number} aria-label={`Round ${round.number}`} className="space-y-2">
          <div className="flex items-center gap-2">
            <h3 className="eyebrow">Round {round.number}</h3>
            {round.status === RoundStatus.RUNNING ? <Badge variant="signal" dot>Playing</Badge> : null}
          </div>
          <div className="flex flex-wrap gap-3">
            {round.matches.toSorted((a, b) => a.board - b.board).map(match => (
              <MatchCard key={match.board} match={match} tournament={tournament} names={names} seeds={seeds} />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function Standings({ tournament, names }: { tournament: Tournament; names: Names }) {
  const swiss = tournament.format === TournamentFormat.SWISS;
  return (
    <ScrollArea orientation="horizontal">
      <table className="w-full min-w-[420px] text-sm" aria-label="Standings">
        <thead>
          <tr className="text-left text-xs text-muted-foreground">
            <th className="w-10 py-1.5 font-medium">#</th>
            <th className="py-1.5 font-medium">Player</th>
            {swiss ? <th className="w-14 py-1.5 text-right font-medium">Points</th> : null}
            <th className="w-10 py-1.5 text-right font-medium">W</th>
            <th className="w-10 py-1.5 text-right font-medium">D</th>
            <th className="w-10 py-1.5 text-right font-medium">L</th>
            {swiss ? null : <th className="w-24 py-1.5 text-right font-medium">Status</th>}
          </tr>
        </thead>
        <tbody>
          {tournament.standings.map(row => (
            <tr key={row.userId} className="border-t border-line" data-standing-rank={row.rank}>
              <td className="py-1.5 font-mono text-xs">{row.rank}</td>
              <td className="truncate py-1.5">{names(row.userId)}</td>
              {swiss ? <td className="py-1.5 text-right font-mono">{points(row.halfPoints)}</td> : null}
              <td className="py-1.5 text-right font-mono">{row.wins}</td>
              <td className="py-1.5 text-right font-mono">{row.draws}</td>
              <td className="py-1.5 text-right font-mono">{row.losses}</td>
              {swiss ? null : (
                <td className="py-1.5 text-right text-xs text-muted-foreground">
                  {tournament.winnerId === row.userId ? "Champion" : row.eliminated ? "Out" : "Playing"}
                </td>
              )}
            </tr>
          ))}
        </tbody>
      </table>
    </ScrollArea>
  );
}

function Registrations({ tournament, names }: { tournament: Tournament; names: Names }) {
  const listed = tournament.registrations.toSorted((a, b) => (a.seed ?? 1e9) - (b.seed ?? 1e9));
  if (!listed.length) return <p className="text-sm text-muted-foreground">Nobody yet</p>;
  return (
    <ul aria-label="Registered players" className="grid gap-1.5 sm:grid-cols-2">
      {listed.map(r => (
        <li key={r.userId} className={cn("flex min-h-9 items-center gap-2 rounded-[10px] border border-line px-2.5 text-sm", r.returned && "text-muted-foreground line-through")}>
          <span className="w-5 shrink-0 font-mono text-2xs text-muted-foreground">{r.seed ?? ""}</span>
          <span className="min-w-0 flex-1 truncate">{names(r.userId)}</span>
          <span className="font-mono text-xs text-muted-foreground">{r.rating}</span>
        </li>
      ))}
    </ul>
  );
}

export function TournamentView({ id }: { id: string }) {
  const query = useTournament(id);
  const tournament = query.data;
  const names = useNames(tournament);
  if (!tournament) {
    return (
      <Pane title="Tournament" back={tournamentsHref} backLabel="Back to tournaments">
        {query.isError ? (
          <ErrorCard title="Could not load the tournament" error={query.error} onRetry={() => void query.refetch()} retrying={query.isFetching} />
        ) : (
          <div className="space-y-3"><Skeleton className="h-24 rounded-[14px]" /><Skeleton className="h-48 rounded-[14px]" /></div>
        )}
      </Pane>
    );
  }
  const kind = gameKey(tournament.game);
  const seeds = new Map(tournament.registrations.filter(r => r.seed !== undefined).map(r => [r.userId, r.seed!]));
  const started = tournament.rounds.length > 0;
  return (
    <Pane title={tournament.name} back={tournamentsHref} backLabel="Back to tournaments" width="wide" actions={<TournamentStatusBadge status={tournament.status} />}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge>{games[kind].title}</Badge>
          <Badge>{tournamentFormatLabel(tournament.format)}</Badge>
          <Badge data-player-count={tournament.playerCount}>{tournament.playerCount}/{tournament.capacity} players</Badge>
          {started ? <Badge>Round {tournament.currentRound}/{tournament.totalRounds}</Badge> : null}
        </div>
        <Actions tournament={tournament} />
      </div>
      {tournament.status === TournamentStatus.FINISHED && tournament.winnerId ? (
        <div role="status" className="flex items-center gap-3 rounded-[14px] border border-signal/40 bg-signal/10 p-4">
          <Trophy aria-hidden className="size-5 text-signal" />
          <span className="text-sm">Winner <strong data-tournament-winner>{names(tournament.winnerId)}</strong></span>
        </div>
      ) : null}
      {started ? (
        <PaneSection id="tournament-rounds" title={tournament.format === TournamentFormat.SWISS ? "Rounds" : "Bracket"}>
          {tournament.format === TournamentFormat.SWISS
            ? <SwissRounds tournament={tournament} names={names} seeds={seeds} />
            : <Bracket tournament={tournament} names={names} seeds={seeds} />}
        </PaneSection>
      ) : null}
      {tournament.standings.length ? (
        <PaneSection id="tournament-standings" title="Standings">
          <Standings tournament={tournament} names={names} />
        </PaneSection>
      ) : null}
      <PaneSection id="tournament-players" title="Players" aside={<span className="text-xs text-muted-foreground">{tournament.playerCount}/{tournament.capacity}</span>}>
        <Registrations tournament={tournament} names={names} />
      </PaneSection>
    </Pane>
  );
}
