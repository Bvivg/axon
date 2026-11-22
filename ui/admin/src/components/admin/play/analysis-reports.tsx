"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Cpu } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, Mono, When } from "@/components/admin/admin-ui";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, Toolbar } from "@/components/admin/filters";
import { Badge } from "@/components/ui/badge";
import type { AnalysisReportEntry } from "@/gen/axon/analysis/v1/admin_pb";
import { useUrlState } from "@/lib/admin/url-state";
import { shortID } from "@/lib/format";
import { flatten } from "@/lib/query/admin";
import { useAnalysisReports } from "@/lib/query/play";

const statusOptions = [
  { value: "", label: "All" },
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
  { id: "session", header: "Game", cell: ({ row }) => <Link href={`/games/${row.original.sessionId}`} className="hover:underline"><Mono>{shortID(row.original.sessionId)}</Mono></Link> },
  { id: "status", header: "Status", cell: ({ row }) => <ReportStatus status={row.original.status} /> },
  { id: "engine", header: "Engine", cell: ({ row }) => <span className="whitespace-nowrap">{row.original.engine || "—"} · depth {row.original.depth}</span> },
  { id: "moves", header: "Moves", cell: ({ row }) => <span className="tabular-nums">{row.original.moves}</span> },
  { id: "duration", header: "Took", cell: ({ row }) => <span className="tabular-nums">{row.original.durationMs ? `${(row.original.durationMs / 1000).toFixed(1)} s` : "—"}</span> },
  { id: "started", header: "Started", cell: ({ row }) => <When at={row.original.startedAt} /> },
  { id: "error", header: "Error", cell: ({ row }) => <span className="block max-w-[20rem] truncate text-xs text-destructive" title={row.original.error}>{row.original.error}</span> },
];

export function AnalysisReports() {
  const [state, update, clear] = useUrlState(["status"] as const);
  const reports = useAnalysisReports(state);
  const rows = useMemo(() => flatten(reports.data), [reports.data]);
  return (
    <AdminListPage title="Analysis" toolbar={<Toolbar><ChipFilter label="Status" value={state.status} options={statusOptions} onChange={(status) => update({ status })} /></Toolbar>}>
      <DataTable
        label="Analysis reports"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.sessionId}
        status={reports.status}
        error={reports.error}
        onRetry={() => void reports.refetch()}
        filtered={!!state.status}
        onClearFilters={clear}
        empty={{ icon: Cpu, title: "No games analysed yet" }}
        hasMore={reports.hasNextPage}
        loadingMore={reports.isFetchingNextPage}
        onLoadMore={() => void reports.fetchNextPage()}
      />
    </AdminListPage>
  );
}
