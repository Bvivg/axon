"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Gamepad2, Square } from "lucide-react";
import Link from "next/link";
import { useMemo, useState } from "react";

import { AdminListPage, count, gameOptions, gameTitle, Mono, Person, When } from "@/components/admin/admin-ui";
import { BulkBar, bulkApply } from "@/components/admin/bulk-bar";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, SelectFilter, TextFilter, Toolbar } from "@/components/admin/filters";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { bulkLimit } from "@/lib/admin/bulk";
import { useUrlState } from "@/lib/admin/url-state";
import { gameAdminClient } from "@/lib/connect/clients";
import { shortID } from "@/lib/format";
import { adminKeys, flatten, GameStateFilter, useAdminGames, useAdminProfiles, useCancelGame, useRefreshAfterBulk } from "@/lib/query/admin";

const keys = ["state", "game", "player", "tournament"] as const;

const stateOptions = [
  { value: GameStateFilter.All, label: "All" },
  { value: GameStateFilter.Live, label: "Live" },
  { value: GameStateFilter.Waiting, label: "Waiting" },
  { value: GameStateFilter.Finished, label: "Finished" },
];

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

export function GamesTable() {
  const access = useAdminAccess();
  const [state, update, clear] = useUrlState(keys);
  const games = useAdminGames(state);
  const rows = useMemo(() => flatten(games.data), [games.data]);
  const ids = useMemo(() => rows.flatMap((session) => session.playerIds), [rows]);
  const profiles = useAdminProfiles(ids);
  const cancel = useCancelGame();
  const [target, setTarget] = useState<Session>();
  const [stopping, setStopping] = useState(false);
  const moderate = access.can(Permission.GamesModerate);
  const refresh = useRefreshAfterBulk([adminKeys.games, adminKeys.tournaments]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkStopping, setBulkStopping] = useState(false);
  const picked = rows.filter((row) => selected.has(row.id));

  const columns = useMemo<ColumnDef<Session, unknown>[]>(() => [
    { id: "game", header: "Game", cell: ({ row }) => <span className="font-medium">{gameTitle(row.original.game)}</span> },
    { id: "status", header: "Status", cell: ({ row }) => <SessionStatusBadge session={row.original} /> },
    {
      id: "players",
      header: "Players",
      cell: ({ row }) => (
        <span className="flex flex-col gap-1">
          {row.original.playerIds.map((id) => (
            <span key={id} className="flex items-center gap-1.5">
              <Person id={id} profiles={profiles.data} />
              {row.original.winnerUserId === id ? <Badge variant="success">winner</Badge> : null}
            </span>
          ))}
        </span>
      ),
    },
    { id: "moves", header: "Moves", cell: ({ row }) => <span className="tabular-nums">{row.original.ply}</span> },
    { id: "spectators", header: "Watching", cell: ({ row }) => <span className="tabular-nums">{row.original.spectators ?? 0}</span> },
    { id: "updated", header: "Updated", cell: ({ row }) => <When at={row.original.updatedAt} /> },
    {
      id: "id",
      header: "Session",
      cell: ({ row }) => (
        <span className="flex flex-col">
          <Link href={`/games/${row.original.id}`} className="hover:underline"><Mono>{shortID(row.original.id)}</Mono></Link>
          {row.original.tournamentId ? <Mono>tournament {shortID(row.original.tournamentId)}</Mono> : null}
        </span>
      ),
    },
    ...(moderate ? [{
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => finished(row.original) || row.original.tournamentId ? null : (
        <Button variant="destructive-outline" size="sm" onClick={() => { setTarget(row.original); setStopping(true); }}><Square aria-hidden /> Stop</Button>
      ),
    } satisfies ColumnDef<Session, unknown>] : []),
  ], [profiles.data, moderate]);

  return (
    <AdminListPage
      title="Games"
      subtitle={count(games.data?.pages[0]?.total, "session")}
      toolbar={
        <>
          <Toolbar>
            <ChipFilter label="State" value={state.state} options={stateOptions} onChange={(next) => update({ state: next })} />
            <SelectFilter label="Game" value={state.game} options={gameOptions} onChange={(game) => update({ game })} />
            <TextFilter label="Player" value={state.player} onChange={(player) => update({ player })} placeholder="user id" />
            <TextFilter label="Tournament" value={state.tournament} onChange={(tournament) => update({ tournament })} placeholder="tournament id" />
          </Toolbar>
          <BulkBar count={picked.length} limit={bulkLimit} onClear={() => setSelected(new Set())}>
            <Button variant="destructive-outline" size="sm" onClick={() => setBulkStopping(true)}><Square aria-hidden /> Stop {picked.length}</Button>
          </BulkBar>
        </>
      }
    >
      <DataTable
        label="Game sessions"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        rowHref={(row) => `/games/${row.id}`}
        status={games.status}
        error={games.error}
        onRetry={() => void games.refetch()}
        filtered={keys.some((key) => !!state[key])}
        onClearFilters={clear}
        empty={{ icon: Gamepad2, title: "No games yet" }}
        hasMore={games.hasNextPage}
        loadingMore={games.isFetchingNextPage}
        onLoadMore={() => void games.fetchNextPage()}
        rowHeight={64}
        selection={moderate ? { selected, onChange: setSelected, selectable: (row) => !finished(row) && !row.tournamentId, limit: bulkLimit } : undefined}
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
        open={stopping}
        onOpenChange={setStopping}
        title={`Stop this ${target ? gameTitle(target.game) : ""} game?`}
        consequence="The game ends without a result and its players return to the lobby. Ratings do not change."
        confirmLabel="Stop game"
        onConfirm={(reason) => cancel.mutateAsync({ sessionId: target?.id ?? "", reason })}
      />
    </AdminListPage>
  );
}
