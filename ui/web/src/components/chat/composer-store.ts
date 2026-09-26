"use client";

import { useSyncExternalStore } from "react";

let open = false;

const listeners = new Set<() => void>();

function set(next: boolean): void {
  open = next;
  listeners.forEach((listener) => listener());
}

export function openComposer(): void {
  set(true);
}

export function closeComposer(): void {
  set(false);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useComposerOpen(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => open,
    () => false,
  );
}
