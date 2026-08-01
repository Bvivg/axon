import { createClient } from "@connectrpc/connect";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { bareTransport } from "@/lib/connect/transport";
import { clearAccessToken, setAccessToken } from "@/lib/auth/tokens";

const client = createClient(AuthService, bareTransport);

/**
 * inFlight is the rotation currently running, shared by everyone waiting on it.
 *
 * This is not an optimisation. Refresh tokens rotate, and presenting a spent one
 * is how the server detects a stolen token — it revokes the entire chain. Ten
 * components reacting to one expiry with ten refreshes would send nine spent
 * tokens and sign the user out of every device they own. One rotation, shared.
 */
let inFlight: Promise<boolean> | null = null;

/**
 * Exchanges the refresh cookie for a new access token.
 *
 * The request body is deliberately empty: the browser has never seen the refresh
 * token, and the gateway fills it in from the cookie. Resolves false when there
 * is no usable session, which is the ordinary answer for a first-time visitor
 * and not an error.
 */
export function refreshSession(): Promise<boolean> {
  inFlight ??= rotate().finally(() => {
    inFlight = null;
  });
  return inFlight;
}

async function rotate(): Promise<boolean> {
  try {
    const resp = await client.refreshToken({});
    setAccessToken(resp.tokens);
    return true;
  } catch {
    // Every failure means the same thing to the caller: there is no session.
    // Which of them it was — no cookie, expired, revoked — is the server's
    // business and is already in its logs.
    clearAccessToken();
    return false;
  }
}
