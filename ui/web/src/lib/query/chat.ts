"use client";

import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { Message, Room } from "@/gen/axon/chat/v1/chat_pb";
import { chatClient } from "@/lib/connect/clients";
import type { WireMessage } from "@/lib/ws/protocol";

export const chatKeys = {
  rooms: ["chat", "rooms"] as const,
  room: (id: string) => ["chat", "room", id] as const,
  history: (id: string) => ["chat", "history", id] as const,
  lastMessage: (id: string) => ["chat", "last-message", id] as const,
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

function toWireMessage(m: Message): WireMessage {
  return {
    id: m.id,
    room_id: m.roomId,
    author_id: m.authorId,
    body: m.body,
    seq: Number(m.seq),
    sent_at: m.sentAt ? new Date(Number(m.sentAt.seconds) * 1000).toISOString() : "",
    client_id: m.clientId,
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

export function useCreateRoom() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (name: string): Promise<Room | undefined> => {
      const resp = await chatClient.createRoom({ name });
      return resp.room;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    },
  });
}

export function useJoinRoom() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (roomID: string) => {
      await chatClient.joinRoom({ roomId: roomID });
      return roomID;
    },
    onSuccess: (roomID) => {
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
      void queryClient.invalidateQueries({ queryKey: chatKeys.room(roomID) });
    },
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
