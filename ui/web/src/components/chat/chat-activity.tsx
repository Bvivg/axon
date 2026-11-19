"use client";

import { useQueryClient } from "@tanstack/react-query";
import { usePathname, useRouter } from "next/navigation";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { toast } from "sonner";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { useSounds } from "@/components/nav/sound-provider";
import type { Member } from "@/gen/axon/chat/v1/chat_pb";
import { useSession } from "@/lib/auth/session";
import { ChatAddressKind, isRoomAt, navigateTo, openChat, parseChatAddress, useChatAddress } from "@/lib/chat/address";
import { ContentKind, previewOf, quoteOf } from "@/lib/chat/content";
import { listenForOpenRequests, pushSupported, resyncPush } from "@/lib/push";
import { chatKeys, usePushKey } from "@/lib/query/chat";
import { usePublicProfileByUsername } from "@/lib/query/people";
import type { WireMessage } from "@/lib/ws/protocol";
import { useRoomActivitySocket } from "@/lib/ws/use-room-activity-socket";
import type { TypingByRoom } from "@/lib/ws/use-typing";
import type { CallChannel } from "@/lib/calls/channel";

const nobody: TypingByRoom = new Map();
const badge = /^\(\d+\+?\) /;

const ChatActivityContext = createContext<TypingByRoom>(nobody);
const ChatCallContext = createContext<CallChannel | null>(null);
const ChatDraftsContext = createContext<Map<string, string> | null>(null);

interface RoomData {
  members: Member[];
}

export function ChatActivityProvider({ children }: { children: ReactNode }) {
  const [drafts] = useState(() => new Map<string, string>());
  const { rooms, entries, peerIDs, unread } = useRoomDirectory();
  const { user } = useSession();
  const router = useRouter();
  const pathname = usePathname();
  const address = useChatAddress();
  const queryClient = useQueryClient();
  const { player, messages: messageSounds } = useSounds();
  const target = parseChatAddress(address);
  const openPerson = usePublicProfileByUsername(target.kind === ChatAddressKind.Person && !entries.some(entry => entry.address === address) ? target.username : "").data;
  const roomIDs = useMemo(() => (rooms.data ?? []).map((room) => room.id), [rooms.data]);

  const announce = useCallback(
    (message: WireMessage) => {
      if (!user || message.author_id === user.id || message.kind === ContentKind.System) return;
      const entry = entries.find((candidate) => candidate.room.id === message.room_id);
      if (document.visibilityState !== "visible" || isRoomAt(address, { id: message.room_id }, entry?.peer)
        || openPerson?.id === message.author_id) return;
      const label = entry?.label || "New message";
      const group = entry !== undefined && !entry.direct;
      const author = queryClient
        .getQueryData<RoomData>(chatKeys.room(message.room_id))
        ?.members.find((member) => member.userId === message.author_id)?.displayName;
      const text = message.payload ? previewOf(message) : quoteOf({ ...message, kind: message.kind ?? ContentKind.Text });
      if (document.querySelector("[data-incoming-call]")) return;
      if (messageSounds) player.message();
      toast(label, {
        id: `message-${message.room_id}`,
        className: "axon-message-toast",
        description: group && author ? `${author}: ${text}` : text,
        action: {
          label: "Open chat",
          onClick: () => openChat(router, entry?.address ?? message.room_id),
        },
      });
    },
    [user, address, entries, queryClient, router, messageSounds, player, openPerson],
  );

  const { typing, calls } = useRoomActivitySocket(roomIDs, peerIDs, announce);

  const pushKey = usePushKey(!!user && pushSupported());
  useEffect(() => {
    if (pushKey.data) void resyncPush(pushKey.data).catch(() => undefined);
  }, [pushKey.data]);

  useEffect(() => listenForOpenRequests((url) => navigateTo(router, url)), [router]);

  useEffect(() => {
    const base = document.title.replace(badge, "");
    document.title = unread > 0 ? `(${unread > 99 ? "99+" : unread}) ${base}` : base;
  }, [unread, pathname, address]);

  return (
    <ChatActivityContext value={typing}>
      <ChatCallContext value={calls}>
        <ChatDraftsContext value={drafts}>{children}</ChatDraftsContext>
      </ChatCallContext>
    </ChatActivityContext>
  );
}

export function useChatDrafts() {
  return useContext(ChatDraftsContext);
}

export function useChatCallChannel() {
  const channel = useContext(ChatCallContext);
  if (!channel) throw new Error("Call channel requires a signed-in session");
  return channel;
}

export function useRoomTyping(): TypingByRoom {
  return useContext(ChatActivityContext);
}
