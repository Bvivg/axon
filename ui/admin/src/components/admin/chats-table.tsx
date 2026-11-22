"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { ExternalLink, MessagesSquare } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, count, Mono, Person, When } from "@/components/admin/admin-ui";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import { DropdownMenuLinkItem } from "@/components/ui/dropdown-menu";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import type { AdminRoom } from "@/gen/axon/chat/v1/admin_pb";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { shortID } from "@/lib/format";
import { adminKeys, loadRooms, useAdminProfiles } from "@/lib/query/admin";

const extra = ["query"] as const;

export const roomKindLabels: Record<RoomKind, string> = {
  [RoomKind.UNSPECIFIED]: "room",
  [RoomKind.OPEN]: "open",
  [RoomKind.DIRECT]: "direct",
  [RoomKind.GROUP]: "group",
};

const kindOptions = [
  { value: "group", label: "Group" },
  { value: "direct", label: "Direct" },
  { value: "open", label: "Open" },
];

export function roomTitle(room?: AdminRoom["room"]): string {
  if (!room) return "Chat";
  return room.name || (room.kind === RoomKind.DIRECT ? "Direct chat" : "Untitled chat");
}

export const roomHref = (id: string) => `/chats/${id}`;

const rowKey = (row: AdminRoom) => row.room?.id ?? "";

function roomColumns(profiles?: Map<string, PublicProfile>): ColumnDef<AdminRoom, unknown>[] {
  return [
    {
      id: "room",
      header: "Chat",
      meta: { label: "Title", sort: "title", filter: { field: "title", label: "Title", kind: FilterKind.Text }, mobile: MobileSlot.Title },
      cell: ({ row }) => <Link href={roomHref(row.original.room?.id ?? "")} className="font-medium hover:underline">{roomTitle(row.original.room)}</Link>,
    },
    {
      id: "kind",
      header: "Kind",
      meta: { filter: { field: "kind", label: "Kind", kind: FilterKind.Choice, options: kindOptions }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => <Badge variant="mono">{roomKindLabels[row.original.room?.kind ?? RoomKind.UNSPECIFIED]}</Badge>,
    },
    {
      id: "members",
      header: "Members",
      meta: {
        sort: "members",
        filter: [
          { field: "member", label: "Member", kind: FilterKind.Person },
          { field: "members", label: "Member count", kind: FilterKind.Number },
        ],
        align: "end",
      },
      cell: ({ row }) => <span className="tabular-nums">{row.original.room?.memberCount ?? 0}</span>,
    },
    {
      id: "messages",
      header: "Messages",
      meta: { sort: "messages", filter: { field: "messages", label: "Messages", kind: FilterKind.Number }, align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{row.original.lastSeq.toLocaleString()}</span>,
    },
    {
      id: "activity",
      header: "Last activity",
      meta: { sort: "last_activity", filter: { field: "last_activity", label: "Last activity", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.lastActivityAt} />,
    },
    {
      id: "creator",
      header: "Created by",
      meta: { filter: { field: "created_by", label: "Created by", kind: FilterKind.Person } },
      cell: ({ row }) => <Person id={row.original.room?.createdBy} profiles={profiles} />,
    },
    {
      id: "created",
      header: "Created",
      meta: { sort: "created", filter: { field: "created", label: "Created", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.room?.createdAt} />,
    },
    {
      id: "id",
      header: "ID",
      meta: { filter: { field: "id", label: "Chat ID", kind: FilterKind.Id }, mobile: MobileSlot.Hidden },
      cell: ({ row }) => <Mono>{shortID(row.original.room?.id ?? "")}</Mono>,
    },
  ];
}

export function ChatsTable() {
  const defs = useMemo(() => columnFilters(roomColumns()), []);
  const sorts = useMemo(() => columnSorts(roomColumns()), []);
  const state = useListState(defs, extra);
  const query = state.state.query ?? "";
  const list = useAdminList([...adminKeys.rooms, "list", query], state, (request, signal) => loadRooms(request, query, signal), rowKey);
  const profiles = useAdminProfiles(useMemo(() => list.rows.map((row) => row.room?.createdBy ?? ""), [list.rows]));
  const columns = useMemo(() => roomColumns(profiles.data), [profiles.data]);

  return (
    <AdminListPage
      title="Chats"
      subtitle={count(list.total, "chat")}
      toolbar={<ListToolbar state={state} search={{ label: "Search by chat name", key: "query" }} sorts={sorts} />}
    >
      <DataTable
        label="Chats"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        rowHref={(row) => roomHref(row.room?.id ?? "")}
        empty={{ icon: MessagesSquare, title: "No chats yet" }}
        rowActions={(row) => <DropdownMenuLinkItem render={<Link href={roomHref(row.room?.id ?? "")} />}><ExternalLink aria-hidden /> Open</DropdownMenuLinkItem>}
      />
    </AdminListPage>
  );
}
