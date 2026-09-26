"use client";

import { useSyncExternalStore } from "react";

export enum Theme {
  Light = "light",
  Dark = "dark",
  System = "system",
}

export const themeStorageKey = "axon-theme";

const darkQuery = "(prefers-color-scheme: dark)";

const listeners = new Set<() => void>();

function isTheme(value: string | null): value is Theme {
  return value === Theme.Light || value === Theme.Dark || value === Theme.System;
}

function readStored(): Theme {
  if (typeof window === "undefined") {
    return Theme.System;
  }
  try {
    const stored = window.localStorage.getItem(themeStorageKey);
    return isTheme(stored) ? stored : Theme.System;
  } catch {
    return Theme.System;
  }
}

function applyToDocument(theme: Theme): void {
  if (theme === Theme.System) {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", theme);
  }
}

let currentTheme = readStored();

function notify(): void {
  listeners.forEach((listener) => listener());
}

function persist(theme: Theme): void {
  try {
    window.localStorage.setItem(themeStorageKey, theme);
  } catch {
    return;
  }
}

export function setTheme(theme: Theme): void {
  currentTheme = theme;
  persist(theme);
  applyToDocument(theme);
  notify();
}

function systemPrefersDark(): boolean {
  return window.matchMedia(darkQuery).matches;
}

function resolve(theme: Theme): Theme.Light | Theme.Dark {
  if (theme !== Theme.System) {
    return theme;
  }
  return systemPrefersDark() ? Theme.Dark : Theme.Light;
}

export function toggleTheme(): void {
  setTheme(resolve(currentTheme) === Theme.Dark ? Theme.Light : Theme.Dark);
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  const media = window.matchMedia(darkQuery);
  media.addEventListener("change", listener);
  return () => {
    listeners.delete(listener);
    media.removeEventListener("change", listener);
  };
}

export function useTheme(): Theme {
  return useSyncExternalStore(
    subscribe,
    () => currentTheme,
    () => Theme.System,
  );
}

export function useResolvedTheme(): Theme.Light | Theme.Dark {
  return useSyncExternalStore(
    subscribe,
    () => resolve(currentTheme),
    () => Theme.Light,
  );
}
