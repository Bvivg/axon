"use client";

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { MessageKind, type Message, type Room } from "@/gen/axon/chat/v1/chat_pb";
import { chatClient } from "@/lib/connect/clients";
import type { WireMessage } from "@/lib/ws/protocol";

export const chatKeys = {
  rooms: ["chat", "rooms"] as const,
  room: (id: string) => ["chat", "room", id] as const,
  history: (id: string) => ["chat", "history", id] as const,
  lastMessage: (id: string) => ["chat", "last-message", id] as const,
  media: (id: string) => ["chat", "media", id] as const,
  mediaOf: (id: string, kinds: MessageKind[]) => ["chat", "media", id, kinds.join(",")] as const,
};

export function useRooms() {
  return useQuery({
    queryKey: chatKeys.rooms,
    queryFn: async () => {
      const resp = await chatClient.listRooms({});
      return resp.rooms;
    },
  });
}

export function useRoom(roomID: string) {
  return useQuery({
    queryKey: chatKeys.room(roomID),
    queryFn: async () => {
      const resp = await chatClient.getRoom({ roomId: roomID });
      return { room: resp.room, members: resp.members };
    },
    enabled: roomID !== "",

    refetchOnWindowFocus: true,
  });
}

interface HistoryPage {
  messages: WireMessage[];
  hasMore: boolean;
}

const kindNames: Partial<Record<MessageKind, string>> = {
  [MessageKind.TEXT]: "text",
  [MessageKind.VOICE]: "voice",
  [MessageKind.ATTACHMENT]: "attachment",
  [MessageKind.IMAGE]: "image",
  [MessageKind.VIDEO]: "video",
  [MessageKind.SYSTEM]: "system",
};

function payloadOf(m: Message): Record<string, unknown> | undefined {
  switch (m.payload.case) {
    case "image":
      return {
        url: m.payload.value.url,
        thumbnail_url: m.payload.value.thumbnailUrl,
        width: m.payload.value.width,
        height: m.payload.value.height,
        size_bytes: Number(m.payload.value.sizeBytes),
        mime: m.payload.value.mime,
      };
    case "video":
      return {
        url: m.payload.value.url,
        poster_url: m.payload.value.posterUrl,
        width: m.payload.value.width,
        height: m.payload.value.height,
        duration_ms: Number(m.payload.value.durationMs),
        size_bytes: Number(m.payload.value.sizeBytes),
        mime: m.payload.value.mime,
      };
    case "voice":
      return {
        url: m.payload.value.url,
        duration_ms: Number(m.payload.value.durationMs),
        mime: m.payload.value.mime ?? "",
      };
    case "attachment":
      return {
        url: m.payload.value.url,
        filename: m.payload.value.filename,
        mime: m.payload.value.mime,
        size_bytes: Number(m.payload.value.sizeBytes),
      };
    default:
      return undefined;
  }
}

export function toWireMessage(m: Message): WireMessage {
  return {
    id: m.id,
    room_id: m.roomId,
    author_id: m.authorId,
    body: m.body,
    seq: Number(m.seq),
    sent_at: m.sentAt ? new Date(Number(m.sentAt.seconds) * 1000).toISOString() : "",
    client_id: m.clientId,
    kind: kindNames[m.kind] ?? "text",
    payload: payloadOf(m),
  };
}

export function useHistory(roomID: string) {
  return useInfiniteQuery({
    queryKey: chatKeys.history(roomID),
    queryFn: async ({ pageParam }): Promise<HistoryPage> => {
      const resp = await chatClient.listMessages({
        roomId: roomID,
        beforeSeq: pageParam > 0 ? BigInt(pageParam) : undefined,
      });
      return {
        messages: resp.messages.map(toWireMessage),
        hasMore: resp.hasMore,
      };
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      if (!lastPage.hasMore || lastPage.messages.length === 0) {
        return undefined;
      }
      return lastPage.messages[0].seq;
    },
    enabled: roomID !== "",
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });
}

export function useDirectRoomLookup() {
  return useMutation({
    mutationFn: async (peerID: string): Promise<Room | null> => {
      const resp = await chatClient.getDirectRoom({ userId: peerID });
      return resp.room ?? null;
    },
  });
}

export function useLastMessage(roomID: string) {
  return useQuery({
    queryKey: chatKeys.lastMessage(roomID),
    queryFn: async () => {
      const resp = await chatClient.listMessages({ roomId: roomID, limit: 1 });
      return resp.messages[0] ?? null;
    },
    enabled: roomID !== "",
    staleTime: 30_000,
  });
}

export function useRoomMedia(roomID: string, kinds: MessageKind[]) {
  return useInfiniteQuery({
    queryKey: chatKeys.mediaOf(roomID, kinds),
    queryFn: async ({ pageParam }): Promise<HistoryPage> => {
      const resp = await chatClient.listMessages({
        roomId: roomID,
        kinds,
        limit: 60,
        beforeSeq: pageParam > 0 ? BigInt(pageParam) : undefined,
      });
      return {
        messages: resp.messages.map(toWireMessage),
        hasMore: resp.hasMore,
      };
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage) => {
      if (!lastPage.hasMore || lastPage.messages.length === 0) {
        return undefined;
      }
      return lastPage.messages[0].seq;
    },
    enabled: roomID !== "",
  });
}

export function useMarkRead() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (vars: { roomID: string; seq: number }) => {
      if (vars.seq <= 0) return vars;
      await chatClient.markRead({ roomId: vars.roomID, seq: BigInt(vars.seq) });
      return vars;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    },
  });
}
