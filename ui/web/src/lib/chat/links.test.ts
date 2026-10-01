import { describe, expect, it } from "vitest";

import { splitLinks } from "@/lib/chat/links";

describe("splitLinks", () => {
  it.each([
    { text: "no links here", want: [{ text: "no links here", href: null }] },
    {
      text: "see https://axon.dev/docs.",
      want: [
        { text: "see ", href: null },
        { text: "https://axon.dev/docs", href: "https://axon.dev/docs" },
        { text: ".", href: null },
      ],
    },
    {
      text: "axon.dev/chat",
      want: [{ text: "axon.dev/chat", href: "https://axon.dev/chat" }],
    },
    {
      text: "mail ada@axon.dev",
      want: [
        { text: "mail ", href: null },
        { text: "ada@axon.dev", href: "mailto:ada@axon.dev" },
      ],
    },
    { text: "javascript:alert(1)", want: [{ text: "javascript:alert(1)", href: null }] },
  ])("$text", ({ text, want }) => {
    expect(splitLinks(text)).toEqual(want);
  });
});
