import { describe, expect, it } from "vitest";

import { traceparent } from "@/lib/ws/traceparent";

describe("traceparent", () => {
  it("builds a sampled W3C trace context with fresh ids", () => {
    const first = traceparent();
    expect(first).toMatch(/^00-[0-9a-f]{32}-[0-9a-f]{16}-01$/);
    expect(traceparent()).not.toBe(first);
  });
});
