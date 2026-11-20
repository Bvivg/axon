import { describe, expect, it, vi } from "vitest";

import { mediaURL } from "./media-url";

const key = "2a5672f3-4d8e-49ca-80a4-243deabc1234/3542a35f-eccf-4b17-8ddc-123456789abc.thumb.jpg";

describe("private attachment URLs", () => {
  it("routes persisted storage URLs through the current gateway without credentials in the URL", () => {
    vi.stubEnv("NEXT_PUBLIC_API_BASE_URL", "/api");
    expect(mediaURL(`http://localhost:59000/chat-attachments/${key}`)).toBe(`/api/media/${key}`);
    vi.unstubAllEnvs();
  });

  it("preserves local upload previews and rejects executable URLs", () => {
    expect(mediaURL("blob:https://axon.test/preview")).toBe("blob:https://axon.test/preview");
    expect(mediaURL("javascript:alert(1)")).toBe("");
    expect(mediaURL("data:text/html,hello")).toBe("");
  });
});
