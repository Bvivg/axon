"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Hourglass, UserMinus } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, gameOptions, gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { DataTable } from "@/components/admin/data-table";
import { SelectFilter, Toolbar } from "@/components/admin/filters";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type { QueueEntry } from "@/gen/axon/game/v1/admin_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useUrlState } from "@/lib/admin/url-state";
import { shortID } from "@/lib/format";
import { useAdminProfiles } from "@/lib/query/admin";
import { useMatchQueue, useRemoveFromQueue } from "@/lib/query/play";

export function MatchQueue() {
  const access = useAdminAccess();
  const [state, update, clear] = useUrlState(["game"] as const);
  const queue = useMatchQueue(state.game);
  const rows = useMemo(() => queue.data ?? [], [queue.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.map((row) => row.userId), [rows]));
  const remove = useRemoveFromQueue();
  const [target, setTarget] = useState<QueueEntry>();
  const moderate = access.can(Permission.GamesModerate);
  const waiting = rows.filter((row) => !row.sessionId).length;

  const columns = useMemo<ColumnDef<QueueEntry, unknown>[]>(() => [
    { id: "player", header: "Player", cell: ({ row }) => <Person id={row.original.userId} profiles={profiles.data} /> },
    { id: "game", header: "Game", cell: ({ row }) => gameTitle(row.original.game) },
    { id: "rating", header: "Rating", cell: ({ row }) => <span className="tabular-nums">{row.original.rating}</span> },
    {
      id: "state",
      header: "State",
      cell: ({ row }) => row.original.sessionId
        ? <Link href={`/games/${row.original.sessionId}`} className="inline-flex items-center gap-1.5 hover:underline"><Badge>matched</Badge><Mono>{shortID(row.original.sessionId)}</Mono></Link>
        : <Badge variant="warning" dot>waiting</Badge>,
    },
    { id: "joined", header: "Joined", cell: ({ row }) => <When at={row.original.joinedAt} /> },
    { id: "seen", header: "Last seen", cell: ({ row }) => <When at={row.original.lastSeenAt} /> },
    ...(moderate ? [{
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => row.original.sessionId ? null : (
        <Button variant="destructive-outline" size="sm" onClick={() => setTarget(row.original)}><UserMinus aria-hidden /> Remove</Button>
      ),
    } satisfies ColumnDef<QueueEntry, unknown>] : []),
  ], [profiles.data, moderate]);

  return (
    <AdminListPage
      title="Match queue"
      subtitle={queue.data ? `${waiting} waiting` : undefined}
      toolbar={<Toolbar><SelectFilter label="Game" value={state.game} options={gameOptions} onChange={(game) => update({ game })} /></Toolbar>}
    >
      <DataTable
        label="Match queue"
        columns={columns}
        rows={rows}
        rowKey={(row) => `${row.game}:${row.userId}`}
        status={queue.status}
        error={queue.error}
        onRetry={() => void queue.refetch()}
        filtered={!!state.game}
        onClearFilters={clear}
        empty={{ icon: Hourglass, title: "Nobody is looking for a game" }}
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
