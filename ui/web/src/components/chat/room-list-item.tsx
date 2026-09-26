"use client";

import type { Timestamp } from "@bufbuild/protobuf/wkt";
import { Check, CheckCheck } from "lucide-react";
import Link from "next/link";
import { usePathname } from "next/navigation";

import { Avatar } from "@/components/ui/avatar";
import type { PublicProfile } from "@/gen/axon/auth/v1/auth_pb";
import { MessageKind, RoomKind, type Message, type Room } from "@/gen/axon/chat/v1/chat_pb";
import { useSession } from "@/lib/auth/session";
import { useLastMessage } from "@/lib/query/chat";
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
  const pathname = usePathname();
  const { user } = useSession();

  const isGroup = room.kind === RoomKind.OPEN;
  const name = isGroup ? room.name : peerProfile?.displayName || shortID(room.peerUserId);
  const avatarSrc = isGroup ? undefined : peerProfile?.avatarUrls?.small || peerProfile?.avatarUrl;

  const lastMessage = useLastMessage(room.id);
  const mine = lastMessage.data?.authorId === user?.id;
  const readByOthers =
    mine && !!lastMessage.data && (room.othersReadSeq ?? BigInt(0)) >= lastMessage.data.seq;

  const unread = Number(room.unreadCount);

  const active = pathname === `/chat/${room.id}`;

  return (
    <Link
      href={`/chat/${room.id}`}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex items-center gap-3 border-b border-border px-4 py-3 transition-colors",
        active ? "bg-primary/5" : "hover:bg-muted",
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
            {formatWhen(lastMessage.data?.sentAt)}
          </time>
        </div>
        <div className="flex items-center gap-1">
          {typing ? (
            <p className="min-w-0 flex-1 truncate text-sm text-primary">typing…</p>
          ) : (
            <>
              {mine ? (
                readByOthers ? (
                  <CheckCheck aria-label="Read" className="size-3.5 shrink-0 text-primary" />
                ) : (
                  <Check aria-label="Sent" className="size-3.5 shrink-0 text-muted-foreground" />
                )
              ) : null}
              <p className="min-w-0 flex-1 truncate text-sm text-muted-foreground">
                {previewFor(lastMessage.data)}
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
  if (!message) {
    return "No messages yet";
  }
  switch (message.kind) {
    case MessageKind.VOICE:
      return "🎤 Voice message";
    case MessageKind.ATTACHMENT:
      return "📎 Attachment";
    case MessageKind.SYSTEM:
      return message.body || "System message";
    default:
      return message.body;
  }
}

function shortID(id?: string): string {
  return id ? id.slice(0, 8) : "?";
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
