"use client";

import { useEffect } from "react";

export function useArenaShortcuts({ enabled, move, undo, restart }: { enabled: boolean; move: (cell: number) => void; undo: () => void; restart: () => void }) {
  useEffect(() => {
    function keyDown(event: KeyboardEvent) {
      if (!enabled || event.defaultPrevented || event.repeat || event.ctrlKey || event.metaKey || event.altKey || document.querySelector('[role="dialog"]') || (event.target instanceof HTMLElement && (event.target.isContentEditable || event.target.closest("input,textarea,select")))) return;
      const key = event.key.toLowerCase();
      if (/^[1-9]$/.test(key)) { event.preventDefault(); move(Number(key) - 1); }
      else if (key === "n") { event.preventDefault(); restart(); }
      else if (key === "z") { event.preventDefault(); undo(); }
    }
    window.addEventListener("keydown", keyDown);
    return () => window.removeEventListener("keydown", keyDown);
  }, [enabled, move, undo, restart]);
}
