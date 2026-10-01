"use client";

import type { useRouter } from "next/navigation";
import { useSyncExternalStore } from "react";

import { normalizeUsername } from "@/lib/format";

export const chatPath = "/chat";

export enum ChatAddressKind {
  None = "none",
  Room = "room",
  Person = "person",
  Unknown = "unknown",
}

export type ChatAddress =
  | { kind: ChatAddressKind.None }
  | { kind: ChatAddressKind.Room; roomId: string }
  | { kind: ChatAddressKind.Person; username: string }
  | { kind: ChatAddressKind.Unknown };

const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
const usernamePattern = /^[a-z][a-z0-9_]{2,31}$/;
const addressChangeEvent = "axon:chat-address";

export function normalizeChatAddress(hash: string): string {
  const raw = hash.startsWith("#") ? hash.slice(1) : hash;
  try {
    return normalizeUsername(decodeURIComponent(raw));
  } catch {
    return normalizeUsername(raw);
  }
}

export function parseChatAddress(address: string): ChatAddress {
  if (address === "") {
    return { kind: ChatAddressKind.None };
  }
  if (uuidPattern.test(address)) {
    return { kind: ChatAddressKind.Room, roomId: address };
  }
  if (usernamePattern.test(address)) {
    return { kind: ChatAddressKind.Person, username: address };
  }
  return { kind: ChatAddressKind.Unknown };
}

export enum HistoryMode {
  Push = "push",
  Replace = "replace",
}

type Router = ReturnType<typeof useRouter>;

export function chatHref(address?: string): string {
  return address ? `${chatPath}#${address}` : chatPath;
}

export function roomAddress(room: { id: string }, peer?: { username?: string }): string {
  return peer?.username || room.id;
}

export function isRoomAt(address: string, room: { id: string }, peer?: { username?: string }): boolean {
  return address !== "" && (address === room.id || address === peer?.username);
}

let historyPatched = false;
let intendedAddress: string | null = null;

export function chatAddressOf(href: string): string | null {
  const url = new URL(href, window.location.href);
  if (url.origin !== window.location.origin || url.pathname !== chatPath) {
    return null;
  }
  return normalizeChatAddress(url.hash);
}

function isAt(href: string): boolean {
  return window.location.pathname + window.location.hash === href;
}

function moveWithinChat(address: string, navigation: HistoryMode) {
  const href = chatHref(address);
  if (navigation === HistoryMode.Replace || isAt(href)) {
    window.history.replaceState(null, "", href);
  } else {
    window.history.pushState(null, "", href);
  }
}

export function openChat(router: Router, address = "", navigation = HistoryMode.Push) {
  patchHistory();
  if (window.location.pathname === chatPath) {
    moveWithinChat(address, navigation);
    return;
  }
  intendedAddress = address;
  if (navigation === HistoryMode.Replace) {
    router.replace(chatHref(address), { scroll: false });
  } else {
    router.push(chatHref(address), { scroll: false });
  }
}

export function navigateTo(router: Router, href: string, navigation = HistoryMode.Push) {
  const address = chatAddressOf(href);
  if (address !== null) {
    openChat(router, address, navigation);
  } else if (navigation === HistoryMode.Replace) {
    router.replace(href);
  } else {
    router.push(href);
  }
}

function settleIntendedAddress(method: "pushState" | "replaceState") {
  if (intendedAddress === null) {
    return;
  }
  if (window.location.pathname !== chatPath) {
    if (method === "pushState") {
      intendedAddress = null;
    }
    return;
  }
  const href = chatHref(intendedAddress);
  intendedAddress = null;
  if (!isAt(href)) {
    window.history.replaceState(null, "", href);
  }
}

function interceptChatLink(event: MouseEvent) {
  if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
    return;
  }
  const anchor = event.target instanceof Element ? event.target.closest("a[href]") : null;
  if (!(anchor instanceof HTMLAnchorElement) || (anchor.target && anchor.target !== "_self") || anchor.hasAttribute("download")) {
    return;
  }
  const address = chatAddressOf(anchor.href);
  if (address === null) {
    return;
  }
  if (window.location.pathname === chatPath) {
    event.preventDefault();
    moveWithinChat(address, HistoryMode.Push);
  } else {
    intendedAddress = address;
  }
}

function syncRouterWithHash() {
  if (window.location.pathname === chatPath) {
    window.history.replaceState(window.history.state, "", chatHref(normalizeChatAddress(window.location.hash)));
  }
}

function announceAddressChange() {
  queueMicrotask(() => window.dispatchEvent(new Event(addressChangeEvent)));
}

function patchHistory() {
  if (historyPatched) return;
  historyPatched = true;
  for (const method of ["pushState", "replaceState"] as const) {
    const original = window.history[method];
    window.history[method] = function (this: History, ...args: Parameters<History["pushState"]>) {
      const result = original.apply(this, args);
      settleIntendedAddress(method);
      announceAddressChange();
      return result;
    };
  }
  window.addEventListener("popstate", () => {
    intendedAddress = null;
  });
  window.addEventListener("hashchange", syncRouterWithHash);
  document.addEventListener("click", interceptChatLink, true);
}

function subscribe(onChange: () => void): () => void {
  patchHistory();
  window.addEventListener("hashchange", onChange);
  window.addEventListener("popstate", onChange);
  window.addEventListener(addressChangeEvent, onChange);
  return () => {
    window.removeEventListener("hashchange", onChange);
    window.removeEventListener("popstate", onChange);
    window.removeEventListener(addressChangeEvent, onChange);
  };
}

function currentAddress(): string {
  return window.location.pathname === chatPath ? normalizeChatAddress(window.location.hash) : "";
}

export function settleChatAddress(router: Router, rendered: string, canonical: string) {
  if (currentAddress() !== rendered) {
    return;
  }
  if (canonical !== rendered || window.location.hash.slice(1) !== canonical) {
    openChat(router, canonical, HistoryMode.Replace);
  }
}

function noAddress(): string {
  return "";
}

export function useChatAddress(): string {
  return useSyncExternalStore(subscribe, currentAddress, noAddress);
}
