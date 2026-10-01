"use client";

import { useQueryClient } from "@tanstack/react-query";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useMemo, type ReactNode } from "react";
import { toast } from "sonner";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import type { Member } from "@/gen/axon/chat/v1/chat_pb";
import { useSession } from "@/lib/auth/session";
import { isRoomAt, navigateTo, openChat, useChatAddress } from "@/lib/chat/address";
import { ContentKind, previewOf } from "@/lib/chat/content";
import { listenForOpenRequests, pushSupported, resyncPush } from "@/lib/push";
import { chatKeys, usePushKey } from "@/lib/query/chat";
import type { WireMessage } from "@/lib/ws/protocol";
import { useRoomActivitySocket } from "@/lib/ws/use-room-activity-socket";
import type { TypingByRoom } from "@/lib/ws/use-typing";

const nobody: TypingByRoom = new Map();
const badge = /^\(\d+\+?\) /;

const ChatActivityContext = createContext<TypingByRoom>(nobody);

interface RoomData {
  members: Member[];
}

export function ChatActivityProvider({ children }: { children: ReactNode }) {
  const { rooms, entries, peerIDs, unread } = useRoomDirectory();
  const { user } = useSession();
  const router = useRouter();
  const pathname = usePathname();
  const address = useChatAddress();
  const queryClient = useQueryClient();
  const roomIDs = useMemo(() => (rooms.data ?? []).map((room) => room.id), [rooms.data]);

  const announce = useCallback(
    (message: WireMessage) => {
      if (!user || message.author_id === user.id || message.kind === ContentKind.System) return;
      const entry = entries.find((candidate) => candidate.room.id === message.room_id);
      if (document.visibilityState !== "visible" || isRoomAt(address, { id: message.room_id }, entry?.peer)) return;
      const label = entry?.label || "New message";
      const group = entry !== undefined && !entry.direct;
      const author = queryClient
        .getQueryData<RoomData>(chatKeys.room(message.room_id))
        ?.members.find((member) => member.userId === message.author_id)?.displayName;
      const text = previewOf(message);
      toast(label, {
        id: `message-${message.room_id}`,
        description: group && author ? `${author}: ${text}` : text,
        action: {
          label: "Open",
          onClick: () => openChat(router, entry?.address ?? message.room_id),
        },
      });
    },
    [user, address, entries, queryClient, router],
  );

  const typing = useRoomActivitySocket(roomIDs, peerIDs, announce);

  const pushKey = usePushKey(!!user && pushSupported());
  useEffect(() => {
    if (pushKey.data) void resyncPush(pushKey.data).catch(() => undefined);
  }, [pushKey.data]);

  useEffect(() => listenForOpenRequests((url) => navigateTo(router, url)), [router]);

  useEffect(() => {
    const base = document.title.replace(badge, "");
    document.title = unread > 0 ? `(${unread > 99 ? "99+" : unread}) ${base}` : base;
  }, [unread, pathname, address]);

  return <ChatActivityContext value={typing}>{children}</ChatActivityContext>;
}

export function useRoomTyping(): TypingByRoom {
  return useContext(ChatActivityContext);
}
