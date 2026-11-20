import { beforeEach, describe, expect, it, vi } from "vitest";

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
    clear: () => values.clear(),
  };
}

describe("deviceId", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.stubGlobal("localStorage", memoryStorage());
  });

  it("creates one id per browser and keeps it", async () => {
    const { deviceId } = await import("@/lib/connect/device");
    const first = deviceId();
    expect(first).toMatch(/^[0-9a-f-]{36}$/);
    expect(localStorage.getItem("axon.device")).toBe(first);
    vi.resetModules();
    const again = await import("@/lib/connect/device");
    expect(again.deviceId()).toBe(first);
  });

  it("still returns an id when storage is unavailable", async () => {
    vi.stubGlobal("localStorage", { getItem: () => { throw new Error("blocked"); }, setItem: () => undefined });
    const { deviceId } = await import("@/lib/connect/device");
    expect(deviceId()).toMatch(/^[0-9a-f-]{36}$/);
  });
});
