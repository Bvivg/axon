"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Phone, PhoneOff } from "lucide-react";
import { useMemo, useState } from "react";

import { AdminListPage, count, Mono, Person, When } from "@/components/admin/admin-ui";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import { DropdownMenuItem } from "@/components/ui/dropdown-menu";
import { EmptyState } from "@/components/ui/empty-state";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { CallMode, CallState, type Call } from "@/gen/axon/calls/v1/calls_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { formatDuration, fromTimestamp, shortID } from "@/lib/format";
import { adminKeys, featureOff, loadCalls, useAdminProfiles, useTerminateCall } from "@/lib/query/admin";

const stateLabels: Record<CallState, string> = {
  [CallState.UNSPECIFIED]: "unknown",
  [CallState.RINGING]: "ringing",
  [CallState.CONNECTING]: "connecting",
  [CallState.ACTIVE]: "active",
  [CallState.ENDED]: "ended",
};

const stateOptions = [
  { value: "ringing", label: "Ringing" },
  { value: "connecting", label: "Connecting" },
  { value: "active", label: "Active" },
  { value: "ended", label: "Ended" },
];

const kindOptions = [
  { value: "direct", label: "Direct" },
  { value: "group", label: "Group" },
];

const modeOptions = [
  { value: "audio", label: "Audio" },
  { value: "video", label: "Video" },
];

function Duration({ call }: { call: Call }) {
  const started = fromTimestamp(call.startedAt);
  if (!started) return <span className="text-muted-foreground">—</span>;
  const ended = fromTimestamp(call.endedAt);
  return <span className="tabular-nums">{formatDuration((ended ?? new Date()).getTime() - started.getTime())}</span>;
}

const rowKey = (row: Call) => row.id;

function callColumns(profiles?: Map<string, PublicProfile>): ColumnDef<Call, unknown>[] {
  return [
    {
      id: "created",
      header: "Started",
      meta: { sort: "started", filter: { field: "started", label: "Started", kind: FilterKind.Date }, mobile: MobileSlot.Title },
      cell: ({ row }) => <When at={row.original.createdAt} />,
    },
    {
      id: "state",
      header: "State",
      meta: {
        filter: [
          { field: "active", label: "In progress", kind: FilterKind.Flag },
          { field: "state", label: "State", kind: FilterKind.Choice, options: stateOptions },
        ],
        mobile: MobileSlot.Subtitle,
      },
      cell: ({ row }) => {
        const call = row.original;
        if (call.state === CallState.ENDED) return <span className="flex flex-col gap-0.5"><Badge variant="secondary">ended</Badge><span className="text-xs text-muted-foreground">{call.endReason}</span></span>;
        return <Badge variant={call.state === CallState.ACTIVE ? "success" : "warning"} dot>{stateLabels[call.state]}</Badge>;
      },
    },
    {
      id: "kind",
      header: "Kind",
      meta: {
        filter: [
          { field: "kind", label: "Kind", kind: FilterKind.Choice, options: kindOptions },
          { field: "mode", label: "Mode", kind: FilterKind.Choice, options: modeOptions },
        ],
      },
      cell: ({ row }) => (
        <span className="flex gap-1">
          <Badge variant="mono">{row.original.kind === "group" ? "group" : "direct"}</Badge>
          <Badge variant="mono">{row.original.mode === CallMode.VIDEO ? "video" : "audio"}</Badge>
        </span>
      ),
    },
    {
      id: "participants",
      header: "Participants",
      meta: {
        sort: "participants",
        filter: [
          { field: "user", label: "Participant", kind: FilterKind.Person },
          { field: "participants", label: "Participant count", kind: FilterKind.Number },
        ],
      },
      cell: ({ row }) => (
        <span className="flex flex-col gap-1">
          {row.original.participants.map((participant) => (
            <span key={participant.userId} className="flex items-center gap-1.5">
              <Person id={participant.userId} profiles={profiles} />
              {participant.userId === row.original.callerId ? <Badge variant="outline">caller</Badge> : null}
            </span>
          ))}
        </span>
      ),
    },
    {
      id: "duration",
      header: "Duration",
      meta: { sort: "duration", filter: { field: "duration", label: "Duration, seconds", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <Duration call={row.original} />,
    },
    {
      id: "ended",
      header: "Ended",
      meta: { sort: "ended", filter: { field: "ended", label: "Ended", kind: FilterKind.Date } },
      cell: ({ row }) => row.original.endedAt ? <When at={row.original.endedAt} /> : <span className="text-muted-foreground">—</span>,
    },
    {
      id: "id",
      header: "Call",
      meta: {
        filter: [
          { field: "id", label: "Call ID", kind: FilterKind.Id },
          { field: "room", label: "Chat ID", kind: FilterKind.Id },
        ],
        mobile: MobileSlot.Hidden,
      },
      cell: ({ row }) => <Mono>{shortID(row.original.id)}</Mono>,
    },
  ];
}

export function CallsTable() {
  const access = useAdminAccess();
  const defs = useMemo(() => columnFilters(callColumns()), []);
  const sorts = useMemo(() => columnSorts(callColumns()), []);
  const state = useListState(defs);
  const list = useAdminList([...adminKeys.calls, "list"], state, loadCalls, rowKey);
  const profiles = useAdminProfiles(useMemo(() => list.rows.flatMap((call) => call.participants.map((p) => p.userId)), [list.rows]));
  const columns = useMemo(() => callColumns(profiles.data), [profiles.data]);
  const terminate = useTerminateCall();
  const [target, setTarget] = useState<Call>();
  const moderate = access.can(Permission.ChatModerate);
  const off = featureOff(list.error);

  return (
    <AdminListPage
      title="Calls"
      subtitle={off ? undefined : count(list.total, "call")}
      toolbar={off ? undefined : <ListToolbar state={state} sorts={sorts} />}
    >
      {off ? <EmptyState icon={PhoneOff} title="Calls are turned off on this server" /> : (
        <DataTable
          label="Calls"
          columns={columns}
          list={list}
          state={state}
          rowKey={rowKey}
          empty={{ icon: Phone, title: "No calls yet" }}
          rowHeight={64}
          rowActions={moderate ? (row) => row.state === CallState.ENDED ? null : (
            <DropdownMenuItem className="text-destructive" onClick={() => setTarget(row)}><PhoneOff aria-hidden /> End call</DropdownMenuItem>
          ) : undefined}
        />
      )}
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
