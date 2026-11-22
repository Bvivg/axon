"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Paperclip } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, count, Mono, Person, When } from "@/components/admin/admin-ui";
import { columnFilters, columnSorts, DataTable, MobileSlot } from "@/components/admin/data-table";
import { ListToolbar } from "@/components/admin/list-toolbar";
import { Badge } from "@/components/ui/badge";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import type { AdminUpload } from "@/gen/axon/chat/v1/admin_pb";
import { useAdminList, useListState } from "@/lib/admin/list";
import { FilterKind } from "@/lib/admin/list-query";
import { formatBytes, shortID } from "@/lib/format";
import { loadUploads, useAdminProfiles } from "@/lib/query/admin";
import { playKeys } from "@/lib/query/play";

const kindOptions = [
  { value: "image", label: "Image" },
  { value: "video", label: "Video" },
  { value: "voice", label: "Voice" },
  { value: "attachment", label: "File" },
];

const rowKey = (row: AdminUpload) => row.id;

function uploadColumns(profiles?: Map<string, PublicProfile>): ColumnDef<AdminUpload, unknown>[] {
  return [
    {
      id: "name",
      header: "File",
      meta: { label: "Name", sort: "name", filter: { field: "name", label: "Name", kind: FilterKind.Text }, mobile: MobileSlot.Title },
      cell: ({ row }) => (
        <span className="flex min-w-0 flex-col">
          <span className="max-w-[18rem] truncate font-medium" title={row.original.name}>{row.original.name || "untitled"}</span>
          <Mono>{row.original.contentType || "unknown type"}</Mono>
        </span>
      ),
    },
    {
      id: "kind",
      header: "Kind",
      meta: { filter: { field: "kind", label: "Kind", kind: FilterKind.Choice, options: kindOptions }, mobile: MobileSlot.Subtitle },
      cell: ({ row }) => <Badge>{row.original.kind}</Badge>,
    },
    {
      id: "size",
      header: "Size",
      meta: { sort: "size", align: "end" },
      cell: ({ row }) => <span className="tabular-nums">{formatBytes(Number(row.original.sizeBytes))}</span>,
    },
    {
      id: "uploader",
      header: "Uploaded by",
      meta: { filter: { field: "uploader", label: "Uploaded by", kind: FilterKind.Person } },
      cell: ({ row }) => <Person id={row.original.uploaderId} profiles={profiles} />,
    },
    {
      id: "room",
      header: "Chat",
      meta: { filter: { field: "room", label: "Chat ID", kind: FilterKind.Id } },
      cell: ({ row }) => row.original.roomId
        ? <Link href={`/chats/${row.original.roomId}`} className="hover:underline"><Mono>{shortID(row.original.roomId)}</Mono></Link>
        : <span className="text-xs text-muted-foreground">not sent</span>,
    },
    {
      id: "created",
      header: "Uploaded",
      meta: { sort: "uploaded", filter: { field: "uploaded", label: "Uploaded", kind: FilterKind.Date } },
      cell: ({ row }) => <When at={row.original.createdAt} />,
    },
  ];
}

export function ChatUploads() {
  const defs = useMemo(() => columnFilters(uploadColumns()), []);
  const sorts = useMemo(() => columnSorts(uploadColumns()), []);
  const state = useListState(defs);
  const list = useAdminList([...playKeys.uploads, "list"], state, loadUploads, rowKey);
  const profiles = useAdminProfiles(useMemo(() => list.rows.map((row) => row.uploaderId), [list.rows]));
  const columns = useMemo(() => uploadColumns(profiles.data), [profiles.data]);

  return (
    <AdminListPage title="Uploads" subtitle={count(list.total, "file")} toolbar={<ListToolbar state={state} sorts={sorts} />}>
      <DataTable
        label="Uploads"
        columns={columns}
        list={list}
        state={state}
        rowKey={rowKey}
        empty={{ icon: Paperclip, title: "No uploads yet" }}
      />
    </AdminListPage>
  );
}
