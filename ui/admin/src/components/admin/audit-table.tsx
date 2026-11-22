"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Download, ScrollText, X } from "lucide-react";
import Link from "next/link";
import { toast } from "sonner";
import { useMemo, useState } from "react";

import { AdminListPage, count, Mono, Person, When } from "@/components/admin/admin-ui";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogClose, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { ErrorCard } from "@/components/ui/error-card";
import { Skeleton } from "@/components/ui/skeleton";
import { AuditOutcome, type AuditEvent } from "@/gen/axon/auth/v1/admin_pb";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { describe } from "@/lib/errors";
import { shortID } from "@/lib/format";
import { cn } from "@/lib/utils";
import { adminKeys, flatten, loadAudit, useAdminProfiles, useAuditEvents, useExportAudit, type AuditFilters } from "@/lib/query/admin";

const blank: AuditFilters = { service: "", action: "", outcome: "", actor: "", targetType: "", target: "", since: "", until: "" };

export function exportFileName(at: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `axon-audit-${at.getFullYear()}${pad(at.getMonth() + 1)}${pad(at.getDate())}-${pad(at.getHours())}${pad(at.getMinutes())}.csv`;
}

function download(bytes: Uint8Array, name: string) {
  const url = URL.createObjectURL(new Blob([bytes as BlobPart], { type: "text/csv;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

function Changes({ event }: { event: AuditEvent }) {
  if (event.changes.length === 0) return null;
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-sm text-muted-foreground">Changes</span>
      <table aria-label="Changed fields" className="w-full table-fixed text-left text-xs">
        <thead className="text-muted-foreground"><tr><th scope="col" className="w-28 py-1 pr-2 font-medium">Field</th><th scope="col" className="py-1 pr-2 font-medium">Before</th><th scope="col" className="py-1 font-medium">After</th></tr></thead>
        <tbody>
          {event.changes.map((change) => (
            <tr key={change.field} className="border-t border-line align-top">
              <td className="py-1.5 pr-2 font-mono">{change.field}</td>
              <td className="py-1.5 pr-2 break-words text-destructive/90 line-through decoration-destructive/40">{change.before || "—"}</td>
              <td className="py-1.5 break-words text-success">{change.after || "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

const outcomeOptions = [
  { value: "succeeded", label: "Succeeded" },
  { value: "denied", label: "Denied" },
  { value: "failed", label: "Failed" },
];

const serviceOptions = ["auth", "chat", "game", "rating", "tournament"].map((value) => ({ value, label: value }));

const targetOptions = ["user", "role", "room", "message", "call", "game_session", "tournament", "procedure"].map((value) => ({ value, label: value }));

export function OutcomeBadge({ outcome }: { outcome: AuditOutcome }) {
  if (outcome === AuditOutcome.DENIED) return <Badge variant="warning">denied</Badge>;
  if (outcome === AuditOutcome.FAILED) return <Badge variant="destructive">failed</Badge>;
  return <Badge variant="success">succeeded</Badge>;
}

function Target({ event, profiles }: { event: AuditEvent; profiles?: Map<string, PublicProfile> }) {
  if (!event.targetType) return <span className="text-muted-foreground">—</span>;
  if (event.targetType === "user") return <Person id={event.targetId} profiles={profiles} />;
  const href = targetPages[event.targetType];
  const id = href && uuid.test(event.targetId) ? shortID(event.targetId) : event.targetId.length > 40 ? `${event.targetId.slice(0, 40)}…` : event.targetId;
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <Badge variant="mono">{event.targetType}</Badge>
      {href ? <Link href={`${href}/${event.targetId}`} className="truncate font-mono text-xs hover:underline" title={event.targetId}>{id}</Link> : <Mono className="truncate">{id}</Mono>}
    </span>
  );
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const targetPages: Record<string, string> = {
  role: "/roles",
  tournament: "/tournaments",
  game_session: "/games",
  room: "/chats",
};

function profileIDs(events: AuditEvent[]): string[] {
  return events.flatMap((event) => [event.actorId, event.targetType === "user" ? event.targetId : ""]).filter(Boolean);
}

function EventDialog({ event, profiles, onClose }: { event?: AuditEvent; profiles?: Map<string, PublicProfile>; onClose: () => void }) {
  const details = Object.entries(event?.details ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <Dialog open={!!event} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="w-[min(calc(100vw-2rem),36rem)]">
        {event ? (
          <div className="flex flex-col gap-4">
            <div className="flex items-start justify-between gap-3">
              <DialogTitle className="font-mono text-base">{event.service} · {event.action}</DialogTitle>
              <DialogClose render={<Button variant="ghost" size="icon" aria-label="Close" />}><X /></DialogClose>
            </div>
            <dl className="grid grid-cols-[8rem_1fr] gap-x-3 gap-y-2 text-sm">
              <dt className="text-muted-foreground">When</dt><dd><When at={event.occurredAt} /></dd>
              <dt className="text-muted-foreground">Outcome</dt><dd><OutcomeBadge outcome={event.outcome} /></dd>
              <dt className="text-muted-foreground">Actor</dt><dd><Person id={event.actorId} profiles={profiles} /></dd>
              <dt className="text-muted-foreground">Target</dt><dd><Target event={event} profiles={profiles} /></dd>
              <dt className="text-muted-foreground">Reason</dt><dd className="break-words whitespace-pre-wrap">{event.reason || "—"}</dd>
              <dt className="text-muted-foreground">IP</dt><dd><Mono>{event.clientIp || "—"}</Mono></dd>
              <dt className="text-muted-foreground">User agent</dt><dd className="break-words text-xs">{event.userAgent || "—"}</dd>
              <dt className="text-muted-foreground">Correlation</dt><dd><Mono className="break-all">{event.correlationId || "—"}</Mono></dd>
              <dt className="text-muted-foreground">Event ID</dt><dd><Mono className="break-all">{event.id}</Mono></dd>
              {details.map(([key, value]) => (
                <div key={key} className="contents"><dt className="font-mono text-xs text-muted-foreground">{key}</dt><dd><Mono className="break-all text-foreground">{value}</Mono></dd></div>
              ))}
            </dl>
            <Changes event={event} />
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function auditColumns(profiles: Map<string, PublicProfile> | undefined, onOpen: (event: AuditEvent) => void): ColumnDef<AuditEvent, unknown>[] {
  return [
    {
      id: "when",
      header: "When",
      meta: { sort: "when", filter: { field: "when", label: "When", kind: FilterKind.Date }, mobile: MobileSlot.Meta },
      cell: ({ row }) => <When at={row.original.occurredAt} />,
    },
    {
      id: "service",
      header: "Service",
      meta: { sort: "service", filter: { field: "service", label: "Service", kind: FilterKind.Choice, options: serviceOptions } },
      cell: ({ row }) => <Badge variant="mono">{row.original.service}</Badge>,
    },
    {
      id: "action",
      header: "Action",
      meta: { sort: "action", filter: { field: "action", label: "Action", kind: FilterKind.Text }, mobile: MobileSlot.Title },
      cell: ({ row }) => (
        <span className="flex items-center gap-1.5">
          <Button variant="link" size="sm" className="font-mono text-xs" onClick={() => onOpen(row.original)}>{row.original.action}</Button>
          {row.original.changes.length ? <Badge variant="mono" title={row.original.changes.map((c) => c.field).join(", ")}>{row.original.changes.length} Δ</Badge> : null}
        </span>
      ),
    },
    {
      id: "outcome",
      header: "Outcome",
      meta: { sort: "outcome", filter: { field: "outcome", label: "Outcome", kind: FilterKind.Choice, options: outcomeOptions }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => <OutcomeBadge outcome={row.original.outcome} />,
    },
    {
      id: "actor",
      header: "Actor",
      meta: { filter: { field: "actor", label: "Actor", kind: FilterKind.Person } },
      cell: ({ row }) => <Person id={row.original.actorId} profiles={profiles} />,
    },
    {
      id: "target",
      header: "Target",
      meta: {
        sort: "target_type",
        label: "Target type",
        filter: [
          { field: "target_type", label: "Target type", kind: FilterKind.Choice, options: targetOptions },
          { field: "target", label: "Target ID", kind: FilterKind.Text },
        ],
      },
      cell: ({ row }) => <Target event={row.original} profiles={profiles} />,
    },
    {
      id: "reason",
      header: "Reason",
      meta: {
        filter: [
          { field: "reason", label: "Reason", kind: FilterKind.Text },
          { field: "client_ip", label: "IP address", kind: FilterKind.Text },
        ],
      },
      cell: ({ row }) => <span className="line-clamp-2 max-w-[18rem] text-muted-foreground" title={row.original.reason}>{row.original.reason || "—"}</span>,
    },
  ];
}

const rowKey = (row: AuditEvent) => row.id;

export function AuditTable() {
  const defs = useMemo(() => columnFilters(auditColumns(undefined, () => {})), []);
  const sorts = useMemo(() => columnSorts(auditColumns(undefined, () => {})), []);
  const state = useListState(defs);
  const list = useAdminList([...adminKeys.audit, "list"], state, loadAudit, rowKey);
  const profiles = useAdminProfiles(useMemo(() => profileIDs(list.rows), [list.rows]));
  const [open, setOpen] = useState<AuditEvent>();
  const columns = useMemo(() => auditColumns(profiles.data, setOpen), [profiles.data]);
  const exporter = useExportAudit();

  const exportCsv = async () => {
    try {
      const result = await exporter.mutateAsync({ filter: state.filter, orderBy: state.orderBy });
      download(result.csv, exportFileName(new Date()));
      if (result.truncated) toast.warning(`Exported the newest ${result.rows.toLocaleString()} events. Narrow the filters to export the rest.`);
      else toast.success(`Exported ${result.rows.toLocaleString()} event${result.rows === 1 ? "" : "s"}`);
    } catch (err) {
      toast.error(`Export failed: ${describe(err)}`);
    }
  };

  return (
    <AdminListPage
      title="Audit log"
      subtitle={count(list.total, "event")}
      actions={<Button variant="outline" size="sm" className="pointer-coarse:h-11" onClick={() => void exportCsv()} disabled={exporter.isPending} aria-busy={exporter.isPending}><Download aria-hidden /> {exporter.isPending ? "Exporting…" : "Export CSV"}</Button>}
      toolbar={<ListToolbar state={state} sorts={sorts} />}
    >
      <DataTable
        label="Audit events"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        empty={{ icon: ScrollText, title: "No audit events yet" }}
      />
      <EventDialog event={open} profiles={profiles.data} onClose={() => setOpen(undefined)} />
    </AdminListPage>
  );
}

export function actionLabel(action: string): string {
  const words = action.replaceAll("_", " ").trim();
  return words ? words.charAt(0).toUpperCase() + words.slice(1) : action;
}

const outcomeDot: Record<AuditOutcome, string> = {
  [AuditOutcome.UNSPECIFIED]: "bg-muted-foreground",
  [AuditOutcome.SUCCEEDED]: "bg-signal",
  [AuditOutcome.DENIED]: "bg-warning",
  [AuditOutcome.FAILED]: "bg-destructive",
};

export function AuditList({ filters, showTarget = false }: { filters: Partial<AuditFilters>; showTarget?: boolean }) {
  const events = useAuditEvents({ ...blank, ...filters }, true, 10);
  const rows = useMemo(() => flatten(events.data), [events.data]);
  const ids = useMemo(() => profileIDs(rows), [rows]);
  const profiles = useAdminProfiles(ids);
  const [open, setOpen] = useState<AuditEvent>();

  if (events.isPending) return <Skeleton className="h-24 w-full rounded-xl" />;
  if (events.isError) return <ErrorCard title="Could not load the audit log" error={events.error} onRetry={() => void events.refetch()} />;
  if (rows.length === 0) return <p className="rounded-xl border border-dashed border-line px-4 py-6 text-center text-sm text-muted-foreground">Nothing recorded yet</p>;

  return (
    <>
      <ol aria-label="Recorded actions" className="flex flex-col">
        {rows.map((event, i) => (
          <li key={event.id} className="grid grid-cols-[20px_minmax(0,1fr)] gap-x-3">
            <span aria-hidden className="relative flex justify-center">
              <span className={cn("relative z-[1] mt-[7px] size-2 rounded-full ring-4 ring-background", outcomeDot[event.outcome])} />
              {i < rows.length - 1 ? <span className="absolute top-4 bottom-0 w-px bg-line" /> : null}
            </span>
            <div className="flex min-w-0 flex-col gap-1 pb-5">
              <div className="flex min-w-0 flex-wrap items-baseline gap-x-2 gap-y-0.5">
                <button type="button" className="text-sm font-medium hover:underline focus-visible:underline focus-visible:outline-none" onClick={() => setOpen(event)}>
                  {actionLabel(event.action)}
                </button>
                {event.outcome === AuditOutcome.SUCCEEDED ? null : <OutcomeBadge outcome={event.outcome} />}
                {event.changes.length ? <span className="text-xs text-muted-foreground">{event.changes.map((change) => change.field).join(", ")}</span> : null}
                <When at={event.occurredAt} className="ml-auto text-xs text-muted-foreground" />
              </div>
              {event.reason ? <p className="line-clamp-2 text-sm text-muted-foreground break-words">{event.reason}</p> : null}
              <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                {event.actorId ? <span className="inline-flex min-w-0 items-center gap-1.5">by <Person id={event.actorId} profiles={profiles.data} /></span> : <span>by the system</span>}
                {showTarget && event.targetType ? <span className="inline-flex min-w-0 items-center gap-1.5">on <Target event={event} profiles={profiles.data} /></span> : null}
              </div>
            </div>
          </li>
        ))}
      </ol>
      <EventDialog event={open} profiles={profiles.data} onClose={() => setOpen(undefined)} />
    </>
  );
}
