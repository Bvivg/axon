"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useRef, useState } from "react";
import useWebSocket, { ReadyState } from "react-use-websocket";

import { signInPath } from "@/lib/auth/guards";
import { useSession } from "@/lib/auth/session";
import { clearAccessToken, getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";
import { apiBaseUrl } from "@/lib/connect/transport";
import { chatKeys, forgetHistory, patchHistory } from "@/lib/query/chat";
import { invalidateProfilesOf } from "@/lib/query/people";
import { bearerPrefix, closeCodes, parseFrame, subprotocol, type Incoming } from "@/lib/ws/protocol";
import { useTyping, type TypingByRoom } from "@/lib/ws/use-typing";

const socketUrl = apiBaseUrl.replace(/^http/, "ws") + "/ws/chat";

const maxRetries = 30;
const reconnectDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 15_000);

export function useRoomActivitySocket(roomIds: string[], peerIds: string[]): TypingByRoom {
  const queryClient = useQueryClient();
  const router = useRouter();
  const { signOut } = useSession();

  const [token, setToken] = useState<string | null>(null);
  const subscribed = useRef(new Set<string>());
  const watched = useRef(new Set<string>());
  const opened = useRef(false);
  const typingTracker = useTyping();

  useEffect(() => {
    if (token) {
      return;
    }

    let cancelled = false;

    void (async () => {
      const existing = getAccessToken();
      if (existing) {
        if (!cancelled) setToken(existing);
        return;
      }

      await refreshSession();
      const refreshed = getAccessToken();
      if (!cancelled && refreshed) {
        setToken(refreshed);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [token]);

  const protocols = useMemo(
    () => (token ? [subprotocol, bearerPrefix + token] : undefined),
    [token],
  );

  const [revoked, setRevoked] = useState(false);

  const onFrame = (frame: Incoming) => {
    switch (frame.type) {
      case "message":
        typingTracker.stop(frame.message.room_id, frame.message.author_id);
        void queryClient.invalidateQueries({ queryKey: chatKeys.lastMessage(frame.message.room_id) });
        void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
        break;

      case "read":
        void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
        break;

      case "typing":
        typingTracker.start(frame.room_id, frame.user_id);
        break;

      case "presence":
        void invalidateProfilesOf(queryClient, frame.user_id);
        break;

      case "message_updated":
        patchHistory(queryClient, frame.message);
        void queryClient.invalidateQueries({ queryKey: chatKeys.lastMessage(frame.message.room_id) });
        void queryClient.invalidateQueries({ queryKey: chatKeys.media(frame.message.room_id) });
        break;

      case "room_added":
        if (!subscribed.current.has(frame.room_id)) {
          void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
        }
        break;

      case "room_removed":
        subscribed.current.delete(frame.room_id);
        void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
        forgetHistory(queryClient, frame.room_id);
        break;
    }
  };

  const { sendJsonMessage, readyState } = useWebSocket(
    token ? socketUrl : null,
    {
      protocols,
      share: false,
      retryOnError: true,
      reconnectAttempts: maxRetries,
      reconnectInterval: reconnectDelay,
      shouldReconnect: (event) => {
        if (event.code === closeCodes.tokenExpired) {
          setToken(null);
          return false;
        }
        if (event.code === closeCodes.sessionRevoked) {
          clearAccessToken();
          setToken(null);
          setRevoked(true);
          return false;
        }
        return event.code !== closeCodes.unauthenticated;
      },
      onOpen: () => {
        subscribed.current.clear();
        watched.current.clear();
        if (opened.current) {
          void queryClient.invalidateQueries({ queryKey: chatKeys.lastMessages });
          void queryClient.invalidateQueries({ queryKey: chatKeys.rooms });
        }
        opened.current = true;
      },
      onMessage: (event) => {
        if (typeof event.data !== "string") {
          return;
        }
        const frame = parseFrame(event.data);
        if (frame) {
          onFrame(frame);
        }
      },
      filter: () => false,
    },
  );

  useEffect(() => {
    if (revoked) {
      void signOut().then(() => router.replace(signInPath));
    }
  }, [revoked, signOut, router]);

  useEffect(() => {
    if (readyState !== ReadyState.OPEN) {
      return;
    }
    for (const id of roomIds) {
      if (!subscribed.current.has(id)) {
        sendJsonMessage({ type: "subscribe", room_id: id, since: 0 });
        subscribed.current.add(id);
      }
    }
  }, [roomIds, readyState, sendJsonMessage]);

  useEffect(() => {
    if (readyState !== ReadyState.OPEN) {
      return;
    }
    for (const id of peerIds) {
      if (!watched.current.has(id)) {
        sendJsonMessage({ type: "watch_presence", to_user_id: id });
        watched.current.add(id);
      }
    }
  }, [peerIds, readyState, sendJsonMessage]);

  return typingTracker.typing;
}
