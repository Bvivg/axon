"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import { useMemo, useRef } from "react";

import { useRoomTyping } from "@/components/chat/chat-activity";
import { RoomListItem } from "@/components/chat/room-list-item";
import { Alert } from "@/components/ui/alert";
import { ScrollArea } from "@/components/ui/scroll-area";
import { RoomKind } from "@/gen/axon/chat/v1/chat_pb";
import { isRoomAt, useChatAddress } from "@/lib/chat/address";
import { describe } from "@/lib/errors";
import { useRooms } from "@/lib/query/chat";
import { usePublicProfiles } from "@/lib/query/people";

export function RoomList() {
  const viewport = useRef<HTMLDivElement>(null);
  const rooms = useRooms();
  const entries = rooms.data ?? [];

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
  const address = useChatAddress();

  const profiles = usePublicProfiles(peerIDs);

  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => viewport.current,
    estimateSize: () => 54,
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
    <>
      <ScrollArea className="flex-1" viewportProps={{ ref: viewport }}>
        <div className="relative w-full" style={{ height: virtualizer.getTotalSize() }}>
          {virtualizer.getVirtualItems().map((row) => {
            const room = entries[row.index];
            const peerProfile = room.peerUserId ? profiles.data?.get(room.peerUserId) : undefined;

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
                  peerProfile={peerProfile}
                  typing={typing.has(room.id)}
                  active={isRoomAt(address, room, peerProfile)}
                />
              </div>
            );
          })}
        </div>
      </ScrollArea>
    </>
  );
}
