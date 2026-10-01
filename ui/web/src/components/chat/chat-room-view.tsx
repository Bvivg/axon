"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { ChevronLeft, Images, Info, Loader2, LogOut, MoreVertical, Trash2, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";

import { ConfirmDialog } from "@/components/chat/confirm-dialog";
import { ForwardDialog } from "@/components/chat/forward-dialog";
import { GroupInfoDialog } from "@/components/chat/group-info-dialog";
import { MediaViewer } from "@/components/chat/media-viewer";
import { ReadersDialog } from "@/components/chat/readers-dialog";
import { SelectionBar } from "@/components/chat/selection-bar";
import { ComposerMode, MessageComposer, type ComposerContext } from "@/components/chat/message-composer";
import { RoomMedia } from "@/components/chat/room-media";
import { Transcript, type MessageActions, type TranscriptHandle } from "@/components/chat/transcript";
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
import { copyImage } from "@/lib/chat/copy-image";
import {
  forwardRequest,
  selectedCount,
  selectedMessages,
  selectionText,
  toggleItem,
  toggleMessage,
  type ForwardRequest,
  type MessageSelection,
} from "@/lib/chat/selection";
import { slideIndexOf, viewerSlidesOf } from "@/lib/chat/viewer";
import { usePendingUploads } from "@/lib/chat/use-pending-uploads";
import { describe } from "@/lib/errors";
import { firstUnreadOf } from "@/lib/chat/series";
import { useAttention } from "@/lib/hooks/use-attention";
import {
  chatKeys,
  refreshNewestPage,
  useForwardMessages,
  useHideRoom,
  useHistory,
  useLeaveRoom,
  useMarkRead,
  useRoom,
} from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";
import { refreshSoon } from "@/lib/query/refresh";
import { cn } from "@/lib/utils";
import { useChatSocket, type ChatTarget } from "@/lib/ws/use-chat-socket";
import { newClientID, type WireMessage } from "@/lib/ws/protocol";

const clockSkewMs = 5_000;
const markReadDebounceMs = 250;
const maxPagesToReveal = 20;
const throttledRetryMs = 1_000;

const nextFrames = (count: number) =>
  new Promise<void>((resolve) => {
    const step = (left: number) => (left <= 0 ? resolve() : requestAnimationFrame(() => step(left - 1)));
    step(count);
  });

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
  const [viewing, setViewing] = useState<{ messageID: string; item: number } | null>(null);
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
  const [forwarding, setForwarding] = useState<ForwardRequest | null>(null);
  const [deleting, setDeleting] = useState<string[] | null>(null);
  const [readersOf, setReadersOf] = useState<string | null>(null);
  const [selection, setSelection] = useState<MessageSelection | null>(null);
  const [selectionRoom, setSelectionRoom] = useState(roomID);
  if (selectionRoom !== roomID) {
    setSelectionRoom(roomID);
    setSelection(null);
  }
  const forwardMessages = useForwardMessages();
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
      onForward: (message) => setForwarding({ messageIds: [message.id], parts: [] }),
      onEdit: (message) =>
        setContext({
          mode: ComposerMode.Edit,
          messageId: message.id,
          title: "Edit message",
          preview: previewOf(message),
          body: message.body,
          bodyOptional: contentOf(message).kind !== ContentKind.Text,
        }),
      onDelete: (message) => setDeleting([message.id]),
      onSelect: (message) => setSelection(new Map([[message.id, null]])),
      onReaders: (message) => setReadersOf(message.id),
      onCopyImage: (message) => {
        const content = contentOf(message);
        if (content.kind !== ContentKind.Image) return;
        copyImage(content.image.url).then(
          () => toast.success("Image copied"),
          (error: unknown) => toast.error(error instanceof Error ? error.message : "The image could not be copied."),
        );
      },
    }),
    [],
  );

  const onToggleSelected = useCallback(
    (messageID: string) => setSelection((current) => toggleMessage(current, messageID)),
    [],
  );
  const onToggleItem = useCallback(
    (messageID: string, item: number, total: number) =>
      setSelection((current) => toggleItem(current, messageID, item, total)),
    [],
  );
  const slides = useMemo(() => viewerSlidesOf(socket.messages), [socket.messages]);
  const onOpenMedia = useCallback((message: WireMessage, item: number) => setViewing({ messageID: message.id, item }), []);
  const selected = useMemo(() => selectedMessages(selection, socket.messages), [selection, socket.messages]);
  const canDeleteSelected = selected.length > 0 && selected.every((message) => message.author_id === user?.id);

  const dialogOpen = forwarding !== null || deleting !== null || readersOf !== null;
  useEffect(() => {
    if (selection === null || dialogOpen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !event.defaultPrevented) setSelection(null);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selection, dialogOpen]);

  const copySelected = () => {
    const text = selectionText(selected, nameOf);
    void navigator.clipboard?.writeText(text).then(
      () => toast.success(selected.length === 1 ? "Message copied" : `${selected.length} messages copied`),
      () => toast.error("The messages could not be copied."),
    );
    setSelection(null);
  };

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
    refreshSoon(queryClient, chatKeys.rooms);
    refreshSoon(queryClient, chatKeys.media(roomID));
  }, [roomID, latestSeq, queryClient]);

  const myReadSeq = Number(room.data?.members.find((member) => member.userId === user?.id)?.lastReadSeq ?? 0);
  const [opened, setOpened] = useState<{ roomID: string; readSeq: number; unread: boolean } | null>(null);
  if (roomID && room.data && room.isFetchedAfterMount && opened?.roomID !== roomID) {
    setOpened({ roomID, readSeq: myReadSeq, unread: Number(room.data.room?.unreadCount ?? 0) > 0 });
  }
  const openedHere = opened && opened.roomID === roomID ? opened : null;

  const [freshFor, setFreshFor] = useState<string | null>(null);
  const historyFetchedHere = history.isFetchedAfterMount;
  useEffect(() => {
    if (!roomID || !openedHere?.unread || freshFor === roomID || historyFetchedHere) return;
    let cancelled = false;
    void refreshNewestPage(queryClient, roomID).finally(() => {
      if (!cancelled) setFreshFor(roomID);
    });
    return () => {
      cancelled = true;
    };
  }, [roomID, openedHere, freshFor, historyFetchedHere, queryClient]);
  const historyReady =
    flatHistory !== undefined && (!openedHere?.unread || historyFetchedHere || freshFor === roomID);
  const firstUnreadSeq = useMemo(
    () => (openedHere?.unread && user ? firstUnreadOf(socket.messages, openedHere.readSeq, user.id) : null),
    [openedHere, socket.messages, user],
  );

  const attentive = useAttention();
  const [visibleSeq, setVisibleSeq] = useState(0);
  const markedSeq = useRef(0);
  useEffect(() => {
    markedSeq.current = 0;
  }, [roomID]);
  useEffect(() => {
    if (!roomID || !attentive || visibleSeq <= Math.max(myReadSeq, markedSeq.current)) {
      return;
    }
    const timer = setTimeout(() => {
      markedSeq.current = visibleSeq;
      markReadRef.current({ roomID, seq: visibleSeq });
    }, markReadDebounceMs);
    return () => clearTimeout(timer);
  }, [roomID, visibleSeq, myReadSeq, attentive]);

  const fetchNextPageRef = useRef(history.fetchNextPage);
  useEffect(() => {
    fetchNextPageRef.current = history.fetchNextPage;
  });
  const onLoadOlder = useCallback(() => {
    void fetchNextPageRef.current();
  }, []);

  const transcript = useRef<TranscriptHandle>(null);
  const [jumpTo, setJumpTo] = useState<{ id: string } | null>(null);
  const onJumpTo = useCallback((id: string) => setJumpTo({ id }), []);
  useEffect(() => {
    if (!jumpTo) return;
    let cancelled = false;
    void (async () => {
      let pages = 0;
      for (let attempt = 0; pages < maxPagesToReveal && attempt < maxPagesToReveal * 2 && !cancelled; attempt += 1) {
        if (transcript.current?.reveal(jumpTo.id)) break;
        const result = await fetchNextPageRef.current();
        if (cancelled) break;
        if (result.isError) {
          if (ConnectError.from(result.error).code !== Code.ResourceExhausted) break;
          await new Promise((resolve) => setTimeout(resolve, throttledRetryMs));
          continue;
        }
        pages += 1;
        await nextFrames(2);
        if (!result.hasNextPage) {
          if (!cancelled) transcript.current?.reveal(jumpTo.id);
          break;
        }
      }
      if (!cancelled) setJumpTo(null);
    })();
    return () => {
      cancelled = true;
    };
  }, [jumpTo]);

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

      {roomID && (!openedHere || !historyReady) && !room.isError && !history.isError ? (
        <div className="flex flex-1 items-center justify-center">
          <Loader2 aria-label="Loading messages" className="size-5 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <Transcript
          key={roomID ?? peerID}
          ref={transcript}
          messages={socket.messages}
          firstUnreadSeq={firstUnreadSeq}
          onJumpTo={onJumpTo}
          selfID={user.id}
          nameOf={nameOf}
          avatars={avatars}
          readSeqs={readSeqs}
          onVisibleSeqChange={setVisibleSeq}
          onLoadOlder={onLoadOlder}
          hasOlder={history.hasNextPage}
          loadingOlder={history.isFetchingNextPage}
          onOpenMedia={onOpenMedia}
          actions={socket.connected ? actions : undefined}
          showNames={!isDirect}
          selection={selection}
          onToggleSelected={onToggleSelected}
          onToggleItem={onToggleItem}
          onSelectMessages={socket.connected ? setSelection : undefined}
        />
      )}

      <div className="relative shrink-0">
        <div inert={selection !== null}>
          <MessageComposer
            connected={socket.connected}
            uploads={uploads}
            context={context}
            onSend={socket.send}
            onEdit={socket.edit}
            onCancelContext={() => setContext(null)}
            onTyping={socket.notifyTyping}
          />
        </div>
        {selection !== null ? (
          <SelectionBar
            count={selectedCount(selection, socket.messages)}
            canDelete={canDeleteSelected}
            onCopy={copySelected}
            onForward={() => setForwarding(forwardRequest(selection, socket.messages))}
            onDelete={() => setDeleting(selected.map((message) => message.id))}
            onCancel={() => setSelection(null)}
          />
        ) : null}
      </div>

      <ForwardDialog
        open={forwarding !== null}
        count={forwarding?.messageIds.length ?? 0}
        pending={forwardMessages.isPending}
        error={forwardMessages.error ? describe(forwardMessages.error) : null}
        onOpenChange={(open) => {
          if (!open) {
            setForwarding(null);
            forwardMessages.reset();
          }
        }}
        onForward={(roomIds, comment) => {
          if (!forwarding) return;
          forwardMessages.mutate(
            { ...forwarding, roomIds, comment, clientId: newClientID() },
            {
              onSuccess: () => {
                setForwarding(null);
                setSelection(null);
                if (roomIds.length === 1 && roomIds[0] !== roomID) {
                  router.push(`/chat/${roomIds[0]}`);
                } else {
                  toast.success(roomIds.length === 1 ? "Forwarded" : `Forwarded to ${roomIds.length} chats`);
                }
              },
            },
          );
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting && deleting.length > 1 ? `Delete ${deleting.length} messages?` : "Delete message?"}
        consequence={
          deleting && deleting.length > 1
            ? "They disappear for everyone in this chat."
            : "It disappears for everyone in this chat."
        }
        action="Delete"
        onConfirm={() => {
          if (!deleting) return;
          if (socket.remove(deleting.length === 1 ? deleting[0] : deleting)) {
            if (context && deleting.includes(context.messageId)) setContext(null);
            setDeleting(null);
            setSelection(null);
          }
        }}
      />
      <ReadersDialog
        messageID={readersOf}
        direct={isDirect}
        nameOf={nameOf}
        onOpenChange={(open) => !open && setReadersOf(null)}
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

      <MediaViewer
        slides={slides}
        index={viewing ? slideIndexOf(slides, viewing.messageID, viewing.item) : null}
        onClose={() => setViewing(null)}
      />
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
