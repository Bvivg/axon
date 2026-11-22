"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Play, Trophy, XCircle } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, gameTitle, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { TournamentStatusBadge } from "@/components/admin/tournaments/tournament-detail";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, Toolbar } from "@/components/admin/filters";
import { Button } from "@/components/ui/button";
import { TournamentFormat, TournamentStatus, type Tournament } from "@/gen/axon/tournament/v1/tournament_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useUrlState } from "@/lib/admin/url-state";
import { useAdminProfiles, useAdminTournaments, useCancelTournament, useStartTournament } from "@/lib/query/admin";

enum StatusFilter {
  All = "",
  Registration = "registration",
  Running = "running",
  Finished = "finished",
  Cancelled = "cancelled",
}

const statuses: Record<string, TournamentStatus> = {
  [StatusFilter.Registration]: TournamentStatus.REGISTRATION,
  [StatusFilter.Running]: TournamentStatus.RUNNING,
  [StatusFilter.Finished]: TournamentStatus.FINISHED,
  [StatusFilter.Cancelled]: TournamentStatus.CANCELLED,
};

const statusOptions = [
  { value: StatusFilter.All, label: "All" },
  { value: StatusFilter.Registration, label: "Registration" },
  { value: StatusFilter.Running, label: "Running" },
  { value: StatusFilter.Finished, label: "Finished" },
  { value: StatusFilter.Cancelled, label: "Cancelled" },
];

const open = (status: TournamentStatus) => status === TournamentStatus.REGISTRATION || status === TournamentStatus.SEEDING || status === TournamentStatus.RUNNING;

enum Change {
  Start = "start",
  Cancel = "cancel",
}

export function TournamentsTable() {
  const access = useAdminAccess();
  const [state, update, clear] = useUrlState(["status"] as const);
  const tournaments = useAdminTournaments(statuses[state.status]);
  const rows = useMemo(() => tournaments.data ?? [], [tournaments.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.flatMap((row) => [row.creatorId, row.winnerId ?? ""]).filter(Boolean), [rows]));
  const start = useStartTournament();
  const cancel = useCancelTournament();
  const [target, setTarget] = useState<{ tournament: Tournament; change: Change }>();
  const [confirming, setConfirming] = useState(false);
  const moderate = access.can(Permission.GamesModerate);

  const columns = useMemo<ColumnDef<Tournament, unknown>[]>(() => [
    { id: "name", header: "Tournament", cell: ({ row }) => <Link href={`/tournaments/${row.original.id}`} className="font-medium hover:underline">{row.original.name}</Link> },
    { id: "game", header: "Game", cell: ({ row }) => gameTitle(row.original.game) },
    { id: "format", header: "Format", cell: ({ row }) => row.original.format === TournamentFormat.SWISS ? "Swiss" : "Single elimination" },
    {
      id: "status",
      header: "Status",
      cell: ({ row }) => <TournamentStatusBadge status={row.original.status} />,
    },
    { id: "players", header: "Players", cell: ({ row }) => <span className="tabular-nums">{row.original.playerCount}/{row.original.capacity}</span> },
    { id: "round", header: "Round", cell: ({ row }) => <span className="tabular-nums">{row.original.currentRound}/{row.original.totalRounds || "—"}</span> },
    { id: "creator", header: "Creator", cell: ({ row }) => <Person id={row.original.creatorId} profiles={profiles.data} /> },
    { id: "winner", header: "Winner", cell: ({ row }) => <Person id={row.original.winnerId} profiles={profiles.data} /> },
    { id: "created", header: "Created", cell: ({ row }) => <When at={row.original.createdAt} /> },
    ...(moderate ? [{
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => (
        <span className="flex gap-1.5">
          {row.original.status === TournamentStatus.REGISTRATION ? (
            <Button variant="outline" size="sm" disabled={row.original.playerCount < 2} onClick={() => { setTarget({ tournament: row.original, change: Change.Start }); setConfirming(true); }}><Play aria-hidden /> Start</Button>
          ) : null}
          {open(row.original.status) ? (
            <Button variant="destructive-outline" size="sm" onClick={() => { setTarget({ tournament: row.original, change: Change.Cancel }); setConfirming(true); }}><XCircle aria-hidden /> Cancel</Button>
          ) : null}
        </span>
      ),
    } satisfies ColumnDef<Tournament, unknown>] : []),
  ], [profiles.data, moderate]);

  const name = target?.tournament.name ?? "";

  return (
    <AdminListPage
      title="Tournaments"
      toolbar={<Toolbar><ChipFilter label="Status" value={state.status} options={statusOptions} onChange={(status) => update({ status })} /></Toolbar>}
    >
      <DataTable
        label="Tournaments"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        rowHref={(row) => `/tournaments/${row.id}`}
        status={tournaments.status}
        error={tournaments.error}
        onRetry={() => void tournaments.refetch()}
        filtered={!!state.status}
        onClearFilters={clear}
        empty={{ icon: Trophy, title: "No tournaments yet" }}
      />
      <ConfirmAction
        open={confirming && target?.change === Change.Start}
        onOpenChange={setConfirming}
        title={`Start ${name} now?`}
        consequence="Registration closes and the first round is paired with the players already registered."
        confirmLabel="Start tournament"
        destructive={false}
        onConfirm={(reason) => start.mutateAsync({ tournamentId: target?.tournament.id ?? "", reason })}
      />
      <ConfirmAction
        open={confirming && target?.change === Change.Cancel}
        onOpenChange={setConfirming}
        title={`Cancel ${name}?`}
        consequence="Games in progress are stopped and the tournament ends without a winner."
        confirmLabel="Cancel tournament"
        onConfirm={(reason) => cancel.mutateAsync({ tournamentId: target?.tournament.id ?? "", reason })}
      />
    </AdminListPage>
  );
}
