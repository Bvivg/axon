"use client";

import { useParams } from "next/navigation";

import { ChatRoomView } from "@/components/chat/chat-room-view";

export default function ChatNewDirectPage() {
  const params = useParams<{ peerId: string }>();
  return <ChatRoomView target={{ peerId: params.peerId }} />;
}
