import type { TokenPair } from "@/gen/axon/auth/v1/auth_pb";

let accessToken: string | null = null;

let expiresAt = 0;

const expiryMargin = 30_000;

export function setAccessToken(tokens: TokenPair | undefined): void {
  if (!tokens?.accessToken) {
    clearAccessToken();
    return;
  }

  accessToken = tokens.accessToken;
  expiresAt = Date.now() + Number(tokens.expiresIn) * 1000;
}

export function getAccessToken(): string | null {
  if (!accessToken || Date.now() >= expiresAt - expiryMargin) {
    return null;
  }
  return accessToken;
}

export function clearAccessToken(): void {
  accessToken = null;
  expiresAt = 0;
}
