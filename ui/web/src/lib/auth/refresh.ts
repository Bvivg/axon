import { createClient } from "@connectrpc/connect";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { bareTransport } from "@/lib/connect/transport";
import { clearAccessToken, setAccessToken } from "@/lib/auth/tokens";

const client = createClient(AuthService, bareTransport);

let inFlight: Promise<boolean> | null = null;

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

    clearAccessToken();
    return false;
  }
}
