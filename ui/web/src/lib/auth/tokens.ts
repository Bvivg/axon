import type { TokenPair } from "@/gen/axon/auth/v1/auth_pb";

/**
 * The access token lives here and nowhere else.
 *
 * Not localStorage and not sessionStorage: both are readable by any script that
 * gets onto the page, and a token sitting in one survives the tab being closed
 * for no benefit. In memory it dies with the tab, and the session is restored on
 * the next load by refreshing — see refreshSession.
 *
 * The refresh token is not here at all. The gateway keeps it in an HttpOnly
 * cookie, so this file could not read it even if it wanted to. That is the point
 * of the arrangement: the long-lived credential is out of JavaScript's reach.
 *
 * A module-level variable is per-tab because every module that touches it is a
 * client component. Nothing here may be imported into a server component, where
 * one variable would be shared by every request the server handles.
 */
let accessToken: string | null = null;

/** Wall-clock milliseconds at which accessToken stops being usable. */
let expiresAt = 0;

/**
 * expiryMargin is how early a token is treated as expired.
 *
 * Refreshing on the exact second means a request that spends 200ms in flight
 * can arrive after expiry and come back 401 — which works, because the
 * interceptor retries, but pays a round trip to learn something the clock
 * already knew.
 */
const expiryMargin = 30_000;

/** Records a freshly issued pair. */
export function setAccessToken(tokens: TokenPair | undefined): void {
  if (!tokens?.accessToken) {
    clearAccessToken();
    return;
  }

  accessToken = tokens.accessToken;
  expiresAt = Date.now() + Number(tokens.expiresIn) * 1000;
}

/** Returns the access token, or null if there is none or it has aged out. */
export function getAccessToken(): string | null {
  if (!accessToken || Date.now() >= expiresAt - expiryMargin) {
    return null;
  }
  return accessToken;
}

/** Forgets the token. The cookie is the server's to clear, via Logout. */
export function clearAccessToken(): void {
  accessToken = null;
  expiresAt = 0;
}
