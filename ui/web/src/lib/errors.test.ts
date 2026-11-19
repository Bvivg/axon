import { Code, ConnectError } from "@connectrpc/connect";
import { describe, it, expect } from "vitest";

import { describe as describeError, retryAfterMessage } from "./errors";

describe("rate limit feedback", () => {
  it("uses Retry-After from the server", () => {
    const error = new ConnectError("too many requests", Code.ResourceExhausted, { "Retry-After": "6" });
    expect(describeError(error)).toBe("Too many attempts. Try again in 6 seconds.");
  });
  it("handles one second and missing or malformed metadata", () => {
    expect(retryAfterMessage(new ConnectError("", Code.ResourceExhausted, { "Retry-After": "1" }))).toBe("Try again in 1 second.");
    for (const value of ["", "garbage", "0", "-1"]) {
      expect(retryAfterMessage(new ConnectError("", Code.ResourceExhausted, { "Retry-After": value }))).toBe("Try again shortly.");
    }
  });
});
