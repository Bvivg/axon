import { describe, expect, it } from "vitest";

import { maxMessageLength, messageTooLong } from "./message-limits";

describe("messageTooLong", () => {
  it("accepts 64 KiB of ASCII and rejects one more character", () => {
    expect(messageTooLong("a".repeat(maxMessageLength))).toBe(false);
    expect(messageTooLong("a".repeat(maxMessageLength + 1))).toBe(true);
  });

  it("counts Unicode code points as the backend does", () => {
    expect(messageTooLong("😀".repeat(maxMessageLength))).toBe(false);
    expect(messageTooLong("я".repeat(maxMessageLength + 1))).toBe(true);
  });

  it("ignores surrounding whitespace", () => {
    expect(messageTooLong("  " + "a".repeat(maxMessageLength) + "\n")).toBe(false);
  });
});
