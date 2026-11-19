import { describe, expect, it } from "vitest";

import { calendarDate, lastSeen, shortWhen } from "@/lib/format";

describe("dates follow the interface language", () => {
  const now = new Date(2026, 9, 2, 12, 0);

  it("writes an older last seen in English", () => {
    expect(lastSeen(new Date(2026, 8, 28, 14, 45), now)).toBe("last seen 28 Sept at 14:45");
  });

  it("adds the year outside the current one", () => {
    expect(lastSeen(new Date(2025, 0, 5, 9, 5), now)).toBe("last seen 5 Jan 2025 at 09:05");
  });

  it("keeps empty values empty", () => {
    expect(lastSeen(null, now)).toBe("");
    expect(shortWhen(null, now)).toBe("");
  });

  it("writes a calendar date in English", () => {
    expect(calendarDate(new Date(2026, 8, 28))).toBe("28 Sept 2026");
  });
});
