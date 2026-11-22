"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Hourglass, UserMinus } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, gameOptions, gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import type { QueueEntry } from "@/gen/axon/game/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { shortID } from "@/lib/format";
import { useAdminProfiles } from "@/lib/query/admin";
import { loadQueue, playKeys, useRemoveFromQueue } from "@/lib/query/play";

const games = gameOptions.filter((option) => option.value);

function queueColumns(profiles?: Map<string, PublicProfile>): ColumnDef<QueueEntry, unknown>[] {
  return [
    {
      id: "player",
      header: "Player",
      meta: { filter: { field: "user", label: "Player", kind: FilterKind.Person }, mobile: MobileSlot.Title },
      cell: ({ row }) => <Person id={row.original.userId} profiles={profiles} />,
    },
    {
      id: "game",
      header: "Game",
      meta: { filter: { field: "game", label: "Game", kind: FilterKind.Choice, options: games }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => gameTitle(row.original.game),
    },
    {
      id: "rating",
      header: "Rating",
      meta: { sort: "rating", filter: { field: "rating", label: "Rating", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{row.original.rating}</span>,
    },
    {
      id: "state",
      header: "State",
      meta: { filter: { field: "matched", label: "Matched", kind: FilterKind.Flag } },
      cell: ({ row }) => row.original.sessionId
        ? <Link href={`/games/${row.original.sessionId}`} className="inline-flex items-center gap-1.5 hover:underline"><Badge>matched</Badge><Mono>{shortID(row.original.sessionId)}</Mono></Link>
        : <Badge variant="warning" dot>waiting</Badge>,
    },
    {
      id: "joined",
      header: "Joined",
      meta: { sort: "joined", filter: { field: "joined", label: "Joined", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.joinedAt} />,
    },
    { id: "seen", header: "Last seen", cell: ({ row }) => <When at={row.original.lastSeenAt} /> },
  ];
}

const rowKey = (row: QueueEntry) => `${row.game}:${row.userId}`;

export function MatchQueue() {
  const access = useAdminAccess();
  const defs = useMemo(() => columnFilters(queueColumns()), []);
  const sorts = useMemo(() => columnSorts(queueColumns()), []);
  const state = useListState(defs);
  const list = useAdminList([...playKeys.queue, "list"], state, loadQueue, rowKey, true, 10_000);
  const profiles = useAdminProfiles(useMemo(() => list.rows.map((row) => row.userId), [list.rows]));
  const columns = useMemo(() => queueColumns(profiles.data), [profiles.data]);
  const remove = useRemoveFromQueue();
  const [target, setTarget] = useState<QueueEntry>();
  const moderate = access.can(Permission.GamesModerate);

  return (
    <AdminListPage
      title="Match queue"
      subtitle={list.total === undefined ? undefined : `${list.total.toLocaleString()} in queue`}
      toolbar={<ListToolbar state={state} sorts={sorts} />}
    >
      <DataTable
        label="Match queue"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        empty={{ icon: Hourglass, title: "Nobody is looking for a game" }}
        rowActions={moderate ? (row) => row.sessionId ? null : (
          <DropdownMenuItem className="text-destructive" onClick={() => setTarget(row)}><UserMinus aria-hidden /> Remove from queue</DropdownMenuItem>
        ) : undefined}
      />
      <ConfirmAction
        open={!!target}
        onOpenChange={(open) => { if (!open) setTarget(undefined); }}
        title={`Take this player out of the ${target ? gameTitle(target.game) : ""} queue?`}
        consequence="Their search stops right away. They can search again."
        confirmLabel="Remove from queue"
        onConfirm={(reason) => remove.mutateAsync({ userId: target?.userId ?? "", game: target?.game ?? "", reason })}
      />
    </AdminListPage>
  );
}
