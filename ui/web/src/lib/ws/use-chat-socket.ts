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

/**
 * socketUrl is the gateway's socket, derived from the same base URL the Connect
 * calls use. The gateway terminates it and dials chat itself; the browser never
 * addresses a service directly.
 */
const socketUrl = apiBaseUrl.replace(/^http/, "ws") + "/ws/chat";

/**
 * Reconnection backoff, set explicitly rather than left to the library's
 * defaults, as rules/nextjs-web.md asks. A dropped socket is normal — a laptop
 * lid, a tunnel, a redeploy — so the first retries are quick, and the ceiling
 * keeps a client that has been offline for an hour from hammering the gateway
 * the moment it comes back.
 */
const maxRetries = 30;
const reconnectDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 15_000);

/**
 * The stand-in for a history page that has not arrived. One shared array rather
 * than a literal per render, so a room with nothing in it yet keeps handing back
 * the same empty conversation instead of a new one each time.
 */
const noHistory: WireMessage[] = [];

export interface ChatSocket {
  /** Messages in the room, oldest first. */
  messages: WireMessage[];

  /** Whether the socket is currently connected. */
  connected: boolean;

  /** The last refusal the server sent, cleared by the next successful send. */
  error: string | null;

  /** Sends a message. Returns false when the socket is not up. */
  send: (body: string) => boolean;
}

/**
 * useChatSocket keeps one room's conversation live.
 *
 * The reconnect story is the whole point of the hook. Every message carries its
 * position, the newest one is remembered across drops, and a reconnect
 * subscribes from there — so a socket that dies mid-conversation costs a round
 * trip and nothing else. That is also why the token expiring is not an error
 * here: the server closes with 4402, this refreshes and reconnects from the
 * same position.
 */
export function useChatSocket(roomID: string, history?: WireMessage[]): ChatSocket {
  const [delivered, setDelivered] = useState<WireMessage[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [token, setToken] = useState<string | null>(null);

  /**
   * The conversation is the two halves put together, worked out as it renders
   * rather than accumulated in one place.
   *
   * History cannot be the socket's initial state: the request that fetches it
   * has not answered by the first render, and an initial value is read once and
   * never again — a page opened cold would show an empty room and quietly stay
   * empty. Copying it into state when it lands would mean a setState in an
   * effect, which is a cascading render and a second copy of the same list to
   * keep in step. Combining the two is neither.
   */
  const messages = useMemo(
    () => merge(history ?? noHistory, delivered),
    [history, delivered],
  );

  /**
   * The newest position on screen, mirrored into a ref because the reconnect
   * reads it from inside a callback that must not be re-created every time a
   * message arrives.
   *
   * It has to include history, not only what the socket delivered. A client
   * that holds a page of messages and asks for everything after nothing is
   * answered with nothing at all: the socket owes a reconnecting client the
   * gap, and history is what says where the gap starts.
   */
  const seen = useRef(0);
  useEffect(() => {
    seen.current = Math.max(seen.current, messages[messages.length - 1]?.seq ?? 0);
  }, [messages]);

  // A token before connecting, and a fresh one after an expiry closed the
  // socket. getAccessToken returns null once the token is close enough to
  // expiry to be useless, which is exactly when refreshing is worth the round
  // trip rather than opening a socket that will close in seconds.
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
        // An expired token is not a reason to stop trying — it is a reason to
        // get another one. Dropping the token sends the effect above back for a
        // fresh one, and the socket reopens with it.
        if (event.code === closeCodes.tokenExpired) {
          setToken(null);
          return false;
        }
        return event.code !== closeCodes.unauthenticated;
      },
      onOpen: () => {
        // Everything after the position this client already holds. On a first
        // connection that is nothing, because history came from ListMessages;
        // on a reconnect it is exactly what was missed.
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
        // The id this client gives the message. It comes back on the ack and on
        // the message itself, and it is what makes a resend after a dropped
        // connection land as the same message rather than a second one.
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

/**
 * merge folds messages into the conversation, in order and without repeats.
 *
 * A message can arrive twice — history overlapping live delivery, a catch-up
 * repeating a position, a reconnect asking again. Position is what identifies a
 * message in a room, so that is what duplicates are judged by; ids would work
 * too, but seq is also the sort key, and one key is fewer things to get wrong.
 *
 * The current array is returned unchanged when there is nothing new, so a
 * repeat costs no render.
 */
function merge(current: WireMessage[], incoming: WireMessage[]): WireMessage[] {
  const known = new Set(current.map((m) => m.seq));
  const added = incoming.filter((m) => !known.has(m.seq));
  if (added.length === 0) {
    return current;
  }
  return [...current, ...added].sort((a, b) => a.seq - b.seq);
}

/** handleFrame applies one server frame to the hook's state. */
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
      // The room's current position. Nothing to do with it but notice: the
      // catch-up frames that follow carry the messages themselves.
      break;

    case "ack":
      // The message is durable, and the copy that renders arrives as an
      // ordinary message frame — which is also what moves the position. There
      // is nothing to do here but not treat it as unknown.
      break;

    case "error":
      setError(frame.reason || frame.code);
      break;
  }
}
