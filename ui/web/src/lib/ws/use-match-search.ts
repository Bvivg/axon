"use client";

import { create, fromJsonString, toJson } from "@bufbuild/protobuf";
import { useEffect, useMemo, useRef, useState } from "react";
import useWebSocket from "react-use-websocket";

import { MatchStatus, SessionCommandSchema, SessionEventSchema, type GameOptions, type MatchTicket } from "@/gen/axon/game/v1/game_pb";
import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { gameClient } from "@/lib/connect/clients";
import { apiBaseUrl } from "@/lib/connect/transport";
import { bearerPrefix } from "@/lib/ws/protocol";

const url = apiBaseUrl.replace(/^http/, "ws") + "/ws/game";
const retryDelay = (attempt: number) => Math.min(1_000 * 2 ** attempt, 10_000);

export type MatchTable = { seats?: number; options?: GameOptions };

export function useMatchSearch(game: string, onMatched: (sessionId: string) => void) {
  const [ticket, setTicket] = useState<MatchTicket>();
  const [receivedAt, setReceivedAt] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [token, setToken] = useState(getAccessToken);
  const matched = useRef(onMatched);
  const searching = ticket?.status === MatchStatus.SEARCHING;

  useEffect(() => {
    matched.current = onMatched;
  });

  const accept = (next: MatchTicket | undefined) => {
    if (next?.status === MatchStatus.MATCHED && next.sessionId) {
      setTicket(undefined);
      matched.current(next.sessionId);
      return;
    }
    setTicket(next?.status === MatchStatus.SEARCHING ? next : undefined);
    setReceivedAt(performance.now());
  };

  useEffect(() => {
    let active = true;
    void gameClient.getMatch({ game }).then(({ ticket: next }) => { if (active) accept(next); }, () => {});
    return () => { active = false; };
  }, [game]);

  useEffect(() => {
    if (!searching || token) return;
    let active = true;
    void refreshSession().then(ok => { if (active && ok) setToken(getAccessToken()); });
    return () => { active = false; };
  }, [searching, token]);

  const protocols = useMemo(() => token ? ["axon.game.v1", bearerPrefix + token] : undefined, [token]);
  useWebSocket(searching && token ? url : null, {
    protocols,
    retryOnError: true,
    reconnectAttempts: 10,
    reconnectInterval: retryDelay,
    shouldReconnect: event => event.code !== 1008,
    onOpen: event => {
      const socket = event.target;
      if (socket instanceof WebSocket) socket.send(JSON.stringify(toJson(SessionCommandSchema, create(SessionCommandSchema, { action: { case: "subscribeMatch", value: game } }))));
    },
    onClose: () => {
      const latest = getAccessToken();
      if (latest !== token) setToken(latest);
    },
    onMessage: event => {
      if (typeof event.data !== "string") return;
      try {
        const update = fromJsonString(SessionEventSchema, event.data).update;
        if (update.case === "match") accept(update.value);
      } catch {
        setError("Could not read the search status.");
      }
    },
    filter: () => false,
  });

  return {
    ticket,
    receivedAt,
    searching,
    busy,
    error,
    start: async (table: MatchTable = {}) => {
      setBusy(true);
      setError("");
      try {
        const { ticket: next } = await gameClient.findMatch({ game, requestId: crypto.randomUUID(), seats: table.seats, options: table.options });
        setToken(getAccessToken());
        accept(next);
      } catch {
        setError("Could not start the search. Try again.");
      } finally {
        setBusy(false);
      }
    },
    cancel: async () => {
      setBusy(true);
      try {
        const { ticket: next } = await gameClient.cancelMatch({ game });
        accept(next);
      } catch {
        setError("Could not cancel the search.");
      } finally {
        setBusy(false);
      }
    },
  };
}
