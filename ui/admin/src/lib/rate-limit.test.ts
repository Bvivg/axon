import { afterEach, describe, expect, it, vi } from "vitest";

import { fetchWithRateLimitRetry, retryAfterMs, waitForRetry } from "./rate-limit";

afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

describe("rate limit retries", () => {
  it("reads seconds and HTTP dates, with a fallback for missing or invalid headers", () => {
    expect(retryAfterMs("6")).toBe(6_000);
    expect(retryAfterMs("Tue, 03 Nov 2026 00:00:06 GMT", Date.parse("2026-11-03T00:00:00Z"))).toBe(6_000);
    for (const value of [null, undefined, "", "invalid", "-1"]) expect(retryAfterMs(value)).toBe(1_000);
  });

  it("retries 429 twice, keeps the body and returns the final response", async () => {
    vi.useFakeTimers();
    const fetch = vi.fn(async () => new Response("limited", { status: 429, headers: { "Retry-After": "2" } }));
    vi.stubGlobal("fetch", fetch);
    const result = fetchWithRateLimitRetry("http://localhost/action", { method: "POST", body: "payload" });
    await vi.advanceTimersByTimeAsync(1_999);
    expect(fetch).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(2_001);
    const response = await result;
    expect(fetch).toHaveBeenCalledTimes(3);
    expect(await response.text()).toBe("limited");
    for (const [request] of fetch.mock.calls as unknown as [Request][]) expect(await request.text()).toBe("payload");
  });

  it("stops waiting immediately on cancellation and does not retry", async () => {
    vi.useFakeTimers();
    const fetch = vi.fn(async () => new Response(null, { status: 429, headers: { "Retry-After": "6" } }));
    vi.stubGlobal("fetch", fetch);
    const controller = new AbortController();
    const result = expect(fetchWithRateLimitRetry("http://localhost/action", { signal: controller.signal })).rejects.toMatchObject({ name: "AbortError" });
    await vi.advanceTimersByTimeAsync(100);
    controller.abort();
    await result;
    await vi.advanceTimersByTimeAsync(10_000);
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("does not retry successful requests or unrelated failures", async () => {
    const fetch = vi.fn(async () => new Response(null, { status: 500 }));
    vi.stubGlobal("fetch", fetch);
    expect((await fetchWithRateLimitRetry("http://localhost/action")).status).toBe(500);
    expect(fetch).toHaveBeenCalledTimes(1);
    const controller = new AbortController();
    controller.abort();
    await expect(waitForRetry(1_000, controller.signal)).rejects.toMatchObject({ name: "AbortError" });
  });
});
