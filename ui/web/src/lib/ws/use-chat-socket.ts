"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import useWebSocket, { ReadyState } from "react-use-websocket";

import { getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";
import { apiBaseUrl } from "@/lib/connect/transport";
import {
  bearerPrefix,
  closeCodes,
  newClientID,
  parseFrame,
  subprotocol,
  type Incoming,
  type WireMessage,
} from "@/lib/ws/protocol";

const socketUrl = apiBaseUrl.replace(/^http/, "ws") + "/ws/chat";

const maxRetries = 30;
const reconnectDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 15_000);

const noHistory: WireMessage[] = [];

export interface ChatSocket {

  messages: WireMessage[];

  connected: boolean;

  error: string | null;

  send: (body: string) => boolean;
}

export function useChatSocket(roomID: string, history?: WireMessage[]): ChatSocket {
  const [delivered, setDelivered] = useState<WireMessage[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [token, setToken] = useState<string | null>(null);

  const messages = useMemo(
    () => merge(history ?? noHistory, delivered),
    [history, delivered],
  );

  const seen = useRef(0);
  useEffect(() => {
    seen.current = Math.max(seen.current, messages[messages.length - 1]?.seq ?? 0);
  }, [messages]);

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

  const { sendJsonMessage, lastMessage, readyState } = useWebSocket(
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
        return event.code !== closeCodes.unauthenticated;
      },
      onOpen: () => {

        sendJsonMessage({ type: "subscribe", room_id: roomID, since: seen.current });
      },
    },
  );

  useEffect(() => {
    if (!lastMessage?.data || typeof lastMessage.data !== "string") {
      return;
    }

    const frame = parseFrame(lastMessage.data);
    if (!frame) {
      return;
    }

    handleFrame(frame, setDelivered, setError);
  }, [lastMessage]);

  const send = useCallback(
    (body: string) => {
      if (readyState !== ReadyState.OPEN || !body.trim()) {
        return false;
      }

      sendJsonMessage({
        type: "send",
        room_id: roomID,

        client_id: newClientID(),
        body,
      });
      setError(null);
      return true;
    },
    [readyState, roomID, sendJsonMessage],
  );

  return {
    messages,
    connected: readyState === ReadyState.OPEN,
    error,
    send,
  };
}

function merge(current: WireMessage[], incoming: WireMessage[]): WireMessage[] {
  const known = new Set(current.map((m) => m.seq));
  const added = incoming.filter((m) => !known.has(m.seq));
  if (added.length === 0) {
    return current;
  }
  return [...current, ...added].sort((a, b) => a.seq - b.seq);
}

function handleFrame(
  frame: Incoming,
  setDelivered: React.Dispatch<React.SetStateAction<WireMessage[]>>,
  setError: React.Dispatch<React.SetStateAction<string | null>>,
): void {
  switch (frame.type) {
    case "message":
      setDelivered((current) => merge(current, [frame.message]));
      break;

    case "subscribed":

      break;

    case "ack":

      break;

    case "error":
      setError(frame.reason || frame.code);
      break;
  }
}
