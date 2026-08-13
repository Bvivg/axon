"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { Room } from "@/gen/axon/chat/v1/chat_pb";
import { chatClient } from "@/lib/connect/clients";
import type { WireMessage } from "@/lib/ws/protocol";

export const chatKeys = {
  rooms: ["chat", "rooms"] as const,
  room: (id: string) => ["chat", "room", id] as const,
  history: (id: string) => ["chat", "history", id] as const,
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

export function useHistory(roomID: string) {
  return useQuery({
    queryKey: chatKeys.history(roomID),
    queryFn: async (): Promise<WireMessage[]> => {
      const resp = await chatClient.listMessages({ roomId: roomID });
      return resp.messages.map((m) => ({
        id: m.id,
        room_id: m.roomId,
        author_id: m.authorId,
        body: m.body,
        seq: Number(m.seq),
        sent_at: m.sentAt ? new Date(Number(m.sentAt.seconds) * 1000).toISOString() : "",
        client_id: m.clientId,
      }));
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
