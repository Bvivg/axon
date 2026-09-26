"use client";

import { useSyncExternalStore } from "react";

function subscribe(): () => void {
  return () => {};
}

export function useIsMac(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => /Mac|iPhone|iPad/.test(navigator.userAgent),
    () => true,
  );
}

export function useModKey(): string {
  return useIsMac() ? "⌘" : "Ctrl";
}
