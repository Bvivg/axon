import { Code, ConnectError } from "@connectrpc/connect";
import { describe, expect, it } from "vitest";

import { groupFailures, runBulk } from "./bulk";

describe("runBulk", () => {
  it("never runs more than the concurrency limit at once and reports every item", async () => {
    let running = 0;
    let peak = 0;
    const items = Array.from({ length: 10 }, (_, i) => i);
    const result = await runBulk(items, async (item) => {
      running++;
      peak = Math.max(peak, running);
      await new Promise((resolve) => setTimeout(resolve, 5));
      running--;
      if (item % 3 === 0) throw new Error(item === 0 ? "gone" : "busy");
    }, { concurrency: 4 });
    expect(peak).toBeLessThanOrEqual(4);
    expect(result.done.sort((a, b) => a - b)).toEqual([1, 2, 4, 5, 7, 8]);
    expect(result.failed.map((f) => f.item).sort((a, b) => a - b)).toEqual([0, 3, 6, 9]);
    expect(groupFailures(result.failed, (error) => (error as Error).message)).toEqual([["busy", 3], ["gone", 1]]);
  });
  it("handles an empty selection", async () => {
    expect(await runBulk([], async () => undefined)).toEqual({ done: [], failed: [] });
  });
  it("waits out the rate limit and retries, giving up after the last attempt", async () => {
    const waits: number[] = [];
    const tries = new Map<string, number>();
    const throttled = () => new ConnectError("too many requests", Code.ResourceExhausted, new Headers({ "Retry-After": "2" }));
    const result = await runBulk(["a", "b"], async (item) => {
      const n = (tries.get(item) ?? 0) + 1;
      tries.set(item, n);
      if (item === "a" && n < 2) throw throttled();
      if (item === "b") throw throttled();
    }, { concurrency: 1, attempts: 3, wait: async (seconds) => void waits.push(seconds) });
    expect(result.done).toEqual(["a"]);
    expect(result.failed.map((f) => f.item)).toEqual(["b"]);
    expect(tries.get("b")).toBe(3);
    expect(waits).toEqual([2, 2, 2]);
  });
  it("does not retry other failures", async () => {
    let calls = 0;
    const result = await runBulk([1], async () => {
      calls++;
      throw new ConnectError("gone", Code.NotFound);
    }, { wait: async () => undefined });
    expect(calls).toBe(1);
    expect(result.failed).toHaveLength(1);
  });
});
