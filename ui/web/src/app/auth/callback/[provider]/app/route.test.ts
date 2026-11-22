import { describe, expect, it } from "vitest";

import { GET } from "./route";

const call = (provider: string, query: string) =>
  GET(new Request(`http://localhost:3003/auth/callback/${provider}/app?${query}`), { params: Promise.resolve({ provider }) });

describe("app OAuth callback", () => {
  it("hands the code and state to the app", async () => {
    const response = await call("github", "code=abc&state=xyz&extra=dropped");
    expect(response.status).toBe(302);
    const location = new URL(response.headers.get("Location")!);
    expect(location.protocol).toBe("axon:");
    expect(location.host + location.pathname).toBe("oauth/github");
    expect(Object.fromEntries(location.searchParams)).toEqual({ code: "abc", state: "xyz" });
  });

  it("passes a provider error through", async () => {
    const response = await call("github", "error=access_denied&state=xyz");
    const location = new URL(response.headers.get("Location")!);
    expect(location.searchParams.get("error")).toBe("access_denied");
    expect(location.searchParams.get("state")).toBe("xyz");
  });

  it("reports a callback without a code", async () => {
    const response = await call("github", "state=xyz");
    expect(new URL(response.headers.get("Location")!).searchParams.get("error")).toBe("invalid_request");
  });

  it("refuses providers the app cannot use", async () => {
    expect((await call("apple", "code=abc")).status).toBe(404);
    expect((await call("evil", "code=abc")).status).toBe(404);
  });
});
