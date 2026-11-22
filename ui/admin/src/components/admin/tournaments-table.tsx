"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { ExternalLink, Play, Trophy, XCircle } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, count, gameOptions, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { GameName } from "@/components/admin/game-icon";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { TournamentStatusBadge } from "@/components/admin/tournaments/tournament-detail";
import { DropdownMenuItem, DropdownMenuLinkItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { TournamentFormat, TournamentStatus, type Tournament } from "@/gen/axon/tournament/v1/tournament_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { adminKeys, useAdminProfiles, useCancelTournament, useStartTournament } from "@/lib/query/admin";
import { loadTournaments } from "@/lib/query/play";

const extra = ["query"] as const;

const statusOptions = [
  { value: "registration", label: "Registration" },
  { value: "seeding", label: "Seeding" },
  { value: "running", label: "Running" },
  { value: "finished", label: "Finished" },
  { value: "cancelled", label: "Cancelled" },
];

const formatOptions = [
  { value: "single_elimination", label: "Single elimination" },
  { value: "swiss", label: "Swiss" },
];

const games = gameOptions.filter((option) => option.value);

const open = (status: TournamentStatus) => status === TournamentStatus.REGISTRATION || status === TournamentStatus.SEEDING || status === TournamentStatus.RUNNING;

enum Change {
  Start = "start",
  Cancel = "cancel",
}

const tournamentHref = (id: string) => `/tournaments/${id}`;
const rowKey = (row: Tournament) => row.id;

function tournamentColumns(profiles?: Map<string, PublicProfile>): ColumnDef<Tournament, unknown>[] {
  return [
    {
      id: "name",
      header: "Tournament",
      meta: { label: "Name", sort: "name", filter: { field: "name", label: "Name", kind: FilterKind.Text }, mobile: MobileSlot.Title },
      cell: ({ row }) => <Link href={tournamentHref(row.original.id)} className="font-medium hover:underline">{row.original.name}</Link>,
    },
    {
      id: "game",
      header: "Game",
      meta: { filter: { field: "game", label: "Game", kind: FilterKind.Choice, options: games }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => <GameName game={row.original.game} />,
    },
    {
      id: "format",
      header: "Format",
      meta: { filter: { field: "format", label: "Format", kind: FilterKind.Choice, options: formatOptions } },
      cell: ({ row }) => row.original.format === TournamentFormat.SWISS ? "Swiss" : "Single elimination",
    },
    {
      id: "status",
      header: "Status",
      meta: { filter: { field: "status", label: "Status", kind: FilterKind.Choice, options: statusOptions } },
      cell: ({ row }) => <TournamentStatusBadge status={row.original.status} />,
    },
    {
      id: "players",
      header: "Players",
      meta: {
        sort: "players",
        filter: [
          { field: "players", label: "Players", kind: FilterKind.Number },
          { field: "capacity", label: "Capacity", kind: FilterKind.Number },
        ],
        align: "end",
      },
      cell: ({ row }) => <span className="tabular-nums">{row.original.playerCount}/{row.original.capacity}</span>,
    },
    {
      id: "round",
      header: "Round",
      meta: { align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{row.original.currentRound}/{row.original.totalRounds || "—"}</span>,
    },
    {
      id: "creator",
      header: "Creator",
      meta: { filter: { field: "creator", label: "Creator", kind: FilterKind.Person } },
      cell: ({ row }) => <Person id={row.original.creatorId} profiles={profiles} />,
    },
    { id: "winner", header: "Winner", cell: ({ row }) => <Person id={row.original.winnerId} profiles={profiles} /> },
    {
      id: "created",
      header: "Created",
      meta: { sort: "created", filter: { field: "created", label: "Created", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.createdAt} />,
    },
    {
      id: "started",
      header: "Started",
      meta: { sort: "started", filter: { field: "started", label: "Started", kind: FilterKind.Date } },
      cell: ({ row }) => row.original.startedAt ? <When at={row.original.startedAt} /> : <span className="text-muted-foreground">—</span>,
    },
  ];
}

export function TournamentsTable() {
  const access = useAdminAccess();
  const defs = useMemo(() => columnFilters(tournamentColumns()), []);
  const sorts = useMemo(() => columnSorts(tournamentColumns()), []);
  const state = useListState(defs, extra);
  const query = state.state.query ?? "";
  const list = useAdminList([...adminKeys.tournaments, "list", query], state, (request, signal) => loadTournaments(request, query, signal), rowKey);
  const profiles = useAdminProfiles(useMemo(() => list.rows.flatMap((row) => [row.creatorId, row.winnerId ?? ""]), [list.rows]));
  const columns = useMemo(() => tournamentColumns(profiles.data), [profiles.data]);
  const start = useStartTournament();
  const cancel = useCancelTournament();
  const [target, setTarget] = useState<{ tournament: Tournament; change: Change }>();
  const [confirming, setConfirming] = useState(false);
  const moderate = access.can(Permission.GamesModerate);
  const name = target?.tournament.name ?? "";
  const pick = (tournament: Tournament, change: Change) => {
    setTarget({ tournament, change });
    setConfirming(true);
  };

  return (
    <AdminListPage
      title="Tournaments"
      subtitle={count(list.total, "tournament")}
      toolbar={<ListToolbar state={state} search={{ label: "Search by tournament name", key: "query" }} sorts={sorts} />}
    >
      <DataTable
        label="Tournaments"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        rowHref={(row) => tournamentHref(row.id)}
        empty={{ icon: Trophy, title: "No tournaments yet" }}
        rowActions={(row) => (
          <>
            <DropdownMenuLinkItem render={<Link href={tournamentHref(row.id)} />}><ExternalLink aria-hidden /> Open</DropdownMenuLinkItem>
            {moderate && open(row.status) ? (
              <>
                <DropdownMenuSeparator />
                {row.status === TournamentStatus.REGISTRATION ? (
                  <DropdownMenuItem disabled={row.playerCount < 2} onClick={() => pick(row, Change.Start)}><Play aria-hidden /> Start now</DropdownMenuItem>
                ) : null}
                <DropdownMenuItem className="text-destructive" onClick={() => pick(row, Change.Cancel)}><XCircle aria-hidden /> Cancel</DropdownMenuItem>
              </>
            ) : null}
          </>
        )}
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
