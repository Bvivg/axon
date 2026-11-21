import { createConnectTransport } from "@connectrpc/connect-web";
import type { Interceptor } from "@connectrpc/connect";

import { retryRateLimit } from "@/lib/connect/rate-limit";

const configuredBaseUrl = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:18080";
export const apiBaseUrl = configuredBaseUrl.startsWith("/") && typeof window !== "undefined" ? new URL(configuredBaseUrl, window.location.origin).href : configuredBaseUrl;

const withCredentials: typeof fetch = (input, init) =>
  fetch(input, { ...init, credentials: "include" });

export function newTransport(interceptors: Interceptor[] = []) {
  return createConnectTransport({
    baseUrl: apiBaseUrl,
    fetch: withCredentials,
    interceptors: [...interceptors, retryRateLimit],
  });
}

export const bareTransport = newTransport();
