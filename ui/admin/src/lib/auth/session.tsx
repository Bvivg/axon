"use client";

import { useQueryClient } from "@tanstack/react-query";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";

import type { User } from "@/gen/axon/auth/v1/auth_pb";
import { cancelSessionRefresh, refreshSession } from "@/lib/auth/refresh";
import { clearAccessToken } from "@/lib/auth/tokens";
import { apiClient, authClient } from "@/lib/connect/clients";

export enum SessionStatus {
  Restoring = "restoring",
  Authenticated = "authenticated",
  Anonymous = "anonymous",
}

interface Session {
  status: SessionStatus;
  user: User | null;
  signOut: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState(SessionStatus.Restoring);
  const [user, setUser] = useState<User | null>(null);

  const signOut = useCallback(async () => {
    cancelSessionRefresh();
    await authClient.logout({}).catch(() => undefined);
    clearAccessToken();
    queryClient.clear();
    setUser(null);
    setStatus(SessionStatus.Anonymous);
  }, [queryClient]);

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      if (!(await refreshSession())) {
        if (!cancelled) setStatus(SessionStatus.Anonymous);
        return;
      }
      try {
        const { user: me } = await apiClient.getMe({});
        if (cancelled) return;
        setUser(me ?? null);
        setStatus(me ? SessionStatus.Authenticated : SessionStatus.Anonymous);
      } catch {
        if (cancelled) return;
        clearAccessToken();
        setStatus(SessionStatus.Anonymous);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  const value = useMemo<Session>(() => ({ status, user, signOut }), [status, user, signOut]);
  return <SessionContext value={value}>{children}</SessionContext>;
}

export function useSession(): Session {
  const session = useContext(SessionContext);
  if (!session) throw new Error("useSession must be used inside a SessionProvider");
  return session;
}
