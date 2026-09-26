"use client";

import { useSyncExternalStore } from "react";

export enum ConnectionStatus {
  Connecting = "connecting",
  Connected = "connected",
  Reconnecting = "reconnecting",
  Offline = "offline",
}

let socketOpen = false;
let everOpened = false;

const listeners = new Set<() => void>();

function notify(): void {
  listeners.forEach((listener) => listener());
}

export function reportRealtimeSocket(open: boolean): void {
  if (socketOpen === open) {
    return;
  }
  socketOpen = open;
  everOpened ||= open;
  notify();
}

export function forgetRealtimeSocket(): void {
  socketOpen = false;
  everOpened = false;
  notify();
}

function currentStatus(): ConnectionStatus {
  if (!navigator.onLine) {
    return ConnectionStatus.Offline;
  }
  if (socketOpen) {
    return ConnectionStatus.Connected;
  }
  return everOpened ? ConnectionStatus.Reconnecting : ConnectionStatus.Connecting;
}

function subscribe(listener: () => void): () => void {
  listeners.add(listener);
  window.addEventListener("online", listener);
  window.addEventListener("offline", listener);
  return () => {
    listeners.delete(listener);
    window.removeEventListener("online", listener);
    window.removeEventListener("offline", listener);
  };
}

export function useConnectionStatus(): ConnectionStatus {
  return useSyncExternalStore(subscribe, currentStatus, () => ConnectionStatus.Connecting);
}

export function useOnline(): boolean {
  return useSyncExternalStore(
    subscribe,
    () => navigator.onLine,
    () => true,
  );
}
