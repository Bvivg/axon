import { describe, expect, it } from "vitest";

import { ContentKind } from "@/lib/chat/content";
import {
  canSelect,
  selectableBetween,
  selectedMessages,
  selectionText,
  selectWhole,
  toggleMessage,
} from "@/lib/chat/selection";
import type { WireMessage } from "@/lib/ws/protocol";

const at = new Date(2026, 9, 1, 9, 30).toISOString();

function message(seq: number, extra: Partial<WireMessage> = {}): WireMessage {
  return { id: `m${seq}`, room_id: "r", author_id: seq % 2 ? "ada" : "bob", body: `text ${seq}`, seq, sent_at: at, ...extra };
}

const names = (id?: string) => (id === "ada" ? "Ada" : "Bob");

describe("selection", () => {
  it("system lines and deleted messages cannot be selected", () => {
    expect(canSelect(message(1))).toBe(true);
    expect(canSelect(message(2, { kind: ContentKind.System }))).toBe(false);
    expect(canSelect(message(3, { deleted_at: at }))).toBe(false);
  });

  it("toggling the last message off ends the selection", () => {
    const one = toggleMessage(null, "m1");
    expect([...(one ?? [])]).toEqual(["m1"]);
    expect(toggleMessage(one, "m1")).toBeNull();
  });

  it("a drag adds every selectable message between two rows in either direction", () => {
    const messages = [message(1), message(2, { kind: ContentKind.System }), message(3), message(4)];
    expect(selectableBetween(messages, 4, 1)).toEqual(["m1", "m3", "m4"]);
    const base = toggleMessage(null, "m9");
    expect([...(selectWhole(base, ["m1", "m3"]) ?? [])].sort()).toEqual(["m1", "m3", "m9"]);
  });

  it("selected messages follow the transcript order and drop what got deleted", () => {
    const messages = [message(1), message(2, { deleted_at: at }), message(3)];
    const picked = new Set(["m3", "m2", "m1"]);
    expect(selectedMessages(picked, messages).map((m) => m.id)).toEqual(["m1", "m3"]);
  });

  it("copies one message as its text and several with author and time", () => {
    expect(selectionText([message(1)], names)).toBe("text 1");
    const text = selectionText([message(1), message(2)], names);
    expect(text.split("\n\n")).toHaveLength(2);
    expect(text).toMatch(/^Ada, .+\ntext 1\n\nBob, .+\ntext 2$/);
  });
});
