"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import useWebSocket, { ReadyState } from "react-use-websocket";

import { clearAccessToken, getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";
import { apiBaseUrl } from "@/lib/connect/transport";
import { applyUpdate, patchHistory, refreshNewestPage } from "@/lib/query/chat";
import { invalidateProfilesOf } from "@/lib/query/people";
import {
  bearerPrefix,
  closeCodes,
  newClientID,
  parseFrame,
  subprotocol,
  type Incoming,
  type WireMessage,
} from "@/lib/ws/protocol";
import { useTyping } from "@/lib/ws/use-typing";

const socketUrl = apiBaseUrl.replace(/^http/, "ws") + "/ws/chat";

const maxRetries = 30;
const reconnectDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 15_000);

const typingThrottle = 3_000;

const noHistory: WireMessage[] = [];

const nobody: ReadonlySet<string> = new Set();

export type ChatTarget = { roomId: string } | { peerId: string };

export interface SendOptions {
  uploadId?: string;
  replyToId?: string;
}

export interface ChatSocket {
  messages: WireMessage[];

  connected: boolean;

  error: string | null;

  newRoomId: string | null;

  revoked: boolean;

  readSeqs: Map<string, number>;

  typing: ReadonlySet<string>;

  removed: boolean;

  send: (body: string, options?: SendOptions) => boolean;

  forward: (messageId: string, roomId: string, onForwarded?: () => void) => boolean;

  edit: (messageId: string, body: string, onSettled?: (saved: boolean) => void) => boolean;

  remove: (messageId: string) => boolean;

  notifyTyping: () => void;
}

export function useChatSocket(
  target: ChatTarget,
  history?: WireMessage[],
  presenceOf?: string,
): ChatSocket {
  const queryClient = useQueryClient();
  const roomID = "roomId" in target ? target.roomId : undefined;
  const peerID = "peerId" in target ? target.peerId : undefined;

  const [delivered, setDelivered] = useState<WireMessage[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [newRoomId, setNewRoomId] = useState<string | null>(null);
  const [revoked, setRevoked] = useState(false);
  const [readSeqs, setReadSeqs] = useState<Map<string, number>>(new Map());
  const [removed, setRemoved] = useState(false);
  const [token, setToken] = useState<string | null>(null);
  const typingTracker = useTyping();
  const forwards = useRef(new Map<string, () => void>());
  const edits = useRef(new Map<string, PendingEdit>());
  const opened = useRef(false);

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

  const onFrame = (frame: Incoming) => {
    switch (frame.type) {
      case "message":
        typingTracker.stop(frame.message.room_id, frame.message.author_id);
        setDelivered((current) => merge(current, [frame.message]));
        break;

      case "message_updated":
        if (frame.message.room_id === roomID) {
          setDelivered((current) => applyUpdate(current, frame.message));
          patchHistory(queryClient, frame.message);
        }
        for (const [clientID, pending] of edits.current) {
          if (pending.messageId === frame.message.id) {
            edits.current.delete(clientID);
            pending.settle(true);
          }
        }
        break;

      case "room_removed":
        if (frame.room_id === roomID) {
          setRemoved(true);
        }
        break;

      case "ack":
        forwards.current.get(frame.client_id)?.();
        forwards.current.delete(frame.client_id);
        if (!roomID && frame.room_id) {
          setNewRoomId(frame.room_id);
        }
        break;

      case "read":
        setReadSeqs((current) => {
          if ((current.get(frame.user_id) ?? 0) >= frame.seq) {
            return current;
          }
          const next = new Map(current);
          next.set(frame.user_id, frame.seq);
          return next;
        });
        break;

      case "typing":
        typingTracker.start(frame.room_id, frame.user_id);
        break;

      case "presence":
        void invalidateProfilesOf(queryClient, frame.user_id);
        break;

      case "error":
        if (frame.client_id) {
          forwards.current.delete(frame.client_id);
          edits.current.get(frame.client_id)?.settle(false);
          edits.current.delete(frame.client_id);
        }
        setError(frame.reason || frame.code);
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
        if (roomID) {
          if (opened.current) {
            void refreshNewestPage(queryClient, roomID);
          }
          sendJsonMessage({ type: "subscribe", room_id: roomID, since: seen.current });
        }
        opened.current = true;
      },
      onClose: () => {
        const unconfirmed = forwards.current.size + edits.current.size;
        forwards.current.clear();
        for (const pending of edits.current.values()) {
          pending.settle(false);
        }
        edits.current.clear();
        if (unconfirmed > 0) {
          setError("The connection dropped before your change was confirmed. Check the chat before trying again.");
        }
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

  const connected = readyState === ReadyState.OPEN;

  useEffect(() => {
    if (connected && presenceOf) {
      sendJsonMessage({ type: "watch_presence", to_user_id: presenceOf });
    }
  }, [connected, presenceOf, sendJsonMessage]);

  const lastTypingSent = useRef(0);
  const notifyTyping = useCallback(() => {
    if (!connected || !roomID) {
      return;
    }
    const now = Date.now();
    if (now - lastTypingSent.current < typingThrottle) {
      return;
    }
    lastTypingSent.current = now;
    sendJsonMessage({ type: "typing", room_id: roomID });
  }, [connected, roomID, sendJsonMessage]);

  const send = useCallback(
    (body: string, options: SendOptions = {}) => {
      if (readyState !== ReadyState.OPEN || (!body.trim() && !options.uploadId)) {
        return false;
      }

      const frame = {
        type: "send" as const,
        client_id: newClientID(),
        body,
        ...(options.uploadId ? { upload_id: options.uploadId } : {}),
        ...(options.replyToId ? { reply_to_id: options.replyToId } : {}),
      };

      if (roomID) {
        sendJsonMessage({ ...frame, room_id: roomID });
      } else if (peerID) {
        sendJsonMessage({ ...frame, to_user_id: peerID });
      } else {
        return false;
      }

      lastTypingSent.current = 0;
      setError(null);
      return true;
    },
    [readyState, roomID, peerID, sendJsonMessage],
  );

  const forward = useCallback(
    (messageId: string, targetRoomId: string, onForwarded?: () => void) => {
      if (readyState !== ReadyState.OPEN) {
        return false;
      }
      const clientID = newClientID();
      if (onForwarded) {
        forwards.current.set(clientID, onForwarded);
      }
      sendJsonMessage({
        type: "send",
        room_id: targetRoomId,
        client_id: clientID,
        body: "",
        forwarded_from_id: messageId,
      });
      setError(null);
      return true;
    },
    [readyState, sendJsonMessage],
  );

  const edit = useCallback(
    (messageId: string, body: string, onSettled?: (saved: boolean) => void) => {
      if (readyState !== ReadyState.OPEN) {
        return false;
      }
      const clientID = newClientID();
      if (onSettled) {
        edits.current.set(clientID, { messageId, settle: onSettled });
      }
      sendJsonMessage({ type: "edit", client_id: clientID, message_id: messageId, body });
      setError(null);
      return true;
    },
    [readyState, sendJsonMessage],
  );

  const remove = useCallback(
    (messageId: string) => {
      if (readyState !== ReadyState.OPEN) {
        return false;
      }
      sendJsonMessage({ type: "delete", message_id: messageId });
      setError(null);
      return true;
    },
    [readyState, sendJsonMessage],
  );

  return {
    messages,
    connected,
    error,
    newRoomId,
    revoked,
    readSeqs,
    typing: (roomID && typingTracker.typing.get(roomID)) || nobody,
    removed,
    send,
    forward,
    edit,
    remove,
    notifyTyping,
  };
}

interface PendingEdit {
  messageId: string;
  settle: (saved: boolean) => void;
}

function merge(current: WireMessage[], incoming: WireMessage[]): WireMessage[] {
  const known = new Set(current.map((m) => m.seq));
  const added = incoming.filter((m) => !known.has(m.seq));
  if (added.length === 0) {
    return current;
  }
  return [...current, ...added].sort((a, b) => a.seq - b.seq);
}
