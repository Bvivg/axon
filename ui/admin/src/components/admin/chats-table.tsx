"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { MessagesSquare } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, count, Mono, Person, When } from "@/components/admin/admin-ui";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, SearchFilter, TextFilter, Toolbar } from "@/components/admin/filters";
import { Badge } from "@/components/ui/badge";
import type { AdminRoom } from "@/gen/axon/chat/v1/admin_pb";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { useUrlState } from "@/lib/admin/url-state";
import { flatten, RoomKindFilter, useAdminProfiles, useAdminRooms } from "@/lib/query/admin";

const keys = ["query", "kind", "member"] as const;

const kindOptions = [
  { value: RoomKindFilter.All, label: "All" },
  { value: RoomKindFilter.Group, label: "Groups" },
  { value: RoomKindFilter.Direct, label: "Direct" },
  { value: RoomKindFilter.Open, label: "Open" },
];

export const roomKindLabels: Record<RoomKind, string> = {
  [RoomKind.UNSPECIFIED]: "room",
  [RoomKind.OPEN]: "open",
  [RoomKind.DIRECT]: "direct",
  [RoomKind.GROUP]: "group",
};

export function roomTitle(room?: AdminRoom["room"]): string {
  if (!room) return "Chat";
  return room.name || (room.kind === RoomKind.DIRECT ? "Direct chat" : "Untitled chat");
}

export const roomHref = (id: string) => `/chats/${id}`;

export function ChatsTable() {
  const [state, update, clear] = useUrlState(keys);
  const rooms = useAdminRooms(state);
  const rows = useMemo(() => flatten(rooms.data), [rooms.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.map((row) => row.room?.createdBy ?? "").filter(Boolean), [rows]));

  const columns = useMemo<ColumnDef<AdminRoom, unknown>[]>(() => [
    { id: "room", header: "Chat", cell: ({ row }) => <Link href={roomHref(row.original.room?.id ?? "")} className="font-medium hover:underline">{roomTitle(row.original.room)}</Link> },
    { id: "kind", header: "Kind", cell: ({ row }) => <Badge variant="mono">{roomKindLabels[row.original.room?.kind ?? RoomKind.UNSPECIFIED]}</Badge> },
    { id: "members", header: "Members", cell: ({ row }) => <span className="tabular-nums">{row.original.room?.memberCount ?? 0}</span> },
    { id: "messages", header: "Messages", cell: ({ row }) => <span className="tabular-nums">{row.original.lastSeq.toLocaleString()}</span> },
    { id: "activity", header: "Last activity", cell: ({ row }) => <When at={row.original.lastActivityAt} /> },
    { id: "creator", header: "Created by", cell: ({ row }) => <Person id={row.original.room?.createdBy} profiles={profiles.data} /> },
    { id: "created", header: "Created", cell: ({ row }) => <When at={row.original.room?.createdAt} /> },
    { id: "id", header: "ID", cell: ({ row }) => <Mono>{row.original.room?.id.slice(0, 8)}</Mono> },
  ], [profiles.data]);

  return (
    <AdminListPage
      title="Chats"
      subtitle={count(rooms.data?.pages[0]?.total, "chat")}
      toolbar={
        <Toolbar>
          <SearchFilter label="Search by chat name" value={state.query} onChange={(query) => update({ query })} />
          <ChipFilter label="Kind" value={state.kind} options={kindOptions} onChange={(kind) => update({ kind })} />
          <TextFilter label="Member" value={state.member} onChange={(member) => update({ member })} placeholder="user id" />
        </Toolbar>
      }
    >
      <DataTable
        label="Chats"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.room?.id ?? ""}
        rowHref={(row) => roomHref(row.room?.id ?? "")}
        status={rooms.status}
        error={rooms.error}
        onRetry={() => void rooms.refetch()}
        filtered={keys.some((key) => !!state[key])}
        onClearFilters={clear}
        empty={{ icon: MessagesSquare, title: "No chats yet" }}
        hasMore={rooms.hasNextPage}
        loadingMore={rooms.isFetchingNextPage}
        onLoadMore={() => void rooms.fetchNextPage()}
      />
    </AdminListPage>
  );
}
