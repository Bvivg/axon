"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { Room } from "@/gen/axon/chat/v1/chat_pb";
import { chatClient } from "@/lib/connect/clients";
import type { WireMessage } from "@/lib/ws/protocol";

/**
 * Server state for chat: rooms and history.
 *
 * What arrives on the socket is not in here. Query cache is for state the
 * server owns and this client asks for; live messages are pushed, and mixing
 * the two would mean every arriving message invalidating a query that then
 * re-fetches what it was just handed.
 */

export const chatKeys = {
  rooms: ["chat", "rooms"] as const,
  room: (id: string) => ["chat", "room", id] as const,
  history: (id: string) => ["chat", "history", id] as const,
};

/** useRooms lists the rooms the signed-in user belongs to. */
export function useRooms() {
  return useQuery({
    queryKey: chatKeys.rooms,
    queryFn: async () => {
      const resp = await chatClient.listRooms({});
      return resp.rooms;
    },
  });
}

/** useRoom reads one room and its members. */
export function useRoom(roomID: string) {
  return useQuery({
    queryKey: chatKeys.room(roomID),
    queryFn: async () => {
      const resp = await chatClient.getRoom({ roomId: roomID });
      return { room: resp.room, members: resp.members };
    },
    enabled: roomID !== "",
    // Members change when somebody joins, which arrives on no channel this
    // client watches. Refetching on focus is the cheap way to notice.
    refetchOnWindowFocus: true,
  });
}

/**
 * useHistory reads the most recent page of a room.
 *
 * It runs once per room and then stays put: everything after this page arrives
 * on the socket, and a refetch would fight with it. staleTime of Infinity says
 * so rather than relying on nothing happening to trigger one.
 */
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

/** useCreateRoom opens a room and puts the caller in it. */
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

/** useJoinRoom adds the caller to a room they know the id of. */
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
