"use client";

import { create, fromJsonString, toJson } from "@bufbuild/protobuf";
import { useEffect, useMemo, useState } from "react";
import useWebSocket, { ReadyState } from "react-use-websocket";

import { SessionCommandSchema, SessionEventSchema, type Session } from "@/gen/axon/game/v1/game_pb";
import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { apiBaseUrl } from "@/lib/connect/transport";
import { revisionOf } from "@/lib/games/tv";
import { bearerPrefix } from "@/lib/ws/protocol";
import { traceparent } from "@/lib/ws/traceparent";

const url = apiBaseUrl.replace(/^http/, "ws") + "/ws/game";
const retryDelay = (attempt: number) => Math.min(1_000 * 2 ** attempt, 15_000);

export type FeedFrame = { game: Session; previous?: Session; at: number };

const watchCommand = (ids: readonly string[]) =>
  JSON.stringify(toJson(SessionCommandSchema, create(SessionCommandSchema, { action: { case: "watch", value: { sessionIds: [...ids] } }, traceparent: traceparent() })));

export function useFeedSocket(ids: readonly string[]) {
  const [frames, setFrames] = useState<ReadonlyMap<string, FeedFrame>>(() => new Map());
  const [token, setToken] = useState(getAccessToken);
  const [synchronized, setSynchronized] = useState(false);
  const key = ids.join(",");
  const watched = useMemo(() => key ? key.split(",") : [], [key]);

  useEffect(() => {
    if (token) return;
    let alive = true;
    void refreshSession().then(ok => { if (alive && ok) setToken(getAccessToken()); });
    return () => { alive = false; };
  }, [token]);

  const protocols = useMemo(() => token ? ["axon.game.v1", bearerPrefix + token] : undefined, [token]);
  const { readyState, sendMessage } = useWebSocket(token && watched.length ? url + "?feed=1" : null, {
    protocols,
    retryOnError: true,
    reconnectAttempts: 15,
    reconnectInterval: retryDelay,
    shouldReconnect: event => event.code !== 1008 && getAccessToken() === token,
    onClose: () => {
      setSynchronized(false);
      const fresh = getAccessToken();
      if (fresh !== token) setToken(fresh);
    },
    onMessage: event => {
      if (typeof event.data !== "string") return;
      let update;
      try {
        update = fromJsonString(SessionEventSchema, event.data).update;
      } catch {
        return;
      }
      if (update.case !== "session") return;
      const game = update.value;
      setSynchronized(true);
      setFrames(current => {
        const known = current.get(game.id);
        if (known && (revisionOf(known.game) > revisionOf(game) || revisionOf(known.game) === revisionOf(game) && known.game.spectators === game.spectators)) return current;
        const next = new Map(current);
        next.set(game.id, { game, previous: known && revisionOf(known.game) < revisionOf(game) ? known.game : known?.previous, at: Date.now() });
        return next;
      });
    },
    filter: () => false,
  });

  useEffect(() => {
    if (readyState === ReadyState.OPEN) sendMessage(watchCommand(watched), false);
  }, [watched, readyState, sendMessage]);

  return { frames, connected: readyState === ReadyState.OPEN && synchronized };
}
