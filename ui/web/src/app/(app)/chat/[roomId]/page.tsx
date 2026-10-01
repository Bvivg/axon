"use client";

import { useParams, useRouter } from "next/navigation";
import { useEffect } from "react";

import { HistoryMode, openChat } from "@/lib/chat/address";

export default function LegacyChatRoomPage() {
  const params = useParams<{ roomId: string }>();
  const router = useRouter();

  useEffect(() => {
    openChat(router, params.roomId.toLowerCase(), HistoryMode.Replace);
  }, [params.roomId, router]);

  return null;
}
