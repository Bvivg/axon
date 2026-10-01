"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Check, CheckCheck } from "lucide-react";
import Link from "next/link";

import { Avatar } from "@/components/ui/avatar";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { MessageKind, RoomKind, type Message, type Room } from "@/gen/axon/chat/v1/chat_pb";
import { useSession } from "@/lib/auth/session";
import { chatHref, isRoomAt, roomAddress, useChatAddress } from "@/lib/chat/address";
import { previewOf } from "@/lib/chat/content";
import { profileName, shortID } from "@/lib/format";
import { toWireMessage } from "@/lib/query/chat";
import { cn } from "@/lib/utils";

export function RoomListItem({
  room,
  peerProfile,
  typing = false,
}: {
  room: Room;
  peerProfile?: PublicProfile;
  typing?: boolean;
}) {
  const address = useChatAddress();
  const { user } = useSession();

  const isGroup = room.kind !== RoomKind.DIRECT;
  const name = isGroup ? room.name : profileName(peerProfile) || shortID(room.peerUserId);
  const avatarSrc = isGroup ? undefined : peerProfile?.avatarUrls?.small || peerProfile?.avatarUrl;

  const lastMessage = room.lastMessage;
  const mine = lastMessage?.authorId === user?.id && lastMessage?.kind !== MessageKind.SYSTEM;
  const readByOthers = mine && !!lastMessage && (room.othersReadSeq ?? BigInt(0)) >= lastMessage.seq;

  const unread = Number(room.unreadCount);

  const active = isRoomAt(address, room, peerProfile);

  return (
    <Link
      href={chatHref(roomAddress(room, peerProfile))}
      scroll={false}
      data-room-id={room.id}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center gap-3 border-b border-border px-4 py-3 transition-colors",
        active ? "bg-signal/5" : "hover:bg-muted",
      )}
    >
      <span className="relative shrink-0">
        <Avatar
          src={avatarSrc}
          alt=""
          fallback={name.slice(0, 1).toUpperCase()}
          className="size-12 text-base"
          sizes="48px"
        />
        {!isGroup && peerProfile?.online ? (
          <span
            aria-label="Online"
            className="absolute bottom-0 right-0 size-3.5 rounded-full border-2 border-background bg-green-500"
          />
        ) : null}
      </span>

      <div className="min-w-0 flex-1">
        <div className="flex items-baseline justify-between gap-2">
          <span className="truncate text-sm font-medium">{name}</span>
          <time className="shrink-0 text-xs text-muted-foreground">
            {formatWhen(lastMessage?.sentAt)}
          </time>
        </div>
        <div className="flex items-center gap-1">
          {typing ? (
            <p className="min-w-0 flex-1 truncate text-sm text-signal">typing…</p>
          ) : (
            <>
              {mine ? (
                readByOthers ? (
                  <CheckCheck aria-label="Read" className="size-3.5 shrink-0 text-signal" />
                ) : (
                  <Check aria-label="Sent" className="size-3.5 shrink-0 text-muted-foreground" />
                )
              ) : null}
              <p className="min-w-0 flex-1 truncate text-sm text-muted-foreground">
                {previewFor(lastMessage)}
              </p>
            </>
          )}
          {unread > 0 ? (
            <span className="ml-1 flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-red-500 px-1.5 text-[11px] font-semibold text-white">
              {unread > 99 ? "99+" : unread}
            </span>
          ) : null}
        </div>
      </div>
    </Link>
  );
}

function previewFor(message: Message | null | undefined): string {
  return message ? previewOf(toWireMessage(message)) : "No messages yet";
}

function formatWhen(ts?: Timestamp): string {
  if (!ts) return "";

  const at = new Date(Number(ts.seconds) * 1000);
  if (Number.isNaN(at.getTime())) return "";

  const now = new Date();
  if (at.toDateString() === now.toDateString()) {
    return at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  }
  return at.toLocaleDateString(undefined, { day: "2-digit", month: "2-digit" });
}
