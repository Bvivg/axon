import { describe, expect, it } from "vitest";

import { ContentKind } from "@/lib/chat/content";
import { slideIndexOf, viewerSlidesOf } from "@/lib/chat/viewer";
import type { WireMessage } from "@/lib/ws/protocol";

const at = new Date(2026, 9, 1).toISOString();
const base = { room_id: "r", author_id: "a", sent_at: at };

describe("viewerSlidesOf", () => {
  it("lays out every photo and video of the transcript in order, albums item by item", () => {
    const messages: WireMessage[] = [
      { ...base, id: "t", seq: 1, body: "hello" },
      { ...base, id: "p", seq: 2, body: "a cat", kind: ContentKind.Image, payload: { url: "p.jpg", width: 4, height: 3 } },
      {
        ...base,
        id: "a",
        seq: 3,
        body: "",
        kind: ContentKind.Album,
        payload: {
          items: [
            { kind: "image", url: "a0.jpg", width: 1, height: 1 },
            { kind: "video", url: "a1.mp4", poster_url: "a1.jpg", width: 16, height: 9 },
          ],
        },
      },
      { ...base, id: "d", seq: 4, body: "", kind: ContentKind.Image, payload: {}, deleted_at: at },
    ];

    const slides = viewerSlidesOf(messages);
    expect(slides.map((slide) => [slide.key, slide.kind, slide.url])).toEqual([
      ["p:0", ContentKind.Image, "p.jpg"],
      ["a:0", ContentKind.Image, "a0.jpg"],
      ["a:1", ContentKind.Video, "a1.mp4"],
    ]);
    expect(slides[0].caption).toBe("a cat");
    expect(slides[2].preview).toBe("a1.jpg");
    expect(slideIndexOf(slides, "a", 1)).toBe(2);
    expect(slideIndexOf(slides, "t", 0)).toBe(-1);
  });
});
