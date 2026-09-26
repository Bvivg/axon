"use client";

import { useCallback, useEffect, useRef, useState } from "react";

const typingTimeout = 5_000;

export type TypingByRoom = ReadonlyMap<string, ReadonlySet<string>>;

const nobody: TypingByRoom = new Map();

export function useTyping() {
  const [typing, setTyping] = useState<TypingByRoom>(nobody);
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>());

  const stop = useCallback((roomID: string, userID: string) => {
    const key = `${roomID}/${userID}`;
    const timer = timers.current.get(key);
    if (timer !== undefined) {
      clearTimeout(timer);
      timers.current.delete(key);
    }

    setTyping((current) => {
      const users = current.get(roomID);
      if (!users?.has(userID)) {
        return current;
      }
      const rest = new Set(users);
      rest.delete(userID);
      const next = new Map(current);
      if (rest.size === 0) {
        next.delete(roomID);
      } else {
        next.set(roomID, rest);
      }
      return next;
    });
  }, []);

  const start = useCallback(
    (roomID: string, userID: string) => {
      const key = `${roomID}/${userID}`;
      const timer = timers.current.get(key);
      if (timer !== undefined) {
        clearTimeout(timer);
      }
      timers.current.set(
        key,
        setTimeout(() => stop(roomID, userID), typingTimeout),
      );

      setTyping((current) => {
        const users = current.get(roomID);
        if (users?.has(userID)) {
          return current;
        }
        const next = new Map(current);
        next.set(roomID, new Set([...(users ?? []), userID]));
        return next;
      });
    },
    [stop],
  );

  useEffect(() => {
    const pending = timers.current;
    return () => {
      for (const timer of pending.values()) {
        clearTimeout(timer);
      }
      pending.clear();
    };
  }, []);

  return { typing, start, stop };
}
