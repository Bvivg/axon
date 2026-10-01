"use client";

import { Code } from "@connectrpc/connect";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

import { ChatRoomView } from "@/components/chat/chat-room-view";
import { entryAt, useRoomDirectory } from "@/components/chat/use-room-directory";
import { buttonVariants } from "@/components/ui/button";
import { LoadingDots } from "@/components/ui/status";
import { useSession } from "@/lib/auth/session";
import { ChatAddressKind, HistoryMode, openChat, parseChatAddress, useChatAddress } from "@/lib/chat/address";
import { describe, isCode } from "@/lib/errors";
import { useDirectRoom } from "@/lib/query/chat";
import { usePublicProfileByUsername } from "@/lib/query/people";
import type { ChatTarget } from "@/lib/ws/use-chat-socket";

enum Resolution {
  Empty = "empty",
  Loading = "loading",
  Missing = "missing",
  Found = "found",
}

type Resolved =
  | { status: Resolution.Empty }
  | { status: Resolution.Loading }
  | { status: Resolution.Missing; message: string }
  | { status: Resolution.Found; target: ChatTarget };

const loading: Resolved = { status: Resolution.Loading };
const userNotFound = "User not found.";

const missing = (message: string): Resolved => ({ status: Resolution.Missing, message });
const room = (roomId: string): Resolved => ({ status: Resolution.Found, target: { roomId } });

export function ChatAddressView() {
  const address = useChatAddress();
  const resolved = useResolvedChat(address);

  switch (resolved.status) {
    case Resolution.Empty:
      return (
        <Placeholder>
          <p className="text-sm text-muted-foreground">Select a chat to start messaging.</p>
        </Placeholder>
      );
    case Resolution.Loading:
      return (
        <Placeholder>
          <LoadingDots />
        </Placeholder>
      );
    case Resolution.Missing:
      return (
        <Placeholder>
          <p className="text-sm text-muted-foreground">{resolved.message}</p>
          <Link href="/chat" className={buttonVariants({ variant: "outline", className: "md:hidden" })}>
            Back to chats
          </Link>
        </Placeholder>
      );
    case Resolution.Found:
      return <ChatRoomView key={targetKey(resolved.target)} target={resolved.target} />;
  }
}

function Placeholder({ children }: { children: ReactNode }) {
  return (
    <main className="flex flex-1 flex-col items-center justify-center gap-4 px-6 text-center text-muted-foreground">
      {children}
    </main>
  );
}

function targetKey(target: ChatTarget): string {
  return "roomId" in target ? `room:${target.roomId}` : `peer:${target.peerId}`;
}

function useResolvedChat(address: string): Resolved {
  const router = useRouter();
  const { user } = useSession();
  const { rooms, entries } = useRoomDirectory();
  const parsed = parseChatAddress(address);

  const known = entryAt(entries, address);
  const profile = usePublicProfileByUsername(
    parsed.kind === ChatAddressKind.Person && !known ? parsed.username : "",
  );
  const peer = profile.data ?? undefined;
  const peerRoom = peer ? entries.find((entry) => entry.direct && entry.room.peerUserId === peer.id) : undefined;
  const direct = useDirectRoom(peer && !peerRoom && rooms.isSuccess && peer.id !== user?.id ? peer.id : "");

  const canonical = parsed.kind === ChatAddressKind.Room && known?.direct ? known.address : address;
  useEffect(() => {
    if (canonical !== address || window.location.hash.slice(1) !== canonical) {
      openChat(router, canonical, HistoryMode.Replace);
    }
  }, [canonical, address, router]);

  switch (parsed.kind) {
    case ChatAddressKind.None:
      return { status: Resolution.Empty };
    case ChatAddressKind.Unknown:
      return missing("Chat not found.");
    case ChatAddressKind.Room:
      return room(parsed.roomId);
  }

  if (known) return room(known.room.id);
  if (profile.isError) {
    return missing(isCode(profile.error, Code.NotFound) ? userNotFound : describe(profile.error));
  }
  if (!peer) return profile.isPending ? loading : missing(userNotFound);
  if (peer.id === user?.id) return missing("You can't message yourself.");
  if (peerRoom) return room(peerRoom.room.id);
  if (rooms.isError) return missing(describe(rooms.error));
  if (direct.isError) return missing(describe(direct.error));
  if (!direct.isSuccess) return loading;
  return direct.data ? room(direct.data.id) : { status: Resolution.Found, target: { peerId: peer.id } };
}
