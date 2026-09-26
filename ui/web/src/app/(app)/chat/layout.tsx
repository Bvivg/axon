"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { ChatListHeader } from "@/components/chat/chat-list-header";
import { RoomList } from "@/components/chat/room-list";
import { cn } from "@/lib/utils";

export default function ChatLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const isListRoute = pathname === "/chat";
  return (
    <div className="flex min-h-0 w-full flex-1 overflow-hidden">
      <aside
        aria-label="Chats"
        className={cn(
          "w-full min-h-0 shrink-0 flex-col overflow-hidden md:flex md:w-80 md:border-r md:border-border lg:w-96",
          isListRoute ? "flex" : "hidden",
        )}
      >
        <ChatListHeader />
        <RoomList />
      </aside>

      <div
        className={cn(
          "min-h-0 min-w-0 flex-1 flex-col",
          isListRoute ? "hidden md:flex" : "flex",
        )}
      >
        {children}
      </div>
    </div>
  );
}
