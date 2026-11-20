"use client";

import { create, fromJsonString, toJson, type MessageInitShape } from "@bufbuild/protobuf";
import { useEffect, useMemo, useRef, useState } from "react";
import useWebSocket, { ReadyState } from "react-use-websocket";

import { SessionCommandSchema, SessionError, SessionEventSchema, type DrawAction, type Session } from "@/gen/axon/game/v1/game_pb";
import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { apiBaseUrl } from "@/lib/connect/transport";
import { bearerPrefix } from "@/lib/ws/protocol";

const url = apiBaseUrl.replace(/^http/, "ws") + "/ws/game";
const retryDelay = (attempt: number) => Math.min(1_000 * 2 ** attempt, 15_000);
const reasons: Partial<Record<SessionError, string>> = {
  [SessionError.INVALID_COMMAND]: "That move is invalid.",
  [SessionError.NOT_FOUND]: "This game is unavailable.",
  [SessionError.NOT_YOUR_TURN]: "Wait for your turn.",
  [SessionError.STALE_MOVE]: "The board changed. Try again.",
  [SessionError.CELL_TAKEN]: "That square is already taken.",
  [SessionError.NOT_PLAYING]: "This game is not in progress.",
  [SessionError.UNAVAILABLE]: "The game service is unavailable.",
  [SessionError.UNDO_DISABLED]: "Undo is off in rated games.",
  [SessionError.ILLEGAL_MOVE]: "That move is not allowed.",
  [SessionError.NO_DRAW_OFFER]: "The draw offer is no longer open.",
  [SessionError.RATE_LIMITED]: "You are moving too fast. Try again.",
};

function command(action: MessageInitShape<typeof SessionCommandSchema>) {
  return toJson(SessionCommandSchema, create(SessionCommandSchema, action));
}

export function useGameSocket(initial: Session) {
  const [snapshot, setSnapshot] = useState<{ game: Session; at: number }>();
  const [mountedAt] = useState(() => performance.now());
  const [synchronized, setSynchronized] = useState(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [token, setToken] = useState(getAccessToken);
  const [attempt, setAttempt] = useState(0);
  const pendingRevision = useRef<number | undefined>(undefined);
  const current = snapshot && (snapshot.game.revision ?? snapshot.game.ply) >= (initial.revision ?? initial.ply) ? snapshot : undefined;
  const game = current?.game ?? initial;

  useEffect(() => {
    if (token) return;
    let active = true;
    void refreshSession().then(ok => { if (active && ok) setToken(getAccessToken()); });
    return () => { active = false; };
  }, [token, attempt]);

  const protocols = useMemo(() => token ? ["axon.game.v1", bearerPrefix + token] : undefined, [token]);
  const address = token ? `${url}?connection=${attempt}` : null;
  const { readyState, sendJsonMessage, getWebSocket } = useWebSocket(address, {
    protocols,
    retryOnError: true,
    reconnectAttempts: 15,
    reconnectInterval: retryDelay,
    shouldReconnect: event => event.code !== 1008 && getAccessToken() === token,
    onOpen: event => {
      const socket = event.target;
      if (socket instanceof WebSocket) socket.send(JSON.stringify(command({ action: { case: "subscribe", value: initial.id } })));
    },
    onClose: () => {
      pendingRevision.current = undefined;
      setPending(false);
      setSynchronized(false);
      const latest = getAccessToken();
      if (latest !== token) setToken(latest);
    },
    onMessage: event => {
      if (typeof event.data !== "string") return;
      try {
        const update = fromJsonString(SessionEventSchema, event.data).update;
        if (update.case === "error") {
          pendingRevision.current = undefined;
          setPending(false);
          setError(reasons[update.value] ?? "Could not play that move.");
        } else if (update.case === "session" && update.value.id === initial.id) {
          const next = update.value;
          if (pendingRevision.current !== undefined && (next.revision ?? next.ply) > pendingRevision.current) {
            pendingRevision.current = undefined;
            setPending(false);
          }
          setSnapshot(previous => previous && (previous.game.revision ?? previous.game.ply) > (next.revision ?? next.ply) ? previous : { game: next, at: performance.now() });
          setSynchronized(true);
        }
      } catch {
        setError("Could not read the game update.");
      }
    },
    filter: () => false,
  });

  const connected = readyState === ReadyState.OPEN && synchronized;
  const send = (action: MessageInitShape<typeof SessionCommandSchema>["action"]) => {
    if (!connected || pendingRevision.current !== undefined) return;
    pendingRevision.current = game.revision ?? game.ply;
    setPending(true);
    setError("");
    sendJsonMessage(command({ action }), false);
  };

  useEffect(() => {
    if (!pending) return;
    const timeout = setTimeout(() => {
      const socket = getWebSocket();
      if (socket?.readyState === WebSocket.OPEN) socket.close(4000, "Resync game");
    }, 5000);
    return () => clearTimeout(timeout);
  }, [pending, getWebSocket]);

  return {
    game, connected, pending, error, receivedAt: current?.at ?? mountedAt,
    retry: () => {
      setError("");
      setToken(getAccessToken());
      setAttempt(value => value + 1);
    },
    undo: () => {
      if (!connected || pendingRevision.current !== undefined || game.ply === 0) return;
      pendingRevision.current = game.revision ?? game.ply;
      setPending(true);
      setError("");
      sendJsonMessage(command({ action: { case: "undo", value: { revision: game.revision } } }), false);
    },
    move: (cell: number) => send({ case: "move", value: { ply: game.ply, cell, revision: game.revision } }),
    chessMove: (uci: string) => send({ case: "chessMove", value: { ply: game.ply, uci, revision: game.revision } }),
    draw: (action: DrawAction) => send({ case: "draw", value: { action } }),
  };
}
