import { Code, ConnectError } from "@connectrpc/connect";
import { afterEach, expect, it, vi } from "vitest";

import { CallChannel } from "./channel";

const { registerEndpoint } = vi.hoisted(() => ({ registerEndpoint: vi.fn() }));
vi.mock("@/lib/connect/clients", () => ({ callsClient: { registerEndpoint } }));

afterEach(() => { vi.unstubAllGlobals(); vi.clearAllMocks(); });

it("waits for stale endpoint recovery when the socket opens during sign-in", async () => {
  const values = new Map([["axon.calls.endpoint.user", JSON.stringify({ id: "old", proof: "old-proof", epoch: "1" })]]);
  vi.stubGlobal("sessionStorage", {
    getItem: (key: string) => values.get(key),
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.stubGlobal("navigator", { locks: { request: (_: string, __: unknown, callback: (lock: object) => Promise<void>) => callback({}) } });
  const endpoint = { $typeName: "axon.calls.v1.EndpointRef", id: "new", proof: "new-proof", epoch: 2n };
  registerEndpoint.mockRejectedValueOnce(new ConnectError("revoked family", Code.PermissionDenied)).mockResolvedValueOnce({ endpoint });
  const channel = new CallChannel();
  const listener = vi.fn();
  const sender = vi.fn();
  channel.subscribe(listener);
  channel.bind(sender, vi.fn());
  try {
    const initialization = channel.initialize("user");
    const opening = channel.open();
    await Promise.all([initialization, opening]);
    expect(channel.endpoint).toEqual(endpoint);
    expect(registerEndpoint).toHaveBeenCalledTimes(2);
    expect(registerEndpoint.mock.calls[0][0].resume.id).toBe("old");
    expect(registerEndpoint.mock.calls[1][0].resume).toBeUndefined();
    expect(listener).not.toHaveBeenCalled();
    expect(sender).toHaveBeenCalledWith(expect.objectContaining({ type: "call_attach" }));
    expect(JSON.parse(values.get("axon.calls.endpoint.user")!).id).toBe("new");
  } finally {
    channel.dispose();
  }
});

function stubBrowser() {
  const values = new Map<string, string>();
  vi.stubGlobal("sessionStorage", {
    getItem: (key: string) => values.get(key),
    setItem: (key: string, value: string) => values.set(key, value),
    removeItem: (key: string) => values.delete(key),
  });
  vi.stubGlobal("navigator", { locks: { request: (_: string, __: unknown, callback: (lock: object) => Promise<void>) => callback({}) } });
}

it("attaches without registering again when the socket opens after the endpoint is ready", async () => {
  stubBrowser();
  const endpoint = { $typeName: "axon.calls.v1.EndpointRef", id: "fresh", proof: "proof", epoch: 1n };
  registerEndpoint.mockResolvedValue({ endpoint });
  const channel = new CallChannel();
  const sender = vi.fn();
  channel.bind(sender, vi.fn());
  try {
    await channel.initialize("user");
    expect(sender).not.toHaveBeenCalled();
    await channel.open();
    expect(registerEndpoint).toHaveBeenCalledTimes(1);
    expect(sender).toHaveBeenCalledTimes(1);
    expect(sender).toHaveBeenCalledWith(expect.objectContaining({ type: "call_attach" }));
  } finally {
    channel.dispose();
  }
});

it("resumes the endpoint when the socket reconnects", async () => {
  stubBrowser();
  const first = { $typeName: "axon.calls.v1.EndpointRef", id: "fresh", proof: "proof", epoch: 1n };
  const resumed = { ...first, epoch: 2n };
  registerEndpoint.mockResolvedValueOnce({ endpoint: first }).mockResolvedValueOnce({ endpoint: resumed });
  const channel = new CallChannel();
  const sender = vi.fn();
  channel.bind(sender, vi.fn());
  try {
    await channel.initialize("user");
    await channel.open();
    channel.closed();
    await channel.open();
    expect(registerEndpoint).toHaveBeenCalledTimes(2);
    expect(registerEndpoint.mock.calls[1][0].resume.epoch).toBe(1n);
    expect(channel.endpoint).toEqual(resumed);
    expect(sender.mock.calls.filter(([frame]) => frame.type === "call_attach")).toHaveLength(2);
  } finally {
    channel.dispose();
  }
});
