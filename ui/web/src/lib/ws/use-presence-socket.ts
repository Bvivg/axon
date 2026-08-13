"use client";

import { useEffect, useState } from "react";
import useWebSocket from "react-use-websocket";

import { refreshSession } from "@/lib/auth/refresh";
import { getAccessToken } from "@/lib/auth/tokens";
import { apiBaseUrl } from "@/lib/connect/transport";
import { bearerPrefix, closeCodes } from "@/lib/ws/protocol";

const socketUrl = apiBaseUrl.replace(/^http/, "ws") + "/ws/presence";

const presenceSubprotocol = "axon.presence.v1";

const maxRetries = 30;
const reconnectDelay = (attempt: number) => Math.min(1000 * 2 ** attempt, 15_000);

export function usePresenceSocket(): void {
  const [token, setToken] = useState<string | null>(null);

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

  useWebSocket(token ? socketUrl : null, {
    protocols: token ? [presenceSubprotocol, bearerPrefix + token] : undefined,
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
  });
}
