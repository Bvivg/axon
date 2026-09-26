"use client";

import { createContext, useContext, useMemo, type ReactNode } from "react";

import { useRoomDirectory } from "@/components/chat/use-room-directory";
import { useRoomActivitySocket } from "@/lib/ws/use-room-activity-socket";
import type { TypingByRoom } from "@/lib/ws/use-typing";

const nobody: TypingByRoom = new Map();

const ChatActivityContext = createContext<TypingByRoom>(nobody);

export function ChatActivityProvider({ children }: { children: ReactNode }) {
  const { rooms, peerIDs } = useRoomDirectory();
  const roomIDs = useMemo(() => (rooms.data ?? []).map((room) => room.id), [rooms.data]);
  const typing = useRoomActivitySocket(roomIDs, peerIDs);

  return <ChatActivityContext value={typing}>{children}</ChatActivityContext>;
}

export function useRoomTyping(): TypingByRoom {
  return useContext(ChatActivityContext);
}
