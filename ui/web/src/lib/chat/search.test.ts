import { describe, expect, it } from "vitest";

import { snippetOf } from "@/lib/chat/search";

const marked = (body: string, query: string) =>
  snippetOf(body, query)
    .map((part) => (part.match ? `[${part.text}]` : part.text))
    .join("");

describe("snippetOf", () => {
  it.each([
    ["marks every match ignoring case", "Release notes: the release is out", "release", "[Release] notes: the [release] is out"],
    ["takes regex characters literally", "costs 5.00 (or 5x00)", "5.00", "costs [5.00] (or 5x00)"],
    ["matches Cyrillic case-insensitively", "Привет, МИР и мир", "мир", "Привет, [МИР] и [мир]"],
    ["collapses line breaks", "first line\n\nsecond   line", "second", "first line [second] line"],
    [
      "starts a long body near the match on a word boundary",
      "one two three four five six seven eight nine ten eleven twelve needle here",
      "needle",
      "…nine ten eleven twelve [needle] here",
    ],
    ["keeps the body when nothing matches", "nothing to see", "absent", "nothing to see"],
  ])("%s", (_, body, query, want) => {
    expect(marked(body, query)).toBe(want);
  });
});
