"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { TrendingUp } from "lucide-react";
import { useMemo, type ReactNode } from "react";

import { AdminListPage, gameOptions, gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, SelectFilter, TextFilter, Toolbar } from "@/components/admin/filters";
import type { RatingChangeEntry, RatingEntry } from "@/gen/axon/rating/v1/admin_pb";
import { useUrlState } from "@/lib/admin/url-state";
import { shortID } from "@/lib/format";
import { flatten, useAdminProfiles, useRatingChanges } from "@/lib/query/admin";
import { useStandings } from "@/lib/query/play";
import { cn } from "@/lib/utils";

const keys = ["view", "game", "user"] as const;

enum RatingsView {
  Standings = "",
  Changes = "changes",
}

const viewOptions = [
  { value: RatingsView.Standings, label: "Standings" },
  { value: RatingsView.Changes, label: "Changes" },
];

function Delta({ entry }: { entry: RatingChangeEntry }) {
  const delta = Math.round(entry.ratingAfter) - Math.round(entry.ratingBefore);
  return <span className={cn("tabular-nums font-medium", delta > 0 ? "text-success" : delta < 0 ? "text-destructive" : "text-muted-foreground")}>{delta > 0 ? `+${delta}` : delta}</span>;
}

export function RatingsTable() {
  const [state, update] = useUrlState(keys);
  const standings = state.view !== RatingsView.Changes;
  const toolbar = (
    <Toolbar>
      <ChipFilter label="View" value={state.view} options={viewOptions} onChange={(view) => update({ view })} />
      <SelectFilter label="Game" value={state.game} options={gameOptions} onChange={(game) => update({ game })} />
      <TextFilter label="Player" value={state.user} onChange={(user) => update({ user })} placeholder="user id" />
    </Toolbar>
  );
  const reset = () => update({ game: "", user: "" });
  return standings
    ? <Standings filters={state} toolbar={toolbar} onClear={reset} />
    : <Changes filters={state} toolbar={toolbar} onClear={reset} />;
}

function Standings({ filters, toolbar, onClear }: { filters: { game: string; user: string }; toolbar: ReactNode; onClear: () => void }) {
  const ratings = useStandings(filters);
  const rows = useMemo(() => flatten(ratings.data), [ratings.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.map((row) => row.userId), [rows]));
  const columns = useMemo<ColumnDef<RatingEntry, unknown>[]>(() => [
    { id: "player", header: "Player", cell: ({ row }) => <Person id={row.original.userId} profiles={profiles.data} /> },
    { id: "game", header: "Game", cell: ({ row }) => gameTitle(row.original.game) },
    { id: "rating", header: "Rating", cell: ({ row }) => <span className="tabular-nums font-medium">{Math.round(row.original.rating)}</span> },
    { id: "deviation", header: "Deviation", cell: ({ row }) => <span className="tabular-nums text-muted-foreground">±{Math.round(row.original.deviation)}</span> },
    { id: "games", header: "Games", cell: ({ row }) => <span className="tabular-nums">{row.original.games}</span> },
    { id: "record", header: "W / D / L", cell: ({ row }) => <span className="tabular-nums">{row.original.wins} / {row.original.draws} / {row.original.losses}</span> },
    { id: "updated", header: "Updated", cell: ({ row }) => <When at={row.original.updatedAt} /> },
  ], [profiles.data]);
  return (
    <AdminListPage title="Ratings" toolbar={toolbar}>
      <DataTable
        label="Ratings"
        columns={columns}
        rows={rows}
        rowKey={(row) => `${row.game}:${row.userId}`}
        status={ratings.status}
        error={ratings.error}
        onRetry={() => void ratings.refetch()}
        filtered={!!(filters.game || filters.user)}
        onClearFilters={onClear}
        empty={{ icon: TrendingUp, title: "No rated players yet" }}
        hasMore={ratings.hasNextPage}
        loadingMore={ratings.isFetchingNextPage}
        onLoadMore={() => void ratings.fetchNextPage()}
      />
    </AdminListPage>
  );
}

function Changes({ filters, toolbar, onClear }: { filters: { game: string; user: string }; toolbar: ReactNode; onClear: () => void }) {
  const state = filters;
  const changes = useRatingChanges(state);
  const rows = useMemo(() => flatten(changes.data), [changes.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.map((row) => row.userId), [rows]));

  const columns = useMemo<ColumnDef<RatingChangeEntry, unknown>[]>(() => [
    { id: "when", header: "Finished", cell: ({ row }) => <When at={row.original.finishedAt} /> },
    { id: "player", header: "Player", cell: ({ row }) => <Person id={row.original.userId} profiles={profiles.data} /> },
    { id: "game", header: "Game", cell: ({ row }) => gameTitle(row.original.game) },
    { id: "before", header: "Before", cell: ({ row }) => <span className="tabular-nums">{Math.round(row.original.ratingBefore)}</span> },
    { id: "after", header: "After", cell: ({ row }) => <span className="tabular-nums">{Math.round(row.original.ratingAfter)}</span> },
    { id: "delta", header: "Change", cell: ({ row }) => <Delta entry={row.original} /> },
    { id: "deviation", header: "Deviation", cell: ({ row }) => <span className="tabular-nums text-muted-foreground">±{Math.round(row.original.deviationAfter)}</span> },
    { id: "session", header: "Session", cell: ({ row }) => <Mono>{shortID(row.original.sessionId)}</Mono> },
  ], [profiles.data]);

  return (
    <AdminListPage title="Ratings" toolbar={toolbar}>
      <DataTable
        label="Rating changes"
        columns={columns}
        rows={rows}
        rowKey={(row) => `${row.sessionId}:${row.userId}`}
        status={changes.status}
        error={changes.error}
        onRetry={() => void changes.refetch()}
        filtered={!!(state.game || state.user)}
        onClearFilters={onClear}
        empty={{ icon: TrendingUp, title: "No rated games yet" }}
        hasMore={changes.hasNextPage}
        loadingMore={changes.isFetchingNextPage}
        onLoadMore={() => void changes.fetchNextPage()}
      />
    </AdminListPage>
  );
}
