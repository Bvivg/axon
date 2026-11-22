"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { ExternalLink, Gamepad2, Square } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, gameOptions, gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { bulkApply } from "@/components/admin/bulk-bar";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { DropdownMenuItem, DropdownMenuLinkItem, DropdownMenuSeparator } from "@/components/ui/dropdown-menu";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { bulkLimit } from "@/lib/admin/bulk";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { gameAdminClient } from "@/lib/connect/clients";
import { shortID } from "@/lib/format";
import { adminKeys, loadGames, useAdminProfiles, useCancelGame, useRefreshAfterBulk } from "@/lib/query/admin";

export function SessionStatusBadge({ session }: { session: Session }) {
  switch (session.status) {
    case SessionStatus.PLAYING: return <Badge variant="signal" dot>live</Badge>;
    case SessionStatus.WAITING: return <Badge variant="warning" dot>waiting</Badge>;
    case SessionStatus.WON: return <Badge variant="secondary">{session.resigned ? "resigned" : session.timedOut ? "timed out" : "won"}</Badge>;
    case SessionStatus.DRAW: return <Badge variant="secondary">draw</Badge>;
    default: return <Badge>unknown</Badge>;
  }
}

export const finished = (session: Session) => session.status === SessionStatus.WON || session.status === SessionStatus.DRAW;

const stoppable = (session: Session) => !finished(session) && !session.tournamentId;

const statusOptions = [
  { value: "live", label: "Live" },
  { value: "waiting", label: "Waiting" },
  { value: "finished", label: "Finished" },
  { value: "won", label: "Won" },
  { value: "draw", label: "Draw" },
];

const rowKey = (row: Session) => row.id;

function gameColumns(profiles?: Map<string, PublicProfile>): ColumnDef<Session, unknown>[] {
  return [
    {
      id: "game",
      header: "Game",
      meta: { filter: { field: "game", label: "Game", kind: FilterKind.Choice, options: gameOptions.filter((option) => option.value) }, mobile: MobileSlot.Title },
      cell: ({ row }) => <span className="font-medium">{gameTitle(row.original.game)}</span>,
    },
    {
      id: "status",
      header: "Status",
      meta: { filter: { field: "status", label: "Status", kind: FilterKind.Choice, options: statusOptions } },
      cell: ({ row }) => <SessionStatusBadge session={row.original} />,
    },
    {
      id: "players",
      header: "Players",
      meta: { filter: { field: "player", label: "Player", kind: FilterKind.Person }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => (
        <span className="flex flex-col gap-1">
          {row.original.playerIds.map((id) => (
            <span key={id} className="flex items-center gap-1.5">
              <Person id={id} profiles={profiles} />
              {row.original.winnerUserId === id ? <Badge variant="success">winner</Badge> : null}
            </span>
          ))}
        </span>
      ),
    },
    {
      id: "moves",
      header: "Moves",
      meta: { sort: "moves", filter: { field: "moves", label: "Moves", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{row.original.ply}</span>,
    },
    {
      id: "rated",
      header: "Rated",
      meta: { filter: { field: "rated", label: "Rated", kind: FilterKind.Flag } },
      cell: ({ row }) => (row.original.rated ? "Rated" : <span className="text-muted-foreground">Casual</span>),
    },
    {
      id: "updated",
      header: "Updated",
      meta: { sort: "updated", filter: { field: "updated", label: "Updated", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.updatedAt} />,
    },
    {
      id: "id",
      header: "Session",
      meta: { filter: { field: "tournament", label: "Tournament ID", kind: FilterKind.Id } },
      cell: ({ row }) => (
        <span className="flex flex-col">
          <Link href={`/games/${row.original.id}`} className="hover:underline"><Mono>{shortID(row.original.id)}</Mono></Link>
          {row.original.tournamentId ? <Link href={`/tournaments/${row.original.tournamentId}`} className="hover:underline"><Mono>tournament {shortID(row.original.tournamentId)}</Mono></Link> : null}
        </span>
      ),
    },
  ];
}

export function GamesTable() {
  const access = useAdminAccess();
  const cancel = useCancelGame();
  const [target, setTarget] = useState<Session>();
  const moderate = access.can(Permission.GamesModerate);
  const refresh = useRefreshAfterBulk([adminKeys.games, adminKeys.tournaments]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkStopping, setBulkStopping] = useState(false);
  const defs = useMemo(() => columnFilters(gameColumns()), []);
  const sorts = useMemo(() => columnSorts(gameColumns()), []);
  const state = useListState(defs);
  const list = useAdminList([...adminKeys.games, "list"], state, loadGames, rowKey);
  const ids = useMemo(() => list.rows.flatMap((session) => session.playerIds), [list.rows]);
  const profiles = useAdminProfiles(ids);
  const columns = useMemo(() => gameColumns(profiles.data), [profiles.data]);
  const picked = list.rows.filter((row) => selected.has(row.id));

  return (
    <AdminListPage
      title="Games"
      subtitle={list.total !== undefined ? `${list.total.toLocaleString()} session${list.total === 1 ? "" : "s"}` : undefined}
      toolbar={<ListToolbar state={state} sorts={sorts} />}
    >
      <DataTable
        label="Game sessions"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        rowHref={(row) => `/games/${row.id}`}
        empty={{ icon: Gamepad2, title: "No games yet" }}
        rowHeight={64}
        selection={moderate ? { selected, onChange: setSelected, selectable: stoppable, limit: bulkLimit } : undefined}
        bulkActions={<Button variant="destructive-outline" size="sm" className="pointer-coarse:h-11" onClick={() => setBulkStopping(true)}><Square aria-hidden /> Stop {picked.length}</Button>}
        rowActions={(row) => (
          <>
            <DropdownMenuLinkItem render={<Link href={`/games/${row.id}`} />}><ExternalLink aria-hidden /> Open</DropdownMenuLinkItem>
            {moderate && stoppable(row) ? (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem className="text-destructive" onClick={() => setTarget(row)}><Square aria-hidden /> Stop</DropdownMenuItem>
              </>
            ) : null}
          </>
        )}
      />
      <ConfirmAction
        open={bulkStopping}
        onOpenChange={setBulkStopping}
        title={`Stop ${picked.length} game${picked.length === 1 ? "" : "s"}?`}
        consequence="Each game ends without a result and its players return to the lobby. One audit entry per game."
        confirmLabel="Stop games"
        quiet
        onConfirm={async (reason) => {
          try {
            await bulkApply(picked, "Stopped", (row) => gameAdminClient.cancelGameSession({ sessionId: row.id, reason }));
            setSelected(new Set());
          } finally {
            await refresh();
          }
        }}
      />
      <ConfirmAction
        open={!!target}
        onOpenChange={(open) => !open && setTarget(undefined)}
        title={`Stop this ${target ? gameTitle(target.game) : ""} game?`}
        consequence="The game ends without a result and its players return to the lobby. Ratings do not change."
        confirmLabel="Stop game"
        onConfirm={async (reason) => {
          await cancel.mutateAsync({ sessionId: target?.id ?? "", reason });
          await refresh();
        }}
      />
    </AdminListPage>
  );
}
