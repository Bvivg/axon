"use client";

import type { ColumnDef } from "@tanstack/react-table";
import { Paperclip } from "lucide-react";
import Link from "next/link";
import { useMemo } from "react";

import { AdminListPage, Mono, Person, When } from "@/components/admin/admin-ui";
import { DataTable } from "@/components/admin/data-table";
import { ChipFilter, TextFilter, Toolbar } from "@/components/admin/filters";
import { Badge } from "@/components/ui/badge";
import type { AdminUpload } from "@/gen/axon/chat/v1/admin_pb";
import { useUrlState } from "@/lib/admin/url-state";
import { formatBytes, shortID } from "@/lib/format";
import { flatten, useAdminProfiles } from "@/lib/query/admin";
import { useChatUploads } from "@/lib/query/play";

const keys = ["kind", "uploader", "room"] as const;

const kindOptions = [
  { value: "", label: "All" },
  { value: "image", label: "Images" },
  { value: "video", label: "Video" },
  { value: "voice", label: "Voice" },
  { value: "attachment", label: "Files" },
];

export function ChatUploads() {
  const [state, update, clear] = useUrlState(keys);
  const uploads = useChatUploads(state);
  const rows = useMemo(() => flatten(uploads.data), [uploads.data]);
  const profiles = useAdminProfiles(useMemo(() => rows.map((row) => row.uploaderId), [rows]));

  const columns = useMemo<ColumnDef<AdminUpload, unknown>[]>(() => [
    { id: "name", header: "File", cell: ({ row }) => <span className="flex min-w-0 flex-col"><span className="max-w-[18rem] truncate font-medium" title={row.original.name}>{row.original.name || "untitled"}</span><Mono>{row.original.contentType || "unknown type"}</Mono></span> },
    { id: "kind", header: "Kind", cell: ({ row }) => <Badge>{row.original.kind}</Badge> },
    { id: "size", header: "Size", cell: ({ row }) => <span className="tabular-nums">{formatBytes(Number(row.original.sizeBytes))}</span> },
    { id: "uploader", header: "Uploaded by", cell: ({ row }) => <Person id={row.original.uploaderId} profiles={profiles.data} /> },
    {
      id: "room",
      header: "Chat",
      cell: ({ row }) => row.original.roomId
        ? <Link href={`/chats/${row.original.roomId}`} className="hover:underline"><Mono>{shortID(row.original.roomId)}</Mono></Link>
        : <span className="text-xs text-muted-foreground">not sent</span>,
    },
    { id: "created", header: "Uploaded", cell: ({ row }) => <When at={row.original.createdAt} /> },
  ], [profiles.data]);

  return (
    <AdminListPage
      title="Uploads"
      toolbar={
        <Toolbar>
          <ChipFilter label="Kind" value={state.kind} options={kindOptions} onChange={(kind) => update({ kind })} />
          <TextFilter label="Uploader" value={state.uploader} onChange={(uploader) => update({ uploader })} placeholder="user id" />
          <TextFilter label="Chat" value={state.room} onChange={(room) => update({ room })} placeholder="room id" />
        </Toolbar>
      }
    >
      <DataTable
        label="Uploads"
        columns={columns}
        rows={rows}
        rowKey={(row) => row.id}
        status={uploads.status}
        error={uploads.error}
        onRetry={() => void uploads.refetch()}
        filtered={keys.some((key) => !!state[key])}
        onClearFilters={clear}
        empty={{ icon: Paperclip, title: "No uploads yet" }}
        hasMore={uploads.hasNextPage}
        loadingMore={uploads.isFetchingNextPage}
        onLoadMore={() => void uploads.fetchNextPage()}
      />
    </AdminListPage>
  );
}
