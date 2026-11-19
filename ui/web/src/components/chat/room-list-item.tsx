"use client";

import { Check, CheckCheck } from "lucide-react";

import { Avatar } from "@/components/ui/avatar";
import { ListBadge, ListItem } from "@/components/ui/list-item";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { MessageKind, RoomKind, type Message, type Room } from "@/gen/axon/chat/v1/chat_pb";
import { useSession } from "@/lib/auth/session";
import { chatHref, roomAddress } from "@/lib/chat/address";
import { previewOf } from "@/lib/chat/content";
import { fromTimestamp, profileName, shortID, shortWhen } from "@/lib/format";
import { toWireMessage } from "@/lib/query/chat";

export function RoomListItem({
  room,
  peerProfile,
  typing = false,
  active = false,
}: {
  room: Room;
  peerProfile?: PublicProfile;
  typing?: boolean;
  active?: boolean;
}) {
  const { user } = useSession();

  const isGroup = room.kind !== RoomKind.DIRECT;
  const name = isGroup ? room.name : profileName(peerProfile) || shortID(room.peerUserId);
  const avatarSrc = isGroup ? undefined : peerProfile?.avatarUrls?.small || peerProfile?.avatarUrl;

  const lastMessage = room.lastMessage;
  const mine = lastMessage?.authorId === user?.id && lastMessage?.kind !== MessageKind.SYSTEM;
  const readByOthers = mine && !!lastMessage && (room.othersReadSeq ?? BigInt(0)) >= lastMessage.seq;

  const unread = Number(room.unreadCount);

  return (
    <ListItem
      href={chatHref(roomAddress(room, peerProfile))}
      scroll={false}
      data-room-id={room.id}
      active={active}
      leading={
        <>
          <Avatar
            src={avatarSrc}
            alt=""
            fallback={name.slice(0, 1).toUpperCase()}
            className="size-8 rounded-[10px] text-[11px]"
            sizes="32px"
          />
          {!isGroup && peerProfile?.online ? (
            <span
              aria-label="Online"
              className="absolute bottom-0 right-0 size-3.5 rounded-full border-2 border-background bg-green-500"
            />
          ) : null}
        </>
      }
      title={name}
      meta={<time>{shortWhen(fromTimestamp(lastMessage?.sentAt))}</time>}
      subtitle={
        typing ? (
          <span className="truncate text-signal">typing…</span>
        ) : (
          <>
            {mine ? (
              readByOthers ? (
                <CheckCheck aria-label="Read" className="size-3.5 shrink-0 text-signal" />
              ) : (
                <Check aria-label="Sent" className="size-3.5 shrink-0" />
              )
            ) : null}
            <span className="truncate">{previewFor(lastMessage)}</span>
          </>
        )
      }
      trailing={unread > 0 ? <ListBadge>{unread > 99 ? "99+" : unread}</ListBadge> : null}
    />
  );
}

function previewFor(message: Message | null | undefined): string {
  return message ? previewOf(toWireMessage(message)) : "No messages yet";
}
