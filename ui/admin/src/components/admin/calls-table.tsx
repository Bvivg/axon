"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { PhoneOff, Phone } from "lucide-react";
import { useMemo, useState } from "react";

import { AdminListPage, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, TextFilter, Toolbar } from "@/components/admin/filters";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { CallMode, CallState, type Call } from "@/gen/axon/calls/v1/calls_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useUrlState } from "@/lib/admin/url-state";
import { formatDuration, fromTimestamp, shortID } from "@/lib/format";
import { CallScope, featureOff, flatten, useAdminCalls, useAdminProfiles, useTerminateCall } from "@/lib/query/admin";

const keys = ["active", "user"] as const;

const activeOptions = [
  { value: CallScope.All, label: "All" },
  { value: CallScope.Active, label: "In progress" },
];

const stateLabels: Record<CallState, string> = {
  [CallState.UNSPECIFIED]: "unknown",
  [CallState.RINGING]: "ringing",
  [CallState.CONNECTING]: "connecting",
  [CallState.ACTIVE]: "active",
  [CallState.ENDED]: "ended",
};

function Duration({ call }: { call: Call }) {
  const started = fromTimestamp(call.startedAt);
  if (!started) return <span className="text-muted-foreground">—</span>;
  const ended = fromTimestamp(call.endedAt);
  return <span className="tabular-nums">{formatDuration((ended ?? new Date()).getTime() - started.getTime())}</span>;
}

export function CallsTable() {
  const access = useAdminAccess();
  const [state, update, clear] = useUrlState(keys);
  const calls = useAdminCalls(state);
  const rows = useMemo(() => flatten(calls.data), [calls.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.flatMap((call) => call.participants.map((p) => p.userId)), [rows]));
  const terminate = useTerminateCall();
  const [target, setTarget] = useState<Call>();
  const moderate = access.can(Permission.ChatModerate);
  const active = calls.data?.pages[0]?.total;

  const columns = useMemo<ColumnDef<Call, unknown>[]>(() => [
    { id: "created", header: "Started", cell: ({ row }) => <When at={row.original.createdAt} /> },
    {
      id: "state",
      header: "State",
      cell: ({ row }) => {
        const call = row.original;
        if (call.state === CallState.ENDED) return <span className="flex flex-col gap-0.5"><Badge variant="secondary">ended</Badge><span className="text-xs text-muted-foreground">{call.endReason}</span></span>;
        return <Badge variant={call.state === CallState.ACTIVE ? "success" : "warning"} dot>{stateLabels[call.state]}</Badge>;
      },
    },
    { id: "kind", header: "Kind", cell: ({ row }) => <span className="flex gap-1"><Badge variant="mono">{row.original.kind === "group" ? "group" : "direct"}</Badge><Badge variant="mono">{row.original.mode === CallMode.VIDEO ? "video" : "audio"}</Badge></span> },
    {
      id: "participants",
      header: "Participants",
      cell: ({ row }) => (
        <span className="flex flex-col gap-1">
          {row.original.participants.map((participant) => (
            <span key={participant.userId} className="flex items-center gap-1.5">
              <Person id={participant.userId} profiles={profiles.data} />
              {participant.userId === row.original.callerId ? <Badge variant="outline">caller</Badge> : null}
            </span>
          ))}
        </span>
      ),
    },
    { id: "duration", header: "Duration", cell: ({ row }) => <Duration call={row.original} /> },
    { id: "id", header: "Call", cell: ({ row }) => <Mono>{shortID(row.original.id)}</Mono> },
    ...(moderate ? [{
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => row.original.state === CallState.ENDED ? null : (
        <Button variant="destructive-outline" size="sm" onClick={() => setTarget(row.original)}><PhoneOff aria-hidden /> End</Button>
      ),
    } satisfies ColumnDef<Call, unknown>] : []),
  ], [profiles.data, moderate]);

  return (
    <AdminListPage
      title="Calls"
      subtitle={active === undefined ? undefined : `${Number(active).toLocaleString()} in progress`}
      toolbar={
        <Toolbar>
          <ChipFilter label="State" value={state.active} options={activeOptions} onChange={(next) => update({ active: next })} />
          <TextFilter label="Participant" value={state.user} onChange={(user) => update({ user })} placeholder="user id" />
        </Toolbar>
      }
    >
      {featureOff(calls.error) ? <EmptyState icon={PhoneOff} title="Calls are turned off on this server" /> : <DataTable
        label="Calls"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        status={calls.status}
        error={calls.error}
        onRetry={() => void calls.refetch()}
        filtered={keys.some((key) => !!state[key])}
        onClearFilters={clear}
        empty={{ icon: Phone, title: "No calls yet" }}
        hasMore={calls.hasNextPage}
        loadingMore={calls.isFetchingNextPage}
        onLoadMore={() => void calls.fetchNextPage()}
        rowHeight={64}
      />}
      <ConfirmAction
        open={!!target}
        onOpenChange={(open) => !open && setTarget(undefined)}
        title="End this call?"
        consequence="Every participant is disconnected at once."
        confirmLabel="End call"
        onConfirm={(reason) => terminate.mutateAsync({ callId: target?.id ?? "", reason })}
      />
    </AdminListPage>
  );
}
