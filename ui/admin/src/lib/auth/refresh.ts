import { createClient } from "@connectrpc/connect";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { bareTransport } from "@/lib/connect/transport";
import { clearAccessToken, setAccessToken, tokenRevision } from "@/lib/auth/tokens";

const client = createClient(AuthService, bareTransport);

let inFlight: { promise: Promise<boolean>; controller: AbortController } | null = null;

export function refreshSession(): Promise<boolean> {
  if (inFlight) return inFlight.promise;
  const controller = new AbortController();
  const promise = rotate(controller.signal, tokenRevision()).finally(() => {
    if (inFlight?.promise === promise) inFlight = null;
  });
  inFlight = { promise, controller };
  return promise;
}

export function cancelSessionRefresh(): void {
  inFlight?.controller.abort();
  inFlight = null;
}

async function rotate(signal: AbortSignal, revision: number): Promise<boolean> {
  try {
    const resp = await client.refreshToken({}, { signal });
    if (signal.aborted || tokenRevision() !== revision) return false;
    setAccessToken(resp.tokens);
    return true;
  } catch {

    if (!signal.aborted && tokenRevision() === revision) clearAccessToken();
    return false;
  }
}
