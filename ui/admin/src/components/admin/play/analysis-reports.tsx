"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Cpu } from "lucide-react";
import Link from "next/link";

import { AdminListPage, count, Mono, When } from "@/components/admin/admin-ui";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import type { AnalysisReportEntry } from "@/gen/axon/analysis/v1/admin_pb";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { shortID } from "@/lib/format";
import { loadAnalysis, playKeys } from "@/lib/query/play";

const statusOptions = [
  { value: "pending", label: "Pending" },
  { value: "ready", label: "Ready" },
  { value: "failed", label: "Failed" },
];

function ReportStatus({ status }: { status: string }) {
  if (status === "ready") return <Badge variant="success" dot>ready</Badge>;
  if (status === "failed") return <Badge variant="destructive" dot>failed</Badge>;
  return <Badge variant="warning" dot>{status || "pending"}</Badge>;
}

const columns: ColumnDef<AnalysisReportEntry, unknown>[] = [
  {
    id: "session",
    header: "Game",
    meta: { filter: { field: "session", label: "Game ID", kind: FilterKind.Id }, mobile: MobileSlot.Title },
    cell: ({ row }) => <Link href={`/games/${row.original.sessionId}`} className="hover:underline"><Mono>{shortID(row.original.sessionId)}</Mono></Link>,
  },
  {
    id: "status",
    header: "Status",
    meta: { filter: { field: "status", label: "Status", kind: FilterKind.Choice, options: statusOptions }, mobile: MobileSlot.Subtitle },
    cell: ({ row }) => <ReportStatus status={row.original.status} />,
  },
  { id: "engine", header: "Engine", cell: ({ row }) => <span className="whitespace-nowrap">{row.original.engine || "—"}, depth {row.original.depth}</span> },
  { id: "moves", header: "Moves", meta: { align: "end" }, cell: ({ row }) => <span className="tabular-nums">{row.original.moves}</span> },
  { id: "duration", header: "Took", meta: { align: "end" }, cell: ({ row }) => <span className="tabular-nums">{row.original.durationMs ? `${(row.original.durationMs / 1000).toFixed(1)} s` : "—"}</span> },
  {
    id: "started",
    header: "Started",
    meta: { sort: "started", filter: { field: "started", label: "Started", kind: FilterKind.Date } },
    cell: ({ row }) => <When at={row.original.startedAt} />,
  },
  {
    id: "finished",
    header: "Finished",
    meta: { sort: "finished", filter: { field: "finished", label: "Finished", kind: FilterKind.Date } },
    cell: ({ row }) => row.original.completedAt ? <When at={row.original.completedAt} /> : <span className="text-muted-foreground">—</span>,
  },
  { id: "error", header: "Error", cell: ({ row }) => <span className="block max-w-[20rem] truncate text-xs text-destructive" title={row.original.error}>{row.original.error}</span> },
];

const defs = columnFilters(columns);
const sorts = columnSorts(columns);
const rowKey = (row: AnalysisReportEntry) => row.sessionId;

export function AnalysisReports() {
  const state = useListState(defs);
  const list = useAdminList([...playKeys.analysis, "list"], state, loadAnalysis, rowKey);
  return (
    <AdminListPage title="Analysis" subtitle={count(list.total, "report")} toolbar={<ListToolbar state={state} sorts={sorts} />}>
      <DataTable
        label="Analysis reports"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        rowHref={(row) => `/games/${row.sessionId}`}
        empty={{ icon: Cpu, title: "No games analysed yet" }}
      />
    </AdminListPage>
  );
}
