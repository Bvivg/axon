"use client";

import { useParams } from "next/navigation";

import { ChatRoomView } from "@/components/chat/chat-room-view";

export default function ChatRoomPage() {
  const params = useParams<{ roomId: string }>();
  return <ChatRoomView target={{ roomId: params.roomId }} />;
}
