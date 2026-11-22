"use client";

import { Code } from "@connectrpc/connect";
import type { ColumnDef } from "@tanstack/react-table";
import { Eye, Lock, MessageSquareOff, MessagesSquare, Trash2, UserMinus } from "lucide-react";
import { useMemo, useState } from "react";

import { AuditList } from "@/components/admin/audit-table";
import { Mono, Person, When } from "@/components/admin/admin-ui";
import { roomKindLabels, roomTitle } from "@/components/admin/chats-table";
import { BulkBar, bulkApply } from "@/components/admin/bulk-bar";
import { ConfirmAction } from "@/components/admin/confirm-action";
import { DataTable } from "@/components/admin/data-table";
import { Pane as WorkspacePage } from "@/components/ui/pane";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { PaneSection } from "@/components/ui/pane";
import { Skeleton } from "@/components/ui/skeleton";
import { MemberRole, MessageKind, RoomKind, type Member, type Message } from "@/gen/axon/chat/v1/chat_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { profileName } from "@/lib/format";
import { bulkLimit } from "@/lib/admin/bulk";
import { useAdminProfiles, useAdminRoom, useAdminRoomMessages, useDeleteChatMessage, useRemoveChatMember } from "@/lib/query/admin";

const kindLabels: Record<MessageKind, string> = {
  [MessageKind.UNSPECIFIED]: "message",
  [MessageKind.TEXT]: "text",
  [MessageKind.VOICE]: "voice message",
  [MessageKind.ATTACHMENT]: "file",
  [MessageKind.SYSTEM]: "system event",
  [MessageKind.IMAGE]: "photo",
  [MessageKind.VIDEO]: "video",
  [MessageKind.ALBUM]: "album",
};

function Body({ message }: { message: Message }) {
  if (message.deletedAt) return <span className="italic text-muted-foreground">Deleted <When at={message.deletedAt} className="text-xs" /></span>;
  if (message.kind === MessageKind.TEXT) return <span className="line-clamp-3 break-words whitespace-pre-wrap">{message.body}</span>;
  return (
    <span className="flex min-w-0 items-center gap-2">
      <Badge variant="mono">{kindLabels[message.kind]}</Badge>
      {message.body ? <span className="line-clamp-2 break-words">{message.body}</span> : null}
    </span>
  );
}

export function ChatDetail({ id }: { id: string }) {
  const access = useAdminAccess();
  const detail = useAdminRoom(id);
  const [reason, setReason] = useState("");
  const [asking, setAsking] = useState(false);
  const messages = useAdminRoomMessages(id, reason);
  const rows = useMemo(() => messages.data?.pages.flatMap((page) => [...page.items].reverse()) ?? [], [messages.data]);
  const members = detail.data?.members;
  const ids = useMemo(() => [...(members ?? []).map((member) => member.userId), ...rows.map((row) => row.authorId), detail.data?.room?.room?.createdBy ?? ""].filter(Boolean), [members, rows, detail.data]);
  const profiles = useAdminProfiles(ids);
  const remove = useRemoveChatMember();
  const deleteMessage = useDeleteChatMessage();
  const [leaving, setLeaving] = useState<Member>();
  const [removing, setRemoving] = useState(false);
  const [deleting, setDeleting] = useState<Message>();
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [bulkDeleting, setBulkDeleting] = useState(false);
  const picked = rows.filter((row) => selected.has(row.id));
  const moderate = access.can(Permission.ChatModerate);

  const columns = useMemo<ColumnDef<Message, unknown>[]>(() => [
    { id: "when", header: "Sent", cell: ({ row }) => <When at={row.original.sentAt} /> },
    { id: "author", header: "Author", cell: ({ row }) => row.original.kind === MessageKind.SYSTEM ? <Badge variant="mono">system</Badge> : <Person id={row.original.authorId} profiles={profiles.data} /> },
    { id: "body", header: "Message", cell: ({ row }) => <Body message={row.original} /> },
    { id: "seq", header: "#", cell: ({ row }) => <Mono>{row.original.seq.toString()}</Mono> },
    ...(moderate ? [{
      id: "actions",
      header: () => <span className="sr-only">Actions</span>,
      cell: ({ row }) => row.original.deletedAt || row.original.kind === MessageKind.SYSTEM ? null : (
        <Button variant="ghost" size="icon-sm" aria-label="Delete message" onClick={() => setDeleting(row.original)}><Trash2 /></Button>
      ),
    } satisfies ColumnDef<Message, unknown>] : []),
  ], [profiles.data, moderate]);

  if (detail.isPending) {
    return (
      <WorkspacePage title="Chat" back="/chats" backLabel="Back to chats" width="wide">
        <div role="status" aria-label="Loading chat" className="space-y-3"><Skeleton className="h-28 w-full" /><Skeleton className="h-40 w-full" /></div>
      </WorkspacePage>
    );
  }

  if (detail.isError || !detail.data.room?.room) {
    return (
      <WorkspacePage title="Chat" back="/chats" backLabel="Back to chats" width="wide">
        {isCode(detail.error, Code.NotFound) ? <EmptyState icon={MessageSquareOff} title="This chat does not exist" /> : <ErrorCard title="Could not load the chat" error={detail.error} onRetry={() => void detail.refetch()} />}
      </WorkspacePage>
    );
  }

  const admin = detail.data.room;
  const room = admin.room!;
  const title = roomTitle(room);
  const group = room.kind === RoomKind.GROUP;

  return (
    <WorkspacePage title={title} back="/chats" backLabel="Back to chats" width="wide">
      <PaneSection id="room" title="Chat" aside={<Badge variant="mono">{roomKindLabels[room.kind]}</Badge>}>
        <dl className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-x-6 gap-y-3 text-sm">
          <div><dt className="text-xs text-muted-foreground">Created by</dt><dd><Person id={room.createdBy} profiles={profiles.data} /></dd></div>
          <div><dt className="text-xs text-muted-foreground">Created</dt><dd><When at={room.createdAt} /></dd></div>
          <div><dt className="text-xs text-muted-foreground">Last activity</dt><dd><When at={admin.lastActivityAt} /></dd></div>
          <div><dt className="text-xs text-muted-foreground">Messages</dt><dd className="tabular-nums">{admin.lastSeq.toLocaleString()}</dd></div>
          <div><dt className="text-xs text-muted-foreground">Chat ID</dt><dd><Mono>{room.id}</Mono></dd></div>
        </dl>
      </PaneSection>

      <PaneSection id="members" title={`Members · ${members?.length ?? 0}`}>
        <ul className="divide-y divide-line">
          {members?.map((member) => (
            <li key={member.userId} className="flex items-center justify-between gap-3 py-2">
              <span className="flex min-w-0 items-center gap-2">
                <Person id={member.userId} profiles={profiles.data} />
                {member.role === MemberRole.OWNER ? <Badge variant="signal">owner</Badge> : null}
                <span className="text-xs text-muted-foreground">joined <When at={member.joinedAt} /></span>
              </span>
              {moderate && group ? <Button variant="ghost" size="sm" onClick={() => { setLeaving(member); setRemoving(true); }}><UserMinus aria-hidden /> Remove</Button> : null}
            </li>
          ))}
        </ul>
      </PaneSection>

      <PaneSection id="messages" title="Messages" aside={moderate && reason ? <Badge variant="warning">viewing is audited</Badge> : null}>
        {!moderate ? (
          <EmptyState icon={Lock} title="Reading messages needs the chat moderation permission" />
        ) : !reason ? (
          <Button variant="outline" className="self-start" onClick={() => setAsking(true)}><Eye aria-hidden /> View messages</Button>
        ) : (
          <div className="flex h-[32rem] flex-col overflow-hidden rounded-lg border border-line">
            <BulkBar count={picked.length} limit={bulkLimit} onClear={() => setSelected(new Set())}>
              <Button variant="destructive-outline" size="sm" onClick={() => setBulkDeleting(true)}><Trash2 aria-hidden /> Delete {picked.length}</Button>
            </BulkBar>
            <DataTable
              label="Messages"
              columns={columns}
              rows={rows}
              rowKey={(row) => row.id}
              status={messages.status}
              error={messages.error}
              onRetry={() => void messages.refetch()}
              filtered={false}
              empty={{ icon: MessagesSquare, title: "No messages in this chat" }}
              hasMore={messages.hasNextPage}
              loadingMore={messages.isFetchingNextPage}
              onLoadMore={() => void messages.fetchNextPage()}
              rowHeight={56}
              selection={{ selected, onChange: setSelected, selectable: (row) => !row.deletedAt && row.kind !== MessageKind.SYSTEM, limit: bulkLimit }}
            />
          </div>
        )}
      </PaneSection>

      {access.can(Permission.AuditRead) ? (
        <PaneSection id="history" title="Moderation history">
          <AuditList filters={{ service: "chat", targetType: "room", target: room.id }} />
        </PaneSection>
      ) : null}

      <ConfirmAction
        open={asking}
        onOpenChange={setAsking}
        title="Open the message history?"
        consequence="Your reason is saved with every page you load."
        confirmLabel="View messages"
        destructive={false}
        onConfirm={async (next) => setReason(next)}
      />
      <ConfirmAction
        open={removing}
        onOpenChange={setRemoving}
        title={`Remove ${profileName(profiles.data?.get(leaving?.userId ?? "")) || "this member"} from the group?`}
        consequence="They leave the group at once and lose its history. If they own it, the oldest member becomes the owner."
        confirmLabel="Remove member"
        onConfirm={(next) => remove.mutateAsync({ roomId: room.id, userId: leaving?.userId ?? "", reason: next })}
      />
      <ConfirmAction
        open={!!deleting}
        onOpenChange={(open) => !open && setDeleting(undefined)}
        title="Delete this message?"
        consequence="It disappears for everyone in the chat. This cannot be undone."
        confirmLabel="Delete message"
        onConfirm={(next) => deleteMessage.mutateAsync({ messageId: deleting?.id ?? "", reason: next })}
      />
      <ConfirmAction
        open={bulkDeleting}
        onOpenChange={setBulkDeleting}
        title={`Delete ${picked.length} message${picked.length === 1 ? "" : "s"}?`}
        consequence="They disappear for everyone in the chat. This cannot be undone. One audit entry per message."
        confirmLabel="Delete messages"
        quiet
        onConfirm={async (next) => {
          await bulkApply(picked, "Deleted", (row) => deleteMessage.mutateAsync({ messageId: row.id, reason: next }));
          setSelected(new Set());
        }}
      />
    </WorkspacePage>
  );
}
