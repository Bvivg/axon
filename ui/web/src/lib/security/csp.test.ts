import { describe, expect, it } from "vitest";

import { contentSecurityPolicy, newNonce } from "./csp";

const directives = (policy: string) =>
  new Map(policy.split(";").map((part) => part.trim()).filter(Boolean).map((part) => {
    const [name, ...sources] = part.split(/\s+/);
    return [name, sources] as const;
  }));

describe("contentSecurityPolicy", () => {
  const policy = directives(contentSecurityPolicy("abc123", { gateway: "http://localhost:18080", assets: "http://localhost:59000", development: false }));

  it("only runs scripts that carry the request's nonce", () => {
    expect(policy.get("script-src")).toEqual(["'self'", "'nonce-abc123'", "'strict-dynamic'"]);
    expect(policy.get("default-src")).toEqual(["'self'"]);
    expect(policy.get("object-src")).toEqual(["'none'"]);
    expect(policy.get("base-uri")).toEqual(["'self'"]);
    expect(policy.get("frame-ancestors")).toEqual(["'none'"]);
    expect(policy.get("form-action")).toEqual(["'self'"]);
  });

  it("lets only embeddable pages be framed, and only by the same origin", () => {
    const embedded = directives(contentSecurityPolicy("n", { gateway: "/api", development: false, embeddable: true }));
    expect(embedded.get("frame-ancestors")).toEqual(["'self'"]);
    expect(embedded.get("frame-src")).toEqual(["'none'"]);
  });

  it("reaches the gateway over HTTP and WebSocket and nothing else", () => {
    expect(policy.get("connect-src")).toEqual(["'self'", "http://localhost:18080", "ws://localhost:18080"]);
  });

  it("loads images and media from the gateway and the avatar store", () => {
    expect(policy.get("img-src")).toEqual(["'self'", "data:", "blob:", "http://localhost:18080", "http://localhost:59000"]);
    expect(policy.get("media-src")).toEqual(["'self'", "data:", "blob:", "http://localhost:18080"]);
  });

  it("keeps eval out of production and allows it for the dev server only", () => {
    expect(policy.get("script-src")).not.toContain("'unsafe-eval'");
    const dev = directives(contentSecurityPolicy("n", { gateway: "/api", development: true }));
    expect(dev.get("script-src")).toContain("'unsafe-eval'");
  });

  it("treats a same-origin gateway as self and uses wss behind https", () => {
    const sameOrigin = directives(contentSecurityPolicy("n", { gateway: "/api", development: false }));
    expect(sameOrigin.get("connect-src")).toEqual(["'self'"]);
    const tls = directives(contentSecurityPolicy("n", { gateway: "https://api.axon.example/rpc", development: false }));
    expect(tls.get("connect-src")).toEqual(["'self'", "https://api.axon.example", "wss://api.axon.example"]);
  });

  it("ignores a malformed origin instead of widening the policy", () => {
    const odd = directives(contentSecurityPolicy("n", { gateway: "http://localhost:18080", assets: "javascript:alert(1); script-src *", development: false }));
    expect(odd.get("img-src")).toEqual(["'self'", "data:", "blob:", "http://localhost:18080"]);
    expect(odd.get("script-src")).toEqual(["'self'", "'nonce-n'", "'strict-dynamic'"]);
  });
});

describe("newNonce", () => {
  it("is unpredictable and fits the nonce grammar", () => {
    const a = newNonce();
    expect(a).toMatch(/^[A-Za-z0-9+/]+={0,2}$/);
    expect(a.length).toBeGreaterThanOrEqual(22);
    expect(newNonce()).not.toBe(a);
  });
});
