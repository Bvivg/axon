"use client";

import { useSyncExternalStore } from "react";

export const idleAfterMs = 15_000;

const activityEvents = ["pointermove", "pointerdown", "keydown", "wheel", "touchstart", "touchmove"] as const;

const listeners = new Set<() => void>();
let attentive = false;
let lastActivity = 0;
let idleTimer: ReturnType<typeof setTimeout> | null = null;
let started = false;

function compute(): boolean {
  return document.visibilityState === "visible" && document.hasFocus() && Date.now() - lastActivity < idleAfterMs;
}

function refresh(): void {
  const next = compute();
  if (next !== attentive) {
    attentive = next;
    listeners.forEach((listener) => listener());
  }
  if (idleTimer !== null) clearTimeout(idleTimer);
  idleTimer = null;
  if (attentive) {
    idleTimer = setTimeout(refresh, Math.max(0, lastActivity + idleAfterMs - Date.now()));
  }
}

function onActivity(): void {
  lastActivity = Date.now();
  if (!attentive) refresh();
}

function onReturn(): void {
  if (document.visibilityState === "visible" && document.hasFocus()) {
    lastActivity = Date.now();
  }
  refresh();
}

function start(): void {
  if (started || typeof window === "undefined") return;
  started = true;
  for (const type of activityEvents) {
    window.addEventListener(type, onActivity, { capture: true, passive: true });
  }
  window.addEventListener("focus", onReturn);
  window.addEventListener("blur", refresh);
  document.addEventListener("visibilitychange", onReturn);
  if (document.hasFocus()) lastActivity = Date.now();
  refresh();
}

function subscribe(listener: () => void): () => void {
  start();
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function useAttention(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => attentive,
    () => false,
  );
}

export function isAttentive(): boolean {
  start();
  return attentive;
}
