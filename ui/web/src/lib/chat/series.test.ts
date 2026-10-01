import { describe, expect, it } from "vitest";

import { ContentKind } from "@/lib/chat/content";
import { firstUnreadOf, groupTranscript, sameItem, seriesMaxMessages } from "@/lib/chat/series";
import type { WireMessage } from "@/lib/ws/protocol";

const base = new Date(2026, 9, 1, 12, 0, 0).getTime();

function message(seq: number, author: string, minutes: number, kind?: string): WireMessage {
  return {
    id: `m${seq}`,
    room_id: "r",
    author_id: author,
    body: `#${seq}`,
    seq,
    sent_at: new Date(base + minutes * 60_000).toISOString(),
    kind,
  };
}

const shape = (messages: WireMessage[], unread: number | null = null) =>
  groupTranscript(messages, unread).map((item) => ({
    seqs: item.messages.map((m) => m.seq),
    newDay: item.newDay,
    unreadStart: item.unreadStart,
  }));

describe("groupTranscript", () => {
  it.each([
    {
      name: "same author within five minutes is one series",
      messages: [message(1, "a", 0), message(2, "a", 4)],
      want: [[1, 2]],
    },
    {
      name: "a five-minute gap starts a new series",
      messages: [message(1, "a", 0), message(2, "a", 5)],
      want: [[1], [2]],
    },
    {
      name: "another author starts a new series",
      messages: [message(1, "a", 0), message(2, "b", 1), message(3, "a", 2)],
      want: [[1], [2], [3]],
    },
    {
      name: "a system line stands alone",
      messages: [message(1, "a", 0), message(2, "a", 1, ContentKind.System), message(3, "a", 2)],
      want: [[1], [2], [3]],
    },
  ])("$name", ({ messages, want }) => {
    expect(shape(messages).map((item) => item.seqs)).toEqual(want);
  });

  it("a new day opens a new item with a day marker", () => {
    const late = message(1, "a", 11 * 60 + 58);
    const next = message(2, "a", 12 * 60 + 1);
    expect(shape([late, next])).toEqual([
      { seqs: [1], newDay: true, unreadStart: false },
      { seqs: [2], newDay: true, unreadStart: false },
    ]);
  });

  it("the first unread message opens its own item", () => {
    const messages = [message(1, "b", 0), message(2, "b", 1), message(3, "b", 2)];
    expect(shape(messages, 2)).toEqual([
      { seqs: [1], newDay: true, unreadStart: false },
      { seqs: [2, 3], newDay: false, unreadStart: true },
    ]);
  });

  it("caps a series so one row never holds the whole history", () => {
    const messages = Array.from({ length: seriesMaxMessages + 1 }, (_, index) => message(index + 1, "a", 0));
    expect(shape(messages).map((item) => item.seqs.length)).toEqual([seriesMaxMessages, 1]);
  });

  it("regrouping the same messages compares equal item by item", () => {
    const messages = [message(1, "a", 0), message(2, "a", 1)];
    const [first] = groupTranscript(messages, null);
    const [again] = groupTranscript(messages, null);
    expect(first).not.toBe(again);
    expect(sameItem(first, again)).toBe(true);
    expect(sameItem(first, groupTranscript([...messages, message(3, "a", 2)], null)[0])).toBe(false);
  });
});

describe("firstUnreadOf", () => {
  it("skips the reader's own messages", () => {
    const messages = [message(1, "b", 0), message(2, "me", 1), message(3, "b", 2)];
    expect(firstUnreadOf(messages, 1, "me")).toBe(3);
    expect(firstUnreadOf(messages, 3, "me")).toBeNull();
  });
});
