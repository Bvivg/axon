"use client";

import { useMemo } from "react";

import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { RoomKind, type Room } from "@/gen/axon/chat/v1/chat_pb";
import { shortID } from "@/lib/format";
import { useRooms } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";

export interface RoomEntry {
  room: Room;
  label: string;
  direct: boolean;
  peer?: PublicProfile;
  avatarSrc?: string;
  unread: number;
}

export function useRoomDirectory() {
  const rooms = useRooms();

  const peerIDs = useMemo(
    () =>
      (rooms.data ?? [])
        .filter((room) => room.kind === RoomKind.DIRECT && !!room.peerUserId)
        .map((room) => room.peerUserId as string),
    [rooms.data],
  );

  const profiles = usePublicProfiles(peerIDs);

  const entries = useMemo<RoomEntry[]>(
    () => (rooms.data ?? []).map((room) => toEntry(room, profiles.data)),
    [rooms.data, profiles.data],
  );

  const unread = useMemo(() => entries.reduce((sum, entry) => sum + entry.unread, 0), [entries]);

  return { rooms, entries, peerIDs, unread };
}

function toEntry(room: Room, profiles?: Map<string, PublicProfile>): RoomEntry {
  const direct = room.kind === RoomKind.DIRECT;
  const peer = direct && room.peerUserId ? profiles?.get(room.peerUserId) : undefined;
  return {
    room,
    direct,
    peer,
    label: direct ? peer?.displayName || shortID(room.peerUserId) : room.name,
    avatarSrc: peer?.avatarUrls?.small || peer?.avatarUrl,
    unread: Number(room.unreadCount),
  };
}
