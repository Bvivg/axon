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

/**
 * "restoring" is a distinct state on purpose. Treating an unknown session as
 * anonymous makes every reload flash the sign-in page before the refresh
 * finishes, which reads as being signed out.
 */
export type SessionStatus = "restoring" | "authenticated" | "anonymous";

interface Session {
  status: SessionStatus;
  user: User | null;

  /** Records the result of a sign-in that has already happened. */
  signedIn: (user: User, tokens: TokenPair | undefined) => void;

  /** Revokes the refresh chain and forgets the access token. */
  signOut: () => Promise<void>;
}

const SessionContext = createContext<Session | null>(null);

export function SessionProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<SessionStatus>("restoring");
  const [user, setUser] = useState<User | null>(null);

  /**
   * Set once something authoritative has happened — a sign-in or a sign-out.
   *
   * The restore below races with them, and on one page it always loses: the
   * OAuth callback loads with no cookie yet, so the restore is already asking
   * "is there a session?" while CompleteOAuth is busy creating one. Without this
   * the restore's "no" can land after the sign-in's "yes" and bounce the user
   * straight back to the sign-in page, intermittently.
   */
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
      // The body is empty: the refresh token is in the cookie, which the
      // gateway reads and then expires. An empty body is the whole request.
      await authClient.logout({});
    } catch {
      // Signing out has to work even when the call does not. The local state is
      // cleared regardless, and the token it revokes expires on its own.
    }

    clearAccessToken();
    setUser(null);
    setStatus("anonymous");
  }, []);

  /**
   * Restores the session on first load.
   *
   * The access token died with the previous page, so the only evidence a
   * session exists is the refresh cookie. This is the single reason a reload
   * does not sign the user out, and the reason nothing has to be stored in
   * JavaScript to achieve that.
   */
  useEffect(() => {
    let cancelled = false;

    // Nothing the restore learns may overwrite a sign-in or sign-out that
    // happened while it was in flight.
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
