"use client";

import { useRouter } from "next/navigation";
import { useEffect } from "react";

import { navItems } from "@/components/nav/items";
import { togglePalette } from "@/components/nav/palette-store";
import { toggleTheme } from "@/lib/theme";

const chordWindow = 1_000;

export function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  return target.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(target.tagName);
}

export function useGlobalShortcuts(): void {
  const router = useRouter();

  useEffect(() => {
    let chordStartedAt = 0;

    const onKeyDown = (event: KeyboardEvent) => {
      const key = event.key.toLowerCase();
      const mod = event.metaKey || event.ctrlKey;

      if (mod && !event.shiftKey && key === "k") {
        event.preventDefault();
        togglePalette();
        return;
      }
      if (mod && event.shiftKey && key === "l") {
        event.preventDefault();
        toggleTheme();
        return;
      }
      if (mod || event.altKey || event.repeat || isTypingTarget(event.target)) {
        return;
      }

      if (chordStartedAt && event.timeStamp - chordStartedAt < chordWindow) {
        chordStartedAt = 0;
        const target = navItems.find((item) => item.keys?.[1]?.toLowerCase() === key);
        if (target) {
          event.preventDefault();
          router.push(target.href);
        }
        return;
      }

      chordStartedAt = key === "g" ? event.timeStamp : 0;
    };

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [router]);
}
