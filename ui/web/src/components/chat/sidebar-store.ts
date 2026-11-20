"use client";

import type { useRouter } from "next/navigation";
import { useSyncExternalStore } from "react";

import { chatPath, openChat } from "@/lib/chat/address";

export enum SidebarMode {
  Chats = "chats",
  Search = "search",
  Compose = "compose",
}

const initial = { mode: SidebarMode.Chats, focus: 0 };
let state = initial;
const listeners = new Set<() => void>();

export function setSidebarMode(mode: SidebarMode): void {
  state = { mode, focus: state.focus + 1 };
  listeners.forEach((listener) => listener());
}

export function showChatSidebar(router: ReturnType<typeof useRouter>, mode: SidebarMode): void {
  if (window.location.pathname !== chatPath) openChat(router);
  setSidebarMode(mode);
}

export function useChatSidebar() {
  return useSyncExternalStore(
    (listener) => {
      listeners.add(listener);
      return () => { listeners.delete(listener); };
    },
    () => state,
    () => initial,
  );
}
