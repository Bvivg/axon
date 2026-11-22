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
import { CopyID, Fact, Facts, RecordEmpty, RecordFrame, RecordIcon, RecordLoading, RecordPage, RecordRow, RecordRows, RecordSection, RecordState } from "@/components/admin/record";
import { DataTable } from "@/components/admin/data-table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";
import { ErrorCard } from "@/components/ui/error-card";
import { MemberRole, MessageKind, RoomKind, type Member, type Message } from "@/gen/axon/chat/v1/chat_pb";
import { Permission, useAdminAccess } from "@/lib/admin/access";
import { isCode } from "@/lib/errors";
import { profileName } from "@/lib/format";
import { bulkLimit } from "@/lib/admin/bulk";
import { useAdminProfiles, useAdminRoom, useAdminRoomMessages, useDeleteChatMessage, useRemoveChatMember } from "@/lib/query/admin";

const back = { section: "Chats", back: "/chats", backLabel: "Back to chats" };

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

  if (detail.isPending) return <RecordLoading {...back} label="Loading chat" />;

  if (detail.isError || !detail.data.room?.room) {
    return (
      <RecordState {...back}>
        {isCode(detail.error, Code.NotFound) ? <EmptyState icon={MessageSquareOff} title="This chat does not exist" /> : <ErrorCard title="Could not load the chat" error={detail.error} onRetry={() => void detail.refetch()} />}
      </RecordState>
    );
  }

  const admin = detail.data.room;
  const room = admin.room!;
  const title = roomTitle(room);
  const group = room.kind === RoomKind.GROUP;
  const memberCount = members?.length ?? 0;

  return (
    <RecordPage
      {...back}
      media={<RecordIcon icon={MessagesSquare} tone="muted" />}
      title={title}
      lines={
        <>
          <span className="capitalize">{roomKindLabels[room.kind]}</span>
          <span className="tabular-nums">{memberCount.toLocaleString()} member{memberCount === 1 ? "" : "s"}</span>
          <span className="tabular-nums">{admin.lastSeq.toLocaleString()} message{admin.lastSeq === BigInt(1) ? "" : "s"}</span>
        </>
      }
      facts={
        <Facts>
          <Fact term="Created by"><Person id={room.createdBy} profiles={profiles.data} /></Fact>
          <Fact term="Created"><When at={room.createdAt} /></Fact>
          <Fact term="Last activity"><When at={admin.lastActivityAt} /></Fact>
          <Fact term="Messages"><span className="tabular-nums">{admin.lastSeq.toLocaleString()}</span></Fact>
          <Fact term="Chat ID"><CopyID value={room.id} label="chat ID" /></Fact>
        </Facts>
      }
    >
      <RecordSection id="members" title="Members" count={memberCount}>
        <RecordFrame>
          {memberCount === 0 ? <RecordEmpty>Nobody is in this chat</RecordEmpty> : (
            <RecordRows label="Members">
              {members?.map((member) => (
                <RecordRow
                  key={member.userId}
                  aside={moderate && group ? (
                    <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => { setLeaving(member); setRemoving(true); }}><UserMinus aria-hidden /> Remove</Button>
                  ) : undefined}
                >
                  <Person id={member.userId} profiles={profiles.data} />
                  {member.role === MemberRole.OWNER ? <Badge variant="signal">owner</Badge> : null}
                  <span className="ml-auto hidden text-xs text-muted-foreground sm:inline">joined <When at={member.joinedAt} /></span>
                </RecordRow>
              ))}
            </RecordRows>
          )}
        </RecordFrame>
      </RecordSection>

      <RecordSection id="messages" title="Messages" aside={moderate && reason ? <Badge variant="warning" dot>Viewing is audited</Badge> : null}>
        {!moderate ? (
          <RecordFrame><EmptyState icon={Lock} title="Reading messages needs the chat moderation permission" /></RecordFrame>
        ) : !reason ? (
          <RecordFrame className="flex flex-col items-center gap-3 px-4 py-8">
            <span aria-hidden className="grid size-10 place-items-center rounded-xl bg-muted text-muted-foreground"><Lock className="size-5" /></span>
            <Button variant="outline" onClick={() => setAsking(true)}><Eye aria-hidden /> View messages</Button>
          </RecordFrame>
        ) : (
          <RecordFrame className="flex h-[32rem] flex-col">
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
          </RecordFrame>
        )}
      </RecordSection>

      {access.can(Permission.AuditRead) ? (
        <RecordSection id="history" title="Moderation history">
          <AuditList filters={{ service: "chat", targetType: "room", target: room.id }} />
        </RecordSection>
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
    </RecordPage>
  );
}
