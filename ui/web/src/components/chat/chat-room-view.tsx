"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { Images, Upload } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState, type DragEvent } from "react";

import { MediaViewer } from "@/components/chat/media-viewer";
import { MessageComposer } from "@/components/chat/message-composer";
import { RoomMedia } from "@/components/chat/room-media";
import { Transcript } from "@/components/chat/transcript";
import { Alert } from "@/components/ui/alert";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { signInPath } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { usePendingUploads } from "@/lib/chat/use-pending-uploads";
import { describe } from "@/lib/errors";
import { usePageVisible } from "@/lib/hooks/use-page-visible";
import { chatKeys, useHistory, useMarkRead, useRoom } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";
import { cn } from "@/lib/utils";
import { useChatSocket, type ChatTarget } from "@/lib/ws/use-chat-socket";
import type { WireMessage } from "@/lib/ws/protocol";

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

  const names = useMemo(() => {
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
  const title = isDirect
    ? peerProfile?.displayName || (peerID ? "New chat" : "Direct message")
    : room.data?.room?.name || "Room";

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
      : `${socket.connected ? "Connected" : "Reconnecting…"}${
          room.data?.room ? ` · ${room.data.room.memberCount} in the room` : ""
        }`);

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
          <h1 className="truncate text-lg font-semibold">{title}</h1>
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
      </header>

      {room.isError ? <Alert className="mt-3">{describe(room.error)}</Alert> : null}
      {history.isError ? <Alert className="mt-3">{describe(history.error)}</Alert> : null}
      {socket.error ? <Alert className="mt-3">{socket.error}</Alert> : null}

      <Transcript
        messages={socket.messages}
        selfID={user.id}
        names={names}
        avatars={avatars}
        readSeqs={readSeqs}
        onVisibleSeqChange={setVisibleSeq}
        onLoadOlder={onLoadOlder}
        hasOlder={history.hasNextPage}
        loadingOlder={history.isFetchingNextPage}
        onOpenMedia={setViewing}
      />

      <MessageComposer
        connected={socket.connected}
        uploads={uploads}
        onSend={socket.send}
        onTyping={socket.notifyTyping}
      />

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
