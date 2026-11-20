"use client";

import { useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import useWebSocket, { ReadyState } from "react-use-websocket";

import { clearAccessToken, getAccessToken } from "@/lib/auth/tokens";
import { refreshSession } from "@/lib/auth/refresh";
import { messageLengthError, messageTooLong } from "@/lib/chat/message-limits";
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
  type Outgoing,
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
  uploadIds?: string[];
  replyToId?: string;
}

export interface ChatSocket {
  messages: WireMessage[];

  connected: boolean;

  error: string | null;
  outgoing: OutgoingMessage[];
  retrySend: (id: string) => void;
  dismissSend: (id: string) => void;

  newRoomId: string | null;

  revoked: boolean;

  readSeqs: Map<string, number>;

  typing: ReadonlySet<string>;

  removed: boolean;

  send: (body: string, options?: SendOptions) => boolean;

  edit: (messageId: string, body: string, onSettled?: (saved: boolean) => void) => boolean;

  remove: (messageId: string | string[]) => boolean;

  notifyTyping: () => void;
}

type SendFrame = Extract<Outgoing, { type: "send" }>;
export interface OutgoingMessage {
  frame: SendFrame;
  status: "sending" | "sent" | "unconfirmed" | "failed";
  createdAt: string;
  seq?: number;
  roomId?: string;
  reason?: string;
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
  const [outgoing, setOutgoing] = useState<OutgoingMessage[]>([]);
  const typingTracker = useTyping();
  const edits = useRef(new Map<string, PendingEdit>());
  const sends = useRef(new Map<string, ReturnType<typeof setTimeout>>());
  const opened = useRef(false);

  useEffect(() => () => {
    for (const timer of sends.current.values()) clearTimeout(timer);
    sends.current.clear();
  }, []);

  const messages = useMemo(
    () => merge(history ?? noHistory, delivered),
    [history, delivered],
  );

  const visibleOutgoing = useMemo(() => outgoing.filter(item => !messages.some(message => message.client_id === item.frame.client_id)), [outgoing, messages]);

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
    const confirm = (id: string) => {
      clearTimeout(sends.current.get(id));
      sends.current.delete(id);
      setOutgoing(current => current.filter(item => item.frame.client_id !== id));
    };
    switch (frame.type) {
      case "message":
        if (frame.message.client_id) confirm(frame.message.client_id);
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

      case "subscribed":
        if (frame.behind && frame.room_id === roomID) {
          setDelivered([]);
          void refreshNewestPage(queryClient, roomID);
        }
        break;

      case "room_removed":
        if (frame.room_id === roomID) {
          setRemoved(true);
        }
        break;

      case "ack":
        clearTimeout(sends.current.get(frame.client_id));
        sends.current.delete(frame.client_id);
        setOutgoing(current => current.map(item => item.frame.client_id === frame.client_id
          ? { ...item, status: "sent", seq: frame.seq, roomId: frame.room_id, reason: undefined } : item));
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
          const isSend = sends.current.has(frame.client_id);
          clearTimeout(sends.current.get(frame.client_id));
          sends.current.delete(frame.client_id);
          setOutgoing(current => current.map(item => item.frame.client_id === frame.client_id
            ? { ...item, status: "failed", reason: frame.reason || "The server rejected this message." } : item));
          edits.current.get(frame.client_id)?.settle(false);
          edits.current.delete(frame.client_id);
          if (isSend) break;
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
      onClose: (event) => {
        for (const timer of sends.current.values()) clearTimeout(timer);
        sends.current.clear();
        setOutgoing(current => current.map(item => item.status === "sending" ? {
          ...item, status: event.code === 1009 ? "failed" : "unconfirmed",
          reason: event.code === 1009 ? "This message is too large to send." : undefined,
        } : item));
        const unconfirmed = edits.current.size;
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

  const transmit = useCallback((frame: SendFrame) => {
    const id = frame.client_id;
    if (!connected || sends.current.has(id)) return false;
    sends.current.set(id, setTimeout(() => {
      sends.current.delete(id);
      setOutgoing(current => current.map(item => item.frame.client_id === id ? { ...item, status: "unconfirmed" } : item));
    }, 30_000));
    setOutgoing(current => {
      const previous = current.find(item => item.frame.client_id === id);
      const next: OutgoingMessage = { frame, status: "sending", createdAt: previous?.createdAt ?? new Date().toISOString() };
      return previous ? current.map(item => item.frame.client_id === id ? next : item) : [...current, next];
    });
    sendJsonMessage(frame, false);
    return true;
  }, [connected, sendJsonMessage]);

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
      const uploads = options.uploadIds?.length ? options.uploadIds : options.uploadId ? [options.uploadId] : [];
      if (readyState !== ReadyState.OPEN || (!body.trim() && uploads.length === 0)) {
        return false;
      }

      if (!roomID && !peerID) return false;
      if (messageTooLong(body)) {
        setError(messageLengthError);
        return false;
      }
      setError(null);
      const clientID = newClientID();
      const frame = {
        type: "send" as const,
        client_id: clientID,
        body: body.trim(),
        ...(uploads.length === 1 ? { upload_id: uploads[0] } : {}),
        ...(uploads.length > 1 ? { upload_ids: uploads } : {}),
        ...(options.replyToId ? { reply_to_id: options.replyToId } : {}),
      };

      lastTypingSent.current = 0;
      return transmit(roomID ? { ...frame, room_id: roomID } : { ...frame, to_user_id: peerID! });
    },
    [readyState, roomID, peerID, transmit],
  );

  const edit = useCallback(
    (messageId: string, body: string, onSettled?: (saved: boolean) => void) => {
      if (readyState !== ReadyState.OPEN) {
        return false;
      }
      if (messageTooLong(body)) {
        setError(messageLengthError);
        return false;
      }
      const clientID = newClientID();
      if (onSettled) {
        edits.current.set(clientID, { messageId, settle: onSettled });
      }
      sendJsonMessage({ type: "edit", client_id: clientID, message_id: messageId, body: body.trim() });
      setError(null);
      return true;
    },
    [readyState, sendJsonMessage],
  );

  const remove = useCallback(
    (messageId: string | string[]) => {
      if (readyState !== ReadyState.OPEN) {
        return false;
      }
      sendJsonMessage(
        Array.isArray(messageId)
          ? { type: "delete", message_ids: messageId }
          : { type: "delete", message_id: messageId },
      );
      setError(null);
      return true;
    },
    [readyState, sendJsonMessage],
  );

  return {
    messages,
    connected,
    error,
    outgoing: visibleOutgoing,
    retrySend: id => {
      const item = outgoing.find(item => item.frame.client_id === id && item.status === "unconfirmed");
      if (item) transmit(item.frame);
    },
    dismissSend: id => setOutgoing(current => current.filter(item => item.frame.client_id !== id)),
    newRoomId,
    revoked,
    readSeqs,
    typing: (roomID && typingTracker.typing.get(roomID)) || nobody,
    removed,
    send,
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
