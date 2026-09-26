"use client";

import { useSyncExternalStore } from "react";

export type Theme = "light" | "dark" | "system";

export const themeStorageKey = "axon-theme";

const listeners = new Set<() => void>();

function isTheme(value: string | null): value is Theme {
  return value === "light" || value === "dark" || value === "system";
}

function readStored(): Theme {
  if (typeof window === "undefined") {
    return "system";
  }
  const stored = window.localStorage.getItem(themeStorageKey);
  return isTheme(stored) ? stored : "system";
}

function applyToDocument(theme: Theme): void {
  if (theme === "system") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", theme);
  }
}

let currentTheme = readStored();

export function setTheme(theme: Theme): void {
  currentTheme = theme;
  window.localStorage.setItem(themeStorageKey, theme);
  applyToDocument(theme);
  listeners.forEach((listener) => listener());
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot(): Theme {
  return currentTheme;
}

function getServerSnapshot(): Theme {
  return "system";
}

export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
