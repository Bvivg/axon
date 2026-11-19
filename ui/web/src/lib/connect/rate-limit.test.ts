import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { afterEach, expect, it, vi } from "vitest";

import { AuthService } from "@/gen/axon/auth/v1/auth_pb";
import { retryRateLimit } from "./rate-limit";

afterEach(() => vi.useRealTimers());

function client(fetch: typeof globalThis.fetch) {
  return createClient(AuthService, createConnectTransport({ baseUrl: "http://localhost", fetch, interceptors: [retryRateLimit] }));
}

const limited = () => new Response(JSON.stringify({ code: "resource_exhausted", message: "too many requests" }), {
  status: 429, headers: { "Content-Type": "application/json", "Retry-After": "2" },
});

it("keeps the RPC pending through 429 and retries the same request", async () => {
  vi.useFakeTimers();
  const bodies: string[] = [];
  const fetch = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
    bodies.push(String(init?.body));
    return bodies.length < 3 ? limited() : new Response("{}", { headers: { "Content-Type": "application/json" } });
  });
  const promise = client(fetch).login({ email: "user@example.test", password: "example" });
  await vi.advanceTimersByTimeAsync(1_999);
  expect(fetch).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(2_001);
  await promise;
  expect(fetch).toHaveBeenCalledTimes(3);
  expect(new Set(bodies).size).toBe(1);
});

it("surfaces the final 429 after two retries", async () => {
  vi.useFakeTimers();
  const fetch = vi.fn(async () => limited());
  const result = expect(client(fetch).login({})).rejects.toMatchObject({ code: 8 });
  await vi.advanceTimersByTimeAsync(4_000);
  await result;
  expect(fetch).toHaveBeenCalledTimes(3);
});

it("cancels a pending retry when a search becomes obsolete", async () => {
  vi.useFakeTimers();
  const fetch = vi.fn(async () => limited());
  const controller = new AbortController();
  const result = expect(client(fetch).searchUsers({ query: "old" }, { signal: controller.signal })).rejects.toBeDefined();
  await vi.advanceTimersByTimeAsync(100);
  controller.abort();
  await result;
  await vi.advanceTimersByTimeAsync(6_000);
  expect(fetch).toHaveBeenCalledTimes(1);
});
