"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ChevronLeft, Images, Info, LogOut, MoreVertical, Trash2, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";

import { ConfirmDialog } from "@/components/chat/confirm-dialog";
import { ForwardDialog } from "@/components/chat/forward-dialog";
import { GroupInfoDialog } from "@/components/chat/group-info-dialog";
import { MediaViewer } from "@/components/chat/media-viewer";
import { ComposerMode, MessageComposer, type ComposerContext } from "@/components/chat/message-composer";
import { RoomMedia } from "@/components/chat/room-media";
import { Transcript, type MessageActions } from "@/components/chat/transcript";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button, buttonVariants } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { signInPath } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { ContentKind, contentOf, membersLabel, previewOf, targetIDsOf, type NameOf } from "@/lib/chat/content";
import { usePendingUploads } from "@/lib/chat/use-pending-uploads";
import { describe } from "@/lib/errors";
import { usePageVisible } from "@/lib/hooks/use-page-visible";
import { chatKeys, useHideRoom, useHistory, useLeaveRoom, useMarkRead, useRoom } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";
import { useChatSocket, type ChatTarget } from "@/lib/ws/use-chat-socket";
import type { WireMessage } from "@/lib/ws/protocol";

const clockSkewMs = 5_000;

export function ChatRoomView({ target }: { target: ChatTarget }) {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { user, signOut } = useSession();

  const roomID = "roomId" in target ? target.roomId : undefined;
  const peerID = "peerId" in target ? target.peerId : undefined;

  const room = useRoom(roomID ?? "");
  const history = useHistory(roomID ?? "");

  const flatHistory = useMemo<WireMessage[] | undefined>(() => {
    if (!history.data) return undefined;
    return [...history.data.pages].reverse().flatMap((page) => page.messages);
  }, [history.data]);

  const roomKind = room.data?.room?.kind;
  const roomPeerID = roomKind === RoomKind.DIRECT ? room.data?.room?.peerUserId : undefined;
  const effectivePeerID = peerID ?? roomPeerID;

  const peerProfiles = usePublicProfiles(effectivePeerID ? [effectivePeerID] : []);
  const peerProfile = effectivePeerID ? peerProfiles.data?.get(effectivePeerID) : undefined;

  const avatars = useMemo(() => {
    const byID = new Map<string, string>();
    if (effectivePeerID) {
      const src = peerProfile?.avatarUrls?.small || peerProfile?.avatarUrl;
      if (src) byID.set(effectivePeerID, src);
    }
    return byID;
  }, [effectivePeerID, peerProfile]);

  const socket = useChatSocket(target, flatHistory, effectivePeerID);

  const readSeqs = useMemo(() => {
    const merged = new Map<string, number>();
    for (const member of room.data?.members ?? []) {
      if (user && member.userId !== user.id) {
        merged.set(member.userId, Number(member.lastReadSeq));
      }
    }
    for (const [id, seq] of socket.readSeqs) {
      merged.set(id, Math.max(merged.get(id) ?? 0, seq));
    }
    return merged;
  }, [room.data, socket.readSeqs, user]);

  useEffect(() => {
    if (socket.newRoomId) {
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
      router.replace(`/chat/${socket.newRoomId}`);
    }
  }, [socket.newRoomId, router, queryClient]);

  useEffect(() => {
    if (socket.revoked) {
      void signOut().then(() => router.replace(signInPath));
    }
  }, [socket.revoked, signOut, router]);

  const uploads = usePendingUploads();
  const [viewing, setViewing] = useState<WireMessage | null>(null);
  const [mediaOpen, setMediaOpen] = useState(false);
  const [dragging, setDragging] = useState(false);
  const dragDepth = useRef(0);

  const memberNames = useMemo(() => {
    const byID = new Map<string, string>();
    for (const member of room.data?.members ?? []) {
      if (member.displayName) {
        byID.set(member.userId, member.displayName);
      }
    }
    if (effectivePeerID && peerProfile?.displayName) {
      byID.set(effectivePeerID, peerProfile.displayName);
    }
    return byID;
  }, [room.data, effectivePeerID, peerProfile]);

  const strangers = useMemo(() => {
    const ids = new Set<string>();
    const note = (id: unknown) => {
      if (typeof id === "string" && id !== user?.id && !memberNames.has(id)) ids.add(id);
    };
    for (const m of socket.messages) {
      note(m.forwarded_from_author_id);
      note(m.reply_to?.author_id);
      if (m.kind === ContentKind.System) {
        note(m.payload?.actor_id);
        targetIDsOf(m.payload ?? {}).forEach(note);
      }
    }
    return [...ids];
  }, [socket.messages, memberNames, user?.id]);
  const strangerProfiles = usePublicProfiles(strangers);

  const names = useMemo(() => {
    const byID = new Map(memberNames);
    for (const [id, profile] of strangerProfiles.data ?? []) {
      if (profile.displayName && !byID.has(id)) {
        byID.set(id, profile.displayName);
      }
    }
    return byID;
  }, [memberNames, strangerProfiles.data]);

  const nameOf = useCallback<NameOf>(
    (id) => {
      if (!id) return "Someone";
      if (id === user?.id) return "You";
      return names.get(id) ?? id.slice(0, 8);
    },
    [names, user?.id],
  );

  const [context, setContext] = useState<ComposerContext | null>(null);
  const [forwarding, setForwarding] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<string | null>(null);
  const [confirmingExit, setConfirmingExit] = useState(false);
  const [infoOpen, setInfoOpen] = useState(false);

  const nameOfRef = useRef(nameOf);
  useEffect(() => {
    nameOfRef.current = nameOf;
  });

  const actions = useMemo<MessageActions>(
    () => ({
      onReply: (message) =>
        setContext({
          mode: ComposerMode.Reply,
          messageId: message.id,
          title: `Reply to ${nameOfRef.current(message.author_id)}`,
          preview: previewOf(message),
          body: message.body,
        }),
      onForward: (message) => setForwarding(message.id),
      onEdit: (message) =>
        setContext({
          mode: ComposerMode.Edit,
          messageId: message.id,
          title: "Edit message",
          preview: previewOf(message),
          body: message.body,
          bodyOptional: contentOf(message).kind !== ContentKind.Text,
        }),
      onDelete: (message) => setDeleting(message.id),
    }),
    [],
  );

  const hide = useHideRoom();
  const leave = useLeaveRoom();

  const latestSystem = useMemo(
    () => socket.messages.findLast((m) => m.kind === ContentKind.System),
    [socket.messages],
  );
  const roomLoadedAt = room.dataUpdatedAt;
  const checkedSystemID = useRef<string | undefined>(undefined);
  useEffect(() => {
    if (!roomID || !latestSystem || !roomLoadedAt || latestSystem.id === checkedSystemID.current) {
      return;
    }
    checkedSystemID.current = latestSystem.id;
    if (Date.parse(latestSystem.sent_at) < roomLoadedAt - clockSkewMs) {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: chatKeys.room(roomID) });
    void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
  }, [roomID, latestSystem, roomLoadedAt, queryClient]);

  useEffect(() => {
    if (socket.removed) {
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
      router.replace("/chat");
    }
  }, [socket.removed, queryClient, router]);

  const askedAbout = useRef(new Set<string>());
  const newestAuthor = socket.messages.at(-1)?.author_id;
  useEffect(() => {
    if (
      !roomID ||
      !newestAuthor ||
      names.has(newestAuthor) ||
      askedAbout.current.has(newestAuthor)
    ) {
      return;
    }
    askedAbout.current.add(newestAuthor);
    void room.refetch();
  }, [roomID, newestAuthor, names, room]);

  const markRead = useMarkRead();
  const markReadRef = useRef(markRead.mutate);
  useEffect(() => {
    markReadRef.current = markRead.mutate;
  });

  const latestSeq = socket.messages.at(-1)?.seq ?? 0;
  useEffect(() => {
    if (!roomID || latestSeq <= 0) {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: chatKeys.lastMessage(roomID) });
    void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    void queryClient.invalidateQueries({ queryKey: chatKeys.media(roomID) });
  }, [roomID, latestSeq, queryClient]);

  const pageVisible = usePageVisible();
  const [visibleSeq, setVisibleSeq] = useState(0);
  useEffect(() => {
    if (!roomID || visibleSeq <= 0 || !pageVisible) {
      return;
    }
    const timer = setTimeout(() => {
      markReadRef.current({ roomID, seq: visibleSeq });
    }, 1500);
    return () => clearTimeout(timer);
  }, [roomID, visibleSeq, pageVisible]);

  const fetchNextPageRef = useRef(history.fetchNextPage);
  useEffect(() => {
    fetchNextPageRef.current = history.fetchNextPage;
  });
  const onLoadOlder = useCallback(() => {
    void fetchNextPageRef.current();
  }, []);

  if (!user) {
    return (
      <main className="flex flex-1 items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  const isDirect = peerID !== undefined || roomKind === RoomKind.DIRECT;
  const isGroup = roomKind === RoomKind.GROUP;
  const title = isDirect
    ? peerProfile?.displayName || (peerID ? "New chat" : "Direct message")
    : room.data?.room?.name || "Room";

  const exit = () => {
    if (!roomID) return;
    const done = { onSuccess: () => router.replace("/chat") };
    if (isDirect) {
      hide.mutate(roomID, done);
    } else {
      leave.mutate(roomID, done);
    }
  };
  const exiting = hide.isPending || leave.isPending;
  const exitError = hide.error ?? leave.error;

  const typists = [...socket.typing].filter((id) => id !== user.id);
  const typingLine = isDirect
    ? typists.length > 0
      ? "typing…"
      : ""
    : describeTypists(typists.map((id) => names.get(id) ?? "Someone"));

  const subtitle =
    typingLine ||
    (isDirect
      ? peerProfile?.online
        ? "online"
        : lastSeen(peerProfile?.lastSeenAt)
      : socket.connected
        ? room.data?.room
          ? membersLabel(room.data.room.memberCount)
          : ""
        : "Reconnecting…");

  const carriesFiles = (event: DragEvent) => event.dataTransfer.types.includes("Files");

  const onDragEnter = (event: DragEvent) => {
    if (!carriesFiles(event) || !socket.connected) return;
    event.preventDefault();
    dragDepth.current += 1;
    setDragging(true);
  };

  const onDragOver = (event: DragEvent) => {
    if (!carriesFiles(event) || !socket.connected) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  };

  const onDragLeave = (event: DragEvent) => {
    if (!carriesFiles(event)) return;
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) {
      setDragging(false);
    }
  };

  const onDrop = (event: DragEvent) => {
    if (!carriesFiles(event)) return;
    event.preventDefault();
    dragDepth.current = 0;
    setDragging(false);
    const files = [...event.dataTransfer.files];
    if (files.length > 0 && socket.connected) {
      uploads.add(files);
    }
  };

  return (
    <main
      className="relative flex min-h-0 flex-1 flex-col px-4 py-4 md:px-6"
      onDragEnter={onDragEnter}
      onDragOver={onDragOver}
      onDragLeave={onDragLeave}
      onDrop={onDrop}
    >
      {dragging ? (
        <div className="pointer-events-none absolute inset-2 z-20 flex flex-col items-center justify-center gap-2 rounded-xl border-2 border-dashed border-signal bg-background/90 text-sm font-medium">
          <Upload aria-hidden className="size-6 text-signal" />
          Drop to attach
        </div>
      ) : null}
      <header className="flex shrink-0 items-center gap-3 border-b border-border pb-4">
        <Link
          href="/chat"
          aria-label="Back to chats"
          className={cn(buttonVariants({ variant: "ghost", size: "icon" }), "-ml-2 -mr-1 shrink-0 md:hidden")}
        >
          <ChevronLeft className="size-5" />
        </Link>
        {isDirect ? (
          <Avatar
            src={peerProfile?.avatarUrls?.small || peerProfile?.avatarUrl}
            alt=""
            fallback={(title || "?").slice(0, 1).toUpperCase()}
            className="size-10"
            sizes="40px"
          />
        ) : null}
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-lg font-semibold">
            {isGroup ? (
              <button
                type="button"
                className="max-w-full truncate text-left hover:underline"
                onClick={() => setInfoOpen(true)}
              >
                {title}
              </button>
            ) : (
              title
            )}
          </h1>
          {subtitle ? (
            <p
              className={cn(
                "truncate text-xs",
                typingLine || (isDirect && peerProfile?.online)
                  ? "text-signal"
                  : "text-muted-foreground",
              )}
            >
              {subtitle}
            </p>
          ) : null}
        </div>
        {roomID ? (
          <Button variant="ghost" size="icon" aria-label="Media" onClick={() => setMediaOpen(true)}>
            <Images />
          </Button>
        ) : null}
        {roomID && roomKind !== undefined ? (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button variant="ghost" size="icon" aria-label="Chat actions">
                  <MoreVertical />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-48">
              {isGroup ? (
                <DropdownMenuItem onClick={() => setInfoOpen(true)}>
                  <Info />
                  Group info
                </DropdownMenuItem>
              ) : null}
              <DropdownMenuItem
                onClick={() => setConfirmingExit(true)}
                className="text-destructive [&>svg]:text-destructive"
              >
                {isDirect ? <Trash2 /> : <LogOut />}
                {isDirect ? "Delete chat" : isGroup ? "Leave group" : "Leave room"}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </header>

      {room.isError ? <Alert className="mt-3">{describe(room.error)}</Alert> : null}
      {history.isError ? <Alert className="mt-3">{describe(history.error)}</Alert> : null}
      {socket.error ? <Alert className="mt-3">{socket.error}</Alert> : null}

      <Transcript
        messages={socket.messages}
        selfID={user.id}
        nameOf={nameOf}
        avatars={avatars}
        readSeqs={readSeqs}
        onVisibleSeqChange={setVisibleSeq}
        onLoadOlder={onLoadOlder}
        hasOlder={history.hasNextPage}
        loadingOlder={history.isFetchingNextPage}
        onOpenMedia={setViewing}
        actions={socket.connected ? actions : undefined}
        showNames={!isDirect}
      />

      <MessageComposer
        connected={socket.connected}
        uploads={uploads}
        context={context}
        onSend={socket.send}
        onEdit={socket.edit}
        onCancelContext={() => setContext(null)}
        onTyping={socket.notifyTyping}
      />

      <ForwardDialog
        open={forwarding !== null}
        onOpenChange={(open) => !open && setForwarding(null)}
        onPick={(targetRoomID) => {
          const openTarget = () => {
            if (targetRoomID !== roomID) {
              router.push(`/chat/${targetRoomID}`);
            }
          };
          if (forwarding && socket.forward(forwarding, targetRoomID, openTarget)) {
            setForwarding(null);
          }
        }}
      />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title="Delete message?"
        consequence="It disappears for everyone in this chat."
        action="Delete"
        onConfirm={() => {
          if (deleting && socket.remove(deleting)) {
            if (context?.messageId === deleting) setContext(null);
            setDeleting(null);
          }
        }}
      />

      <ConfirmDialog
        open={confirmingExit}
        onOpenChange={(open) => {
          if (!open) {
            setConfirmingExit(false);
            hide.reset();
            leave.reset();
          }
        }}
        title={isDirect ? "Delete this chat?" : isGroup ? "Leave this group?" : "Leave this room?"}
        consequence={
          isDirect
            ? "The history is cleared for you. The other person keeps theirs, and a new message brings the chat back."
            : isGroup
              ? "You stop receiving its messages. Only the owner can add you back."
              : "You stop receiving its messages."
        }
        action={isDirect ? "Delete chat" : "Leave"}
        pending={exiting}
        error={exitError ? describe(exitError) : null}
        onConfirm={exit}
      />

      {isGroup && roomID ? (
        <GroupInfoDialog
          open={infoOpen}
          onOpenChange={setInfoOpen}
          roomID={roomID}
          selfID={user.id}
          onLeave={() => {
            setInfoOpen(false);
            setConfirmingExit(true);
          }}
        />
      ) : null}

      <MediaViewer message={viewing} onClose={() => setViewing(null)} />
      {roomID ? (
        <RoomMedia
          roomID={roomID}
          names={names}
          selfID={user.id}
          open={mediaOpen}
          onOpenChange={setMediaOpen}
        />
      ) : null}
    </main>
  );
}

function describeTypists(names: string[]): string {
  switch (names.length) {
    case 0:
      return "";
    case 1:
      return `${names[0]} is typing…`;
    case 2:
      return `${names[0]} and ${names[1]} are typing…`;
    default:
      return `${names.length} people are typing…`;
  }
}

function lastSeen(ts?: Timestamp): string {
  if (!ts) {
    return "";
  }

  const at = new Date(Number(ts.seconds) * 1000);
  if (Number.isNaN(at.getTime())) {
    return "";
  }

  const time = at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });

  const today = new Date();
  if (at.toDateString() === today.toDateString()) {
    return `last seen at ${time}`;
  }

  const yesterday = new Date(today);
  yesterday.setDate(today.getDate() - 1);
  if (at.toDateString() === yesterday.toDateString()) {
    return `last seen yesterday at ${time}`;
  }

  const day = at.toLocaleDateString(undefined, {
    day: "numeric",
    month: "short",
    year: at.getFullYear() === today.getFullYear() ? undefined : "numeric",
  });
  return `last seen ${day} at ${time}`;
}
