"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useMemo, useRef } from "react";

import { useRoomTyping } from "@/components/chat/chat-activity";
import { RoomListItem } from "@/components/chat/room-list-item";
import { Alert } from "@/components/ui/alert";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { describe } from "@/lib/errors";
import { useRooms } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";

export function RoomList() {
  const viewport = useRef<HTMLDivElement>(null);
  const rooms = useRooms();

  const peerIDs = useMemo(
    () =>
      (rooms.data ?? [])
        .filter(
          (room): room is typeof room & { peerUserId: string } =>
            room.kind === RoomKind.DIRECT && !!room.peerUserId,
        )
        .map((room) => room.peerUserId),
    [rooms.data],
  );

  const typing = useRoomTyping();

  const profiles = usePublicProfiles(peerIDs);

  const virtualizer = useVirtualizer({
    count: rooms.data?.length ?? 0,
    getScrollElement: () => viewport.current,
    estimateSize: () => 73,
    overscan: 6,
    useFlushSync: false,
  });

  if (rooms.isError) {
    return (
      <div className="p-4">
        <Alert>{describe(rooms.error)}</Alert>
      </div>
    );
  }

  if (rooms.isLoading) {
    return <p className="p-4 text-sm text-muted-foreground">Loading…</p>;
  }

  if (!rooms.data || rooms.data.length === 0) {
    return <p className="p-4 text-sm text-muted-foreground">No chats yet.</p>;
  }

  return (
    <div ref={viewport} className="min-h-0 flex-1 overflow-y-auto">
      <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
        {virtualizer.getVirtualItems().map((row) => {
          const room = rooms.data[row.index];

          return (
            <div
              key={room.id}
              ref={virtualizer.measureElement}
              data-index={row.index}
              className="absolute left-0 top-0 w-full"
              style={{ transform: `translateY(${row.start}px)` }}
            >
              <RoomListItem
                room={room}
                peerProfile={room.peerUserId ? profiles.data?.get(room.peerUserId) : undefined}
                typing={typing.has(room.id)}
              />
            </div>
          );
        })}
      </div>
    </div>
  );
}
