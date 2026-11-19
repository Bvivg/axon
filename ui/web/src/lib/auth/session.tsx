"use client";

import { useQueryClient } from "@tanstack/react-query";
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
import { cancelSessionRefresh, refreshSession } from "@/lib/auth/refresh";
import { disablePush } from "@/lib/push";
import { clearAccessToken, setAccessToken } from "@/lib/auth/tokens";
import { beforeSignOut } from "@/lib/auth/before-sign-out";

export type SessionStatus = "restoring" | "authenticated" | "anonymous";

interface Session {
  status: SessionStatus;
  user: User | null;
  beginSignIn: () => void;

  signedIn: (user: User, tokens: TokenPair | undefined) => void;

  updateUser: (user: User) => void;

  signOut: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<SessionStatus>("restoring");
  const [user, setUser] = useState<User | null>(null);

  const settled = useRef(false);

  const beginSignIn = useCallback(() => {
    settled.current = true;
    cancelSessionRefresh();
    clearAccessToken();
    queryClient.clear();
    setUser(null);
    setStatus("anonymous");
  }, [queryClient]);

  const signedIn = useCallback((next: User, tokens: TokenPair | undefined) => {
    settled.current = true;
    cancelSessionRefresh();
    queryClient.clear();
    setAccessToken(tokens);
    setUser(next);
    setStatus("authenticated");
  }, [queryClient]);

  const updateUser = useCallback((next: User) => {
    setUser((current) => current?.id === next.id ? next : current);
  }, []);

  const signOut = useCallback(async () => {
    settled.current = true;
    await beforeSignOut();
    cancelSessionRefresh();

    await disablePush().catch(() => undefined);

    try {

      await authClient.logout({});
    } catch {

    }

    cancelSessionRefresh();
    clearAccessToken();
    queryClient.clear();
    setUser(null);
    setStatus("anonymous");
  }, [queryClient]);

  useEffect(() => {
    if (settled.current) return;
    let cancelled = false;

    const stale = () => cancelled || settled.current;

    void (async () => {
      if (!(await refreshSession())) {
        if (!stale()) {
          setStatus("anonymous");
        }
        return;
      }

      if (stale()) return;
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
    () => ({ status, user, beginSignIn, signedIn, updateUser, signOut }),
    [status, user, beginSignIn, signedIn, updateUser, signOut],
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
