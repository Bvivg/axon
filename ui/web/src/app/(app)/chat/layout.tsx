"use client";

import { usePathname } from "next/navigation";
import type { ReactNode } from "react";

import { ChatListHeader } from "@/components/chat/chat-list-header";
import { RoomList } from "@/components/chat/room-list";
import { useRequireSession } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { cn } from "@/lib/utils";

export default function ChatLayout({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const isListRoute = pathname === "/chat";
  const { status } = useSession();

  useRequireSession();

  if (status !== "authenticated") {
    return (
      <main className="flex min-h-screen items-center justify-center">
        <p className="text-sm text-muted-foreground">Loading…</p>
      </main>
    );
  }

  return (
    <div className="flex h-[calc(100dvh-7rem)] w-full overflow-hidden md:h-screen">
      <aside
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
