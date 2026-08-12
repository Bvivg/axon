import { createConnectTransport } from "@connectrpc/connect-web";
import type { Interceptor } from "@connectrpc/connect";

export const apiBaseUrl =
  process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:18080";

const withCredentials: typeof fetch = (input, init) =>
  fetch(input, { ...init, credentials: "include" });

export function newTransport(interceptors: Interceptor[] = []) {
  return createConnectTransport({
    baseUrl: apiBaseUrl,
    fetch: withCredentials,
    interceptors,
  });
}

export const bareTransport = newTransport();
