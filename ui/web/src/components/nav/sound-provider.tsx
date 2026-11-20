"use client";

import { createContext, useContext, useEffect, useState, useSyncExternalStore, type ReactNode } from "react";

import { AppSounds } from "@/lib/sounds";

const SoundContext = createContext<{
  player: AppSounds;
  calls: boolean;
  messages: boolean;
  setCalls: (enabled: boolean) => void;
  setMessages: (enabled: boolean) => void;
} | null>(null);

const changes = new EventTarget();
const callsKey = "axon:call-sounds";
const messagesKey = "axon:message-sounds";
const defaults = () => true;

function subscribe(listener: () => void) {
  changes.addEventListener("change", listener);
  window.addEventListener("storage", listener);
  return () => {
    changes.removeEventListener("change", listener);
    window.removeEventListener("storage", listener);
  };
}

const preferences = new Map<string, boolean>();
function enabled(key: string) {
  try { return localStorage.getItem(key) !== "off"; }
  catch { return preferences.get(key) ?? true; }
}

function save(key: string, value: boolean) {
  preferences.set(key, value);
  try { localStorage.setItem(key, value ? "on" : "off"); } catch {}
  changes.dispatchEvent(new Event("change"));
}

export function SoundProvider({ children }: { children: ReactNode }) {
  const [player] = useState(() => new AppSounds());
  const calls = useSyncExternalStore(subscribe, () => enabled(callsKey), defaults);
  const messages = useSyncExternalStore(subscribe, () => enabled(messagesKey), defaults);

  useEffect(() => {
    if (!calls) player.muteCalls();
    if (!messages) player.muteMessages();
    if (!calls && !messages) return;
    document.addEventListener("pointerdown", player.unlock, true);
    document.addEventListener("keydown", player.unlock, true);
    document.addEventListener("click", player.unlock, true);
    document.addEventListener("visibilitychange", player.syncRing);
    return () => {
      document.removeEventListener("pointerdown", player.unlock, true);
      document.removeEventListener("keydown", player.unlock, true);
      document.removeEventListener("click", player.unlock, true);
      document.removeEventListener("visibilitychange", player.syncRing);
    };
  }, [calls, messages, player]);

  useEffect(() => () => player.close(), [player]);

  return <SoundContext value={{
    player, calls, messages,
    setCalls: enabled => {
      if (enabled) player.unlock();
      else player.muteCalls();
      save(callsKey, enabled);
    },
    setMessages: enabled => {
      if (enabled) player.unlock();
      else player.muteMessages();
      save(messagesKey, enabled);
    },
  }}>{children}</SoundContext>;
}

export function useSounds() {
  const sounds = useContext(SoundContext);
  if (!sounds) throw new Error("Sounds require a signed-in session");
  return sounds;
}
