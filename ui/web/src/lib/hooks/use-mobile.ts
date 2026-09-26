"use client";

import { useSyncExternalStore } from "react";

const mobileBreakpoint = 768;

function query(): MediaQueryList {
  return window.matchMedia(`(max-width: ${mobileBreakpoint - 1}px)`);
}

function subscribe(onChange: () => void): () => void {
  const mql = query();
  mql.addEventListener("change", onChange);
  return () => mql.removeEventListener("change", onChange);
}

function getSnapshot(): boolean {
  return query().matches;
}

function getServerSnapshot(): boolean {
  return false;
}

export function useIsMobile(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
