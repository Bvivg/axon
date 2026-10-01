"use client";

import { timestampDate, type Timestamp } from "@bufbuild/protobuf/wkt";
import {
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";

import { MessageKind, type Message, type Room } from "@/gen/axon/chat/v1/chat_pb";
import { chatClient } from "@/lib/connect/clients";
import type { WireMessage, WireReplyPreview } from "@/lib/ws/protocol";

export const chatKeys = {
  rooms: ["chat", "rooms"] as const,
  room: (id: string) => ["chat", "room", id] as const,
  history: (id: string) => ["chat", "history", id] as const,
  lastMessages: ["chat", "last-message"] as const,
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
    case "system":
      return {
        event: m.payload.value.event,
        actor_id: m.payload.value.actorId,
        target_id: m.payload.value.targetId,
        target_ids: m.payload.value.targetIds,
      };
    default:
      return undefined;
  }
}

function isoOf(ts?: Timestamp): string | undefined {
  return ts ? timestampDate(ts).toISOString() : undefined;
}

export function toWireMessage(m: Message): WireMessage {
  return {
    id: m.id,
    room_id: m.roomId,
    author_id: m.authorId,
    body: m.body,
    seq: Number(m.seq),
    sent_at: isoOf(m.sentAt) ?? "",
    client_id: m.clientId,
    kind: kindNames[m.kind] ?? "text",
    payload: payloadOf(m),
    reply_to_id: m.replyToId,
    reply_to: m.replyTo
      ? {
          id: m.replyTo.id,
          author_id: m.replyTo.authorId,
          kind: kindNames[m.replyTo.kind] ?? "text",
          body: m.replyTo.body,
          deleted: m.replyTo.deleted,
        }
      : undefined,
    forwarded_from_id: m.forwardedFromId,
    forwarded_from_author_id: m.forwardedFromAuthorId,
    edited_at: isoOf(m.editedAt),
    deleted_at: isoOf(m.deletedAt),
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

const replyPreviewLength = 140;

function replyPreviewOf(message: WireMessage): WireReplyPreview {
  const deleted = !!message.deleted_at;
  return {
    id: message.id,
    author_id: message.author_id,
    kind: message.kind ?? "text",
    body: deleted ? "" : [...message.body].slice(0, replyPreviewLength).join(""),
    deleted,
  };
}

export function applyUpdate(messages: WireMessage[], updated: WireMessage): WireMessage[] {
  return messages.map((m) => {
    if (m.id === updated.id) {
      return { ...updated, client_id: m.client_id };
    }
    if (m.reply_to?.id === updated.id) {
      return { ...m, reply_to: replyPreviewOf(updated) };
    }
    return m;
  });
}

export function patchHistory(queryClient: QueryClient, message: WireMessage) {
  queryClient.setQueryData<InfiniteData<HistoryPage, number>>(chatKeys.history(message.room_id), (data) => {
    if (!data) return data;
    return {
      ...data,
      pages: data.pages.map((page) => ({ ...page, messages: applyUpdate(page.messages, message) })),
    };
  });
}

export async function refreshNewestPage(queryClient: QueryClient, roomID: string) {
  const key = chatKeys.history(roomID);
  const loaded = queryClient.getQueryData<InfiniteData<HistoryPage, number>>(key);
  if (!loaded || loaded.pages.length <= 1) {
    void queryClient.invalidateQueries({ queryKey: key });
    return;
  }

  let newest: HistoryPage;
  try {
    const resp = await chatClient.listMessages({ roomId: roomID });
    newest = { messages: resp.messages.map(toWireMessage), hasMore: resp.hasMore };
  } catch {
    void queryClient.invalidateQueries({ queryKey: key, refetchType: "none" });
    return;
  }

  queryClient.setQueryData<InfiniteData<HistoryPage, number>>(key, (data) => {
    if (!data || data.pages.length === 0) return data;
    const [first, ...older] = data.pages;
    const knownNewest = first.messages.at(-1)?.seq ?? 0;
    const fetchedOldest = newest.messages[0]?.seq ?? knownNewest + 1;
    if (fetchedOldest > knownNewest + 1) {
      return { pages: [newest], pageParams: [0] };
    }
    const fresh = new Set(newest.messages.map((m) => m.seq));
    const kept = first.messages.filter((m) => !fresh.has(m.seq));
    return { ...data, pages: [{ messages: [...kept, ...newest.messages], hasMore: first.hasMore }, ...older] };
  });
}

export function forgetHistory(queryClient: QueryClient, roomID: string) {
  queryClient.setQueryData<InfiniteData<HistoryPage, number>>(chatKeys.history(roomID), {
    pages: [{ messages: [], hasMore: false }],
    pageParams: [0],
  });
  void queryClient.invalidateQueries({ queryKey: chatKeys.history(roomID), refetchType: "none" });
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

export function useHideRoom() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (roomID: string) => {
      await chatClient.hideRoom({ roomId: roomID });
      return roomID;
    },
    onSuccess: (roomID) => {
      forgetHistory(queryClient, roomID);
      void queryClient.invalidateQueries({ queryKey: chatKeys.lastMessage(roomID) });
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    },
  });
}

export function useLeaveRoom() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (roomID: string) => {
      await chatClient.leaveRoom({ roomId: roomID });
      return roomID;
    },
    onSuccess: (roomID) => {
      forgetHistory(queryClient, roomID);
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    },
  });
}

export function useCreateGroup() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (vars: { name: string; memberIDs: string[] }) => {
      const resp = await chatClient.createGroup({ name: vars.name, memberUserIds: vars.memberIDs });
      return resp.room;
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    },
  });
}

function useGroupChange<T>(change: (vars: T) => Promise<unknown>, roomOf: (vars: T) => string) {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: change,
    onSuccess: (_, vars) => {
      void queryClient.invalidateQueries({ queryKey: chatKeys.room(roomOf(vars)) });
      void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
    },
  });
}

export function useAddGroupMembers() {
  return useGroupChange(
    (vars: { roomID: string; userIDs: string[] }) =>
      chatClient.addGroupMembers({ roomId: vars.roomID, userIds: vars.userIDs }),
    (vars) => vars.roomID,
  );
}

export function useRemoveGroupMember() {
  return useGroupChange(
    (vars: { roomID: string; userID: string }) =>
      chatClient.removeGroupMember({ roomId: vars.roomID, userId: vars.userID }),
    (vars) => vars.roomID,
  );
}

export function useRenameGroup() {
  return useGroupChange(
    (vars: { roomID: string; name: string }) => chatClient.renameGroup({ roomId: vars.roomID, name: vars.name }),
    (vars) => vars.roomID,
  );
}
