import { createConnectTransport } from "@connectrpc/connect-web";
import type { Interceptor } from "@connectrpc/connect";

/**
 * The gateway's base URL. Public because the browser is the one calling it —
 * there is no server-side hop to hide it behind.
 */
export const apiBaseUrl =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:18080";

/**
 * withCredentials sends cookies with every call.
 *
 * This is what carries the HttpOnly refresh cookie the gateway sets. Without it
 * the cookie is stored and never sent, and the symptom is that refresh appears
 * broken for no visible reason — the request simply arrives with nothing in it.
 */
const withCredentials: typeof fetch = (input, init) =>
  fetch(input, { ...init, credentials: "include" });

/** newTransport builds a Connect transport aimed at the gateway. */
export function newTransport(interceptors: Interceptor[] = []) {
  return createConnectTransport({
    baseUrl: apiBaseUrl,
    fetch: withCredentials,
    interceptors,
  });
}

/**
 * bareTransport talks to the gateway with no token handling at all.
 *
 * It is for the calls that establish a session rather than use one: Register,
 * Login, RefreshToken, Logout, StartOAuth, CompleteOAuth. Routing a refresh
 * through the retrying transport would mean a failed refresh triggering another
 * refresh.
 */
export const bareTransport = newTransport();
