"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

import type { TokenPair, User } from "@/gen/axon/auth/v1/auth_pb";
import { authClient, apiClient } from "@/lib/connect/clients";
import { refreshSession } from "@/lib/auth/refresh";
import { clearAccessToken, setAccessToken } from "@/lib/auth/tokens";

export type SessionStatus = "restoring" | "authenticated" | "anonymous";

interface Session {
  status: SessionStatus;
  user: User | null;

  signedIn: (user: User, tokens: TokenPair | undefined) => void;

  signOut: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SessionStatus>("restoring");
  const [user, setUser] = useState<User | null>(null);

  const settled = useRef(false);

  const signedIn = useCallback((next: User, tokens: TokenPair | undefined) => {
    settled.current = true;
    setAccessToken(tokens);
    setUser(next);
    setStatus("authenticated");
  }, []);

  const signOut = useCallback(async () => {
    settled.current = true;

    try {

      await authClient.logout({});
    } catch {

    }

    clearAccessToken();
    setUser(null);
    setStatus("anonymous");
  }, []);

  useEffect(() => {
    let cancelled = false;

    const stale = () => cancelled || settled.current;

    void (async () => {
      if (!(await refreshSession())) {
        if (!stale()) {
          setStatus("anonymous");
        }
        return;
      }

      try {
        const resp = await apiClient.getMe({});
        if (!stale()) {
          setUser(resp.user ?? null);
          setStatus(resp.user ? "authenticated" : "anonymous");
        }
      } catch {
        if (!stale()) {
          clearAccessToken();
          setStatus("anonymous");
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  const value = useMemo<Session>(
    () => ({ status, user, signedIn, signOut }),
    [status, user, signedIn, signOut],
  );

  return <SessionContext value={value}>{children}</SessionContext>;
}

export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) {
    throw new Error("useSession must be used inside a SessionProvider");
  }
  return session;
}
