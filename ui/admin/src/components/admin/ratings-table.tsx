"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { TrendingUp } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, count, gameOptions, Mono, Person, When } from "@/components/admin/admin-ui";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { GameName } from "@/components/admin/game-icon";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import type { RatingChangeEntry, RatingEntry } from "@/gen/axon/rating/v1/admin_pb";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { useUrlState } from "@/lib/admin/url-state";
import { shortID } from "@/lib/format";
import { useAdminProfiles } from "@/lib/query/admin";
import { loadRatingChanges, loadStandings, playKeys } from "@/lib/query/play";
import { cn } from "@/lib/utils";

enum RatingsView {
  Standings = "standings",
  Changes = "changes",
}

const games = gameOptions.filter((option) => option.value);

function Delta({ entry }: { entry: RatingChangeEntry }) {
  const delta = Math.round(entry.ratingAfter) - Math.round(entry.ratingBefore);
  return <span className={cn("tabular-nums font-medium", delta > 0 ? "text-success" : delta < 0 ? "text-destructive" : "text-muted-foreground")}>{delta > 0 ? `+${delta}` : delta}</span>;
}

function ViewSwitch({ view }: { view: RatingsView }) {
  const [, update] = useUrlState(["view", "page", "sort"] as const);
  return (
    <Tabs value={view} onValueChange={(next) => update({ view: next === RatingsView.Standings ? "" : String(next), page: "", sort: "" })}>
      <TabsList aria-label="Ratings view">
        <TabsTrigger value={RatingsView.Standings}>Standings</TabsTrigger>
        <TabsTrigger value={RatingsView.Changes}>Changes</TabsTrigger>
      </TabsList>
    </Tabs>
  );
}

export function RatingsTable() {
  const [state] = useUrlState(["view"] as const);
  return state.view === RatingsView.Changes ? <Changes /> : <Standings />;
}

function standingColumns(profiles?: Map<string, PublicProfile>): ColumnDef<RatingEntry, unknown>[] {
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
      cell: ({ row }) => <GameName game={row.original.game} />,
    },
    {
      id: "rating",
      header: "Rating",
      meta: { sort: "rating", filter: { field: "rating", label: "Rating", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums font-medium">{Math.round(row.original.rating)}</span>,
    },
    {
      id: "deviation",
      header: "Deviation",
      meta: { align: "end" },
      cell: ({ row }) => <span className="tabular-nums text-muted-foreground">±{Math.round(row.original.deviation)}</span>,
    },
    {
      id: "games",
      header: "Games",
      meta: { sort: "games", filter: { field: "games", label: "Games played", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{row.original.games}</span>,
    },
    {
      id: "record",
      header: "W / D / L",
      cell: ({ row }) => <span className="tabular-nums">{row.original.wins} / {row.original.draws} / {row.original.losses}</span>,
    },
    {
      id: "updated",
      header: "Updated",
      meta: { sort: "updated", filter: { field: "updated", label: "Updated", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.updatedAt} />,
    },
  ];
}

const standingKey = (row: RatingEntry) => `${row.game}:${row.userId}`;

function Standings() {
  const defs = useMemo(() => columnFilters(standingColumns()), []);
  const sorts = useMemo(() => columnSorts(standingColumns()), []);
  const state = useListState(defs);
  const list = useAdminList([...playKeys.standings, "list"], state, loadStandings, standingKey);
  const profiles = useAdminProfiles(useMemo(() => list.rows.map((row) => row.userId), [list.rows]));
  const columns = useMemo(() => standingColumns(profiles.data), [profiles.data]);
  return (
    <AdminListPage title="Ratings" subtitle={count(list.total, "rated player")} actions={<ViewSwitch view={RatingsView.Standings} />} toolbar={<ListToolbar state={state} sorts={sorts} />}>
      <DataTable label="Ratings" columns={columns} list={list} state={state} rowKey={standingKey} empty={{ icon: TrendingUp, title: "No rated players yet" }} />
    </AdminListPage>
  );
}

function changeColumns(profiles?: Map<string, PublicProfile>): ColumnDef<RatingChangeEntry, unknown>[] {
  return [
    {
      id: "when",
      header: "Finished",
      meta: { sort: "finished", filter: { field: "finished", label: "Finished", kind: FilterKind.Date }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => <When at={row.original.finishedAt} />,
    },
    {
      id: "player",
      header: "Player",
      meta: { filter: { field: "user", label: "Player", kind: FilterKind.Person }, mobile: MobileSlot.Title },
      cell: ({ row }) => <Person id={row.original.userId} profiles={profiles} />,
    },
    {
      id: "game",
      header: "Game",
      meta: { filter: { field: "game", label: "Game", kind: FilterKind.Choice, options: games } },
      cell: ({ row }) => <GameName game={row.original.game} />,
    },
    {
      id: "before",
      header: "Before",
      meta: { sort: "rating_before", filter: { field: "rating_before", label: "Rating before", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{Math.round(row.original.ratingBefore)}</span>,
    },
    {
      id: "after",
      header: "After",
      meta: { sort: "rating_after", filter: { field: "rating_after", label: "Rating after", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{Math.round(row.original.ratingAfter)}</span>,
    },
    {
      id: "delta",
      header: "Change",
      meta: { sort: "delta", filter: { field: "delta", label: "Change", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <Delta entry={row.original} />,
    },
    {
      id: "deviation",
      header: "Deviation",
      meta: { align: "end" },
      cell: ({ row }) => <span className="tabular-nums text-muted-foreground">±{Math.round(row.original.deviationAfter)}</span>,
    },
    {
      id: "session",
      header: "Game session",
      meta: { filter: { field: "session", label: "Session ID", kind: FilterKind.Id }, mobile: MobileSlot.Hidden },
      cell: ({ row }) => <Link href={`/games/${row.original.sessionId}`} className="hover:underline"><Mono>{shortID(row.original.sessionId)}</Mono></Link>,
    },
  ];
}

const changeKey = (row: RatingChangeEntry) => `${row.sessionId}:${row.userId}`;

function Changes() {
  const defs = useMemo(() => columnFilters(changeColumns()), []);
  const sorts = useMemo(() => columnSorts(changeColumns()), []);
  const state = useListState(defs);
  const list = useAdminList([...playKeys.changes, "list"], state, loadRatingChanges, changeKey);
  const profiles = useAdminProfiles(useMemo(() => list.rows.map((row) => row.userId), [list.rows]));
  const columns = useMemo(() => changeColumns(profiles.data), [profiles.data]);
  return (
    <AdminListPage title="Ratings" subtitle={count(list.total, "rating change")} actions={<ViewSwitch view={RatingsView.Changes} />} toolbar={<ListToolbar state={state} sorts={sorts} />}>
      <DataTable label="Rating changes" columns={columns} list={list} state={state} rowKey={changeKey} empty={{ icon: TrendingUp, title: "No rated games yet" }} />
    </AdminListPage>
  );
}
