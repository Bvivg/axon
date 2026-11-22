import { describe, expect, it } from "vitest";

import { readRpcMetrics } from "./telemetry";

const login = "/axon.auth.v1.AuthService/Login";
const getMe = "/axon.auth.v1.AuthService/GetMe";

describe("Gateway metrics", () => {
  it("keeps explicit zeros and totals only published Gateway procedures", () => {
    const metrics = [
      `rpc_requests_total{code="ok",procedure="${login}",service="gateway"} 3`,
      `rpc_requests_total{code="unauthenticated",procedure="${login}",service="gateway"} 2`,
      `rpc_requests_total{code="permission_denied",procedure="${login}",service="gateway"} 0`,
      `rpc_in_flight{procedure="${login}",service="gateway"} 0`,
      `rpc_in_flight{service="gateway",procedure="${getMe}"} 1`,
      `rpc_requests_total{code="ok",procedure="${getMe}",service="auth"} 90`,
      'rpc_requests_total{code="ok",procedure="/internal",service="gateway"} 80',
    ].join("\n");

    expect(readRpcMetrics(metrics)).toEqual({
      [login]: { total: 5, inFlight: 0, codes: { ok: 3, unauthenticated: 2, permission_denied: 0 } },
      [getMe]: { inFlight: 1, codes: {} },
    });
  });

  it("keeps an empty source distinct from a zero and ignores invalid samples", () => {
    expect(readRpcMetrics(`rpc_in_flight{procedure="${login}",service="gateway"} NaN`)).toEqual({});
    expect(readRpcMetrics(`rpc_requests_total{code="ok",procedure="${login}",service="gateway"} 0`)[login].total).toBe(0);
  });
});
