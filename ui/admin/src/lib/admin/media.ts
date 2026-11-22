"use client";

import { useSyncExternalStore } from "react";

const wide = "(min-width: 768px)";

function subscribe(onChange: () => void): () => void {
  const query = window.matchMedia(wide);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

export function useIsDesktop(): boolean {
  return useSyncExternalStore(subscribe, () => window.matchMedia(wide).matches, () => true);
}
