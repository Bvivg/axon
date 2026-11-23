import { describe, expect, it } from "vitest";

import { isActive } from "@/lib/navigation";

describe("isActive", () => {
  it("marks only the most specific section", () => {
    expect(isActive("/games/tv", "/games/tv")).toBe(true);
    expect(isActive("/games", "/games/tv")).toBe(false);
    expect(isActive("/games", "/games/8d1c")).toBe(true);
    expect(isActive("/games", "/games")).toBe(true);
    expect(isActive("/", "/games")).toBe(false);
  });
});
