"use client";

import { Code } from "@connectrpc/connect";
import { ExternalLink, Pencil, Play, Trophy, UserMinus, XCircle } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { gameTitle, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { CopyID, Fact, Facts, RecordEmpty, RecordFrame, RecordIcon, RecordLoading, RecordNote, RecordPage, RecordRow, RecordRows, RecordSection, RecordState } from "@/components/admin/record";
import { TournamentForm } from "@/components/admin/tournaments/tournament-form";
import { Sheet } from "@/components/ui/sheet";
import { Badge } from "@/components/ui/badge";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { ScrollArea } from "@/components/ui/scroll-area";
import { TournamentFormat, TournamentStatus, type Registration } from "@/gen/axon/tournament/v1/tournament_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { profileName } from "@/lib/format";
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

const back = { section: "Tournaments", back: "/tournaments", backLabel: "Back to tournaments" };

const th = "px-4 py-2.5 font-medium";
const td = "px-4 py-3";

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

  if (tournament.isPending) return <RecordLoading {...back} label="Loading tournament" />;
  if (tournament.isError) {
    return (
      <RecordState {...back}>
        {isCode(tournament.error, Code.NotFound) ? <EmptyState icon={Trophy} title="This tournament does not exist" /> : <ErrorCard title="Could not load the tournament" error={tournament.error} onRetry={() => void tournament.refetch()} />}
      </RecordState>
    );
  }

  const t = tournament.data;
  const moderate = access.can(Permission.GamesModerate);
  const registration = t.status === TournamentStatus.REGISTRATION;
  const live = registration || t.status === TournamentStatus.SEEDING || t.status === TournamentStatus.RUNNING;
  const players = t.registrations.filter((r) => !r.returned);
  const cancelled = t.status === TournamentStatus.CANCELLED;

  return (
    <RecordPage
      {...back}
      media={<RecordIcon icon={Trophy} tone={cancelled ? "destructive" : t.status === TournamentStatus.RUNNING ? "signal" : "muted"} />}
      title={t.name}
      lines={
        <>
          <span>{gameTitle(t.game)}</span>
          <span>{t.format === TournamentFormat.SWISS ? "Swiss" : "Single elimination"}</span>
          <span className="tabular-nums">{t.playerCount} of {t.capacity} places taken</span>
        </>
      }
      tags={<TournamentStatusBadge status={t.status} />}
      actions={
        <>
          <a href={tournamentHref(t.id)} className={buttonVariants({ variant: "ghost", size: "sm" })}><ExternalLink aria-hidden /> Open in Axon</a>
          {moderate && registration ? <Button variant="outline" size="sm" onClick={() => setEditing(true)}><Pencil aria-hidden /> Edit</Button> : null}
          {moderate && registration ? <Button variant="outline" size="sm" disabled={players.length < 2} onClick={() => setPending(Pending.Start)}><Play aria-hidden /> Start</Button> : null}
          {moderate && live ? <Button variant="destructive-outline" size="sm" onClick={() => setPending(Pending.Cancel)}><XCircle aria-hidden /> Cancel</Button> : null}
        </>
      }
      facts={
        <Facts>
          <Fact term="Players"><span className="tabular-nums">{t.playerCount}/{t.capacity}</span></Fact>
          <Fact term="Round"><span className="tabular-nums">{t.currentRound}/{t.totalRounds || "—"}</span></Fact>
          <Fact term="Creator"><Person id={t.creatorId} profiles={profiles.data} /></Fact>
          <Fact term="Winner"><Person id={t.winnerId} profiles={profiles.data} /></Fact>
          <Fact term="Created"><When at={t.createdAt} /></Fact>
          <Fact term="Started"><When at={t.startedAt} /></Fact>
          <Fact term={cancelled ? "Cancelled" : "Finished"}><When at={t.finishedAt ?? t.cancelledAt} /></Fact>
          <Fact term="Tournament ID"><CopyID value={t.id} label="tournament ID" /></Fact>
        </Facts>
      }
    >
      <Sheet open={editing} onOpenChange={setEditing} title={`Edit ${t.name}`}>
        <TournamentForm tournament={t} onDone={() => setEditing(false)} />
      </Sheet>
      {!registration ? <RecordNote>Settings and the player list are locked once the tournament starts.</RecordNote> : null}

      {t.standings.length ? (
        <RecordSection id="standings" title="Standings">
          <RecordFrame>
            <ScrollArea orientation="horizontal">
              <table aria-label="Standings" className="w-full min-w-[30rem] text-left text-sm">
                <thead className="bg-surface-raised text-xs text-muted-foreground">
                  <tr>
                    <th scope="col" className={`${th} w-12 text-right`}>#</th>
                    <th scope="col" className={th}>Player</th>
                    <th scope="col" className={`${th} text-right`}>Points</th>
                    <th scope="col" className={`${th} text-right`}>W</th>
                    <th scope="col" className={`${th} text-right`}>D</th>
                    <th scope="col" className={`${th} text-right`}>L</th>
                  </tr>
                </thead>
                <tbody>
                  {t.standings.map((s) => (
                    <tr key={s.userId} className="border-t border-line">
                      <td className={`${td} text-right tabular-nums text-muted-foreground`}>{s.rank}</td>
                      <td className={td}>
                        <span className="flex min-w-0 items-center gap-2">
                          <Person id={s.userId} profiles={profiles.data} />
                          {s.eliminated ? <Badge>out</Badge> : null}
                          {t.winnerId === s.userId ? <Badge variant="signal">winner</Badge> : null}
                        </span>
                      </td>
                      <td className={`${td} text-right font-medium tabular-nums`}>{s.halfPoints / 2}</td>
                      <td className={`${td} text-right tabular-nums text-muted-foreground`}>{s.wins}</td>
                      <td className={`${td} text-right tabular-nums text-muted-foreground`}>{s.draws}</td>
                      <td className={`${td} text-right tabular-nums text-muted-foreground`}>{s.losses}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </ScrollArea>
          </RecordFrame>
        </RecordSection>
      ) : null}

      <RecordSection id="players" title="Players" count={players.length}>
        <RecordFrame>
          {players.length === 0 ? <RecordEmpty>Nobody has registered yet</RecordEmpty> : (
            <RecordRows label="Registered players">
              {players.map((r) => (
                <RecordRow
                  key={r.userId}
                  aside={moderate && registration ? (
                    <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" aria-label={`Remove ${profileName(profiles.data?.get(r.userId))}`} onClick={() => {
                      setPlayer(r);
                      setPending(Pending.Remove);
                    }}><UserMinus aria-hidden /><span className="max-sm:hidden">Remove</span></Button>
                  ) : undefined}
                >
                  <Person id={r.userId} profiles={profiles.data} />
                  <span className="ml-auto flex shrink-0 items-center gap-4 text-xs text-muted-foreground tabular-nums">
                    <span><span className="sr-only">Rating </span>{r.rating}</span>
                    <span className="max-sm:hidden">seed {r.seed ?? "—"}</span>
                    <When at={r.registeredAt} className="max-md:hidden" />
                  </span>
                </RecordRow>
              ))}
            </RecordRows>
          )}
        </RecordFrame>
      </RecordSection>

      {access.can(Permission.AuditRead) ? (
        <RecordSection id="history" title="History" aside={<Link className={buttonVariants({ variant: "link", size: "sm" })} href={`/audit?f.target_type=tournament&f.target=${t.id}`}>Open in audit log</Link>}>
          <AuditList filters={{ targetType: "tournament", target: t.id }} />
        </RecordSection>
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
    </RecordPage>
  );
}
