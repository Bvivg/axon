"use client";

import { Code } from "@connectrpc/connect";
import { ExternalLink, Pencil, Play, Trophy, UserMinus, XCircle } from "lucide-react";
import Link from "next/link";
import { useMemo, useState, type ReactNode } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { TournamentForm } from "@/components/admin/tournaments/tournament-form";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { Pane, PaneSection } from "@/components/ui/pane";
import { ScrollArea } from "@/components/ui/scroll-area";
import { Skeleton } from "@/components/ui/skeleton";
import { TournamentFormat, TournamentStatus, type Registration } from "@/gen/axon/tournament/v1/tournament_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { tournamentHref } from "@/lib/games/catalog";
import { useAdminProfiles } from "@/lib/query/admin";
import { useAdminTournament, useRemoveTournamentPlayer, useTournamentControls } from "@/lib/query/play";

export const tournamentStatusLabels: Record<TournamentStatus, string> = {
  [TournamentStatus.UNSPECIFIED]: "unknown",
  [TournamentStatus.REGISTRATION]: "registration",
  [TournamentStatus.SEEDING]: "seeding",
  [TournamentStatus.RUNNING]: "running",
  [TournamentStatus.FINISHED]: "finished",
  [TournamentStatus.CANCELLED]: "cancelled",
};

export function TournamentStatusBadge({ status }: { status: TournamentStatus }) {
  return <Badge variant={status === TournamentStatus.RUNNING ? "signal" : status === TournamentStatus.CANCELLED ? "destructive" : "outline"} dot>{tournamentStatusLabels[status]}</Badge>;
}

function Fact({ term, children }: { term: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{term}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  );
}

enum Pending {
  None = "",
  Start = "start",
  Cancel = "cancel",
  Remove = "remove",
}

export function TournamentDetail({ id }: { id: string }) {
  const access = useAdminAccess();
  const tournament = useAdminTournament(id);
  const { start, cancel } = useTournamentControls();
  const removePlayer = useRemoveTournamentPlayer();
  const [editing, setEditing] = useState(false);
  const [pending, setPending] = useState(Pending.None);
  const [player, setPlayer] = useState<Registration>();
  const people = useMemo(() => {
    const t = tournament.data;
    if (!t) return [];
    return [t.creatorId, t.winnerId ?? "", ...t.registrations.map((r) => r.userId)].filter(Boolean);
  }, [tournament.data]);
  const profiles = useAdminProfiles(people);

  if (tournament.isPending) {
    return <Pane title="Tournament" back="/tournaments" width="wide"><div role="status" aria-label="Loading tournament" className="space-y-3"><Skeleton className="h-28 w-full" /><Skeleton className="h-40 w-full" /></div></Pane>;
  }
  if (tournament.isError) {
    return (
      <Pane title="Tournament" back="/tournaments" width="wide">
        {isCode(tournament.error, Code.NotFound) ? <EmptyState icon={Trophy} title="This tournament does not exist" /> : <ErrorCard title="Could not load the tournament" error={tournament.error} onRetry={() => void tournament.refetch()} />}
      </Pane>
    );
  }

  const t = tournament.data;
  const moderate = access.can(Permission.GamesModerate);
  const registration = t.status === TournamentStatus.REGISTRATION;
  const live = registration || t.status === TournamentStatus.SEEDING || t.status === TournamentStatus.RUNNING;
  const players = t.registrations.filter((r) => !r.returned);

  return (
    <Pane
      title={t.name}
      back="/tournaments"
      backLabel="Back to tournaments"
      width="wide"
      actions={
        <>
          <a href={tournamentHref(t.id)} className={buttonVariants({ variant: "ghost", size: "sm" })}><ExternalLink aria-hidden /> Open in Axon</a>
          {moderate && registration && !editing ? <Button variant="outline" size="sm" onClick={() => setEditing(true)}><Pencil aria-hidden /> Edit</Button> : null}
          {moderate && registration ? <Button variant="outline" size="sm" disabled={players.length < 2} onClick={() => setPending(Pending.Start)}><Play aria-hidden /> Start</Button> : null}
          {moderate && live ? <Button variant="destructive-outline" size="sm" onClick={() => setPending(Pending.Cancel)}><XCircle aria-hidden /> Cancel</Button> : null}
        </>
      }
    >
      {editing ? <TournamentForm tournament={t} onDone={() => setEditing(false)} /> : (
        <PaneSection id="tournament" title="Tournament" aside={<TournamentStatusBadge status={t.status} />}>
          <dl className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-x-6 gap-y-3">
            <Fact term="Game">{gameTitle(t.game)}</Fact>
            <Fact term="Format">{t.format === TournamentFormat.SWISS ? "Swiss" : "Single elimination"}</Fact>
            <Fact term="Players"><span className="tabular-nums">{t.playerCount}/{t.capacity}</span></Fact>
            <Fact term="Rounds"><span className="tabular-nums">{t.currentRound}/{t.totalRounds || "—"}</span></Fact>
            <Fact term="Creator"><Person id={t.creatorId} profiles={profiles.data} /></Fact>
            <Fact term="Winner"><Person id={t.winnerId} profiles={profiles.data} /></Fact>
            <Fact term="Created"><When at={t.createdAt} /></Fact>
            <Fact term="Started"><When at={t.startedAt} /></Fact>
            <Fact term="Finished"><When at={t.finishedAt ?? t.cancelledAt} /></Fact>
            <Fact term="Version"><Mono>{t.etag || "—"}</Mono></Fact>
          </dl>
          {!registration ? <p className="text-xs text-muted-foreground">Settings and the player list are locked once the tournament starts.</p> : null}
        </PaneSection>
      )}

      <PaneSection id="players" title={`Players · ${players.length}`}>
        {players.length === 0 ? <p className="text-sm text-muted-foreground">Nobody has registered yet</p> : (
          <ScrollArea orientation="horizontal">
            <table aria-label="Registered players" className="w-full min-w-[32rem] text-left text-sm">
              <thead className="text-xs text-muted-foreground">
                <tr><th scope="col" className="py-2 pr-3 font-medium">Player</th><th scope="col" className="py-2 pr-3 font-medium">Rating</th><th scope="col" className="py-2 pr-3 font-medium">Seed</th><th scope="col" className="py-2 pr-3 font-medium">Registered</th><th scope="col" className="py-2"><span className="sr-only">Actions</span></th></tr>
              </thead>
              <tbody>
                {players.map((r) => (
                  <tr key={r.userId} className="border-t border-line">
                    <td className="py-2 pr-3"><Person id={r.userId} profiles={profiles.data} /></td>
                    <td className="py-2 pr-3 tabular-nums">{r.rating}</td>
                    <td className="py-2 pr-3 tabular-nums">{r.seed ?? "—"}</td>
                    <td className="py-2 pr-3"><When at={r.registeredAt} /></td>
                    <td className="py-2 text-right">
                      {moderate && registration ? (
                        <Button variant="destructive-outline" size="sm" onClick={() => {
                          setPlayer(r);
                          setPending(Pending.Remove);
                        }}><UserMinus aria-hidden /> Remove</Button>
                      ) : null}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </ScrollArea>
        )}
      </PaneSection>

      {t.standings.length ? (
        <PaneSection id="standings" title="Standings">
          <ol className="divide-y divide-line">
            {t.standings.map((s) => (
              <li key={s.userId} className="flex items-center justify-between gap-3 py-2 text-sm">
                <span className="flex min-w-0 items-center gap-3"><span className="w-6 text-right tabular-nums text-muted-foreground">{s.rank}</span><Person id={s.userId} profiles={profiles.data} /></span>
                <span className="tabular-nums text-muted-foreground">{s.halfPoints / 2} pts · {s.wins}W {s.draws}D {s.losses}L{s.eliminated ? " · out" : ""}</span>
              </li>
            ))}
          </ol>
        </PaneSection>
      ) : null}

      {access.can(Permission.AuditRead) ? (
        <PaneSection id="history" title="History" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?targetType=tournament&target=${t.id}`}>Open in audit log</Link>}>
          <AuditList filters={{ targetType: "tournament", target: t.id }} />
        </PaneSection>
      ) : null}

      <ConfirmAction
        open={pending === Pending.Start}
        onOpenChange={(open) => setPending(open ? Pending.Start : Pending.None)}
        title={`Start ${t.name} now?`}
        consequence="Registration closes and the first round is paired with the players already registered."
        confirmLabel="Start tournament"
        destructive={false}
        onConfirm={(reason) => start.mutateAsync({ tournamentId: t.id, reason })}
      />
      <ConfirmAction
        open={pending === Pending.Cancel}
        onOpenChange={(open) => setPending(open ? Pending.Cancel : Pending.None)}
        title={`Cancel ${t.name}?`}
        consequence="Games in progress are stopped and the tournament ends without a winner."
        confirmLabel="Cancel tournament"
        onConfirm={(reason) => cancel.mutateAsync({ tournamentId: t.id, reason })}
      />
      <ConfirmAction
        open={pending === Pending.Remove && !!player}
        onOpenChange={(open) => setPending(open ? Pending.Remove : Pending.None)}
        title="Remove this player from the tournament?"
        consequence="Their place frees up. They can register again while registration is open."
        confirmLabel="Remove player"
        onConfirm={async (reason) => {
          if (!player) return;
          await removePlayer.mutateAsync({ tournamentId: t.id, userId: player.userId, reason });
        }}
      />
    </Pane>
  );
}
