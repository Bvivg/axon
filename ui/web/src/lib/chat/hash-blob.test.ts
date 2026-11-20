import { createHash } from "node:crypto";
import { describe, expect, it } from "vitest";

import { hashBlob } from "@/lib/chat/hash-blob";

describe("file checksum", () => {
  for (const size of [0, 3, 2 * 1024 * 1024 + 17]) {
    it(`hashes ${size} bytes identically to the server without loading the whole file`, async () => {
      const bytes = new Uint8Array(size).map((_, index) => index % 251);
      const progress: number[] = [];
      const result = await hashBlob(new Blob([bytes]), (value) => progress.push(value));
      expect(result).toBe(createHash("sha256").update(bytes).digest("hex"));
      if (size > 0) expect(progress.at(-1)).toBe(1);
      if (size > 2 * 1024 * 1024) expect(progress[0]).toBeLessThan(1);
    });
  }
});
