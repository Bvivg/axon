"use client";

import { useSyncExternalStore } from "react";

let open = false;

const listeners = new Set<() => void>();

export function setPaletteOpen(next: boolean): void {
  if (open === next) {
    return;
  }
  open = next;
  listeners.forEach((listener) => listener());
}

export function togglePalette(): void {
  setPaletteOpen(!open);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function usePaletteOpen(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => open,
    () => false,
  );
}
