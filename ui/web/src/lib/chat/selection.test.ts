import { describe, expect, it } from "vitest";

import { ContentKind } from "@/lib/chat/content";
import {
  canSelect,
  forwardRequest,
  isPicked,
  selectableBetween,
  selectedCount,
  selectedMessages,
  selectionText,
  selectWhole,
  toggleItem,
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
    expect([...(one?.keys() ?? [])]).toEqual(["m1"]);
    expect(toggleMessage(one, "m1")).toBeNull();
  });

  it("a drag adds every selectable message between two rows in either direction", () => {
    const messages = [message(1), message(2, { kind: ContentKind.System }), message(3), message(4)];
    expect(selectableBetween(messages, 4, 1)).toEqual(["m1", "m3", "m4"]);
    const base = toggleMessage(null, "m9");
    expect([...(selectWhole(base, ["m1", "m3"])?.keys() ?? [])].sort()).toEqual(["m1", "m3", "m9"]);
  });

  it("selected messages follow the transcript order and drop what got deleted", () => {
    const messages = [message(1), message(2, { deleted_at: at }), message(3)];
    const picked = new Map<string, null>([["m3", null], ["m2", null], ["m1", null]]);
    expect(selectedMessages(picked, messages).map((m) => m.id)).toEqual(["m1", "m3"]);
  });

  it("album items are picked one by one and counted as photos", () => {
    const album = message(5, {
      kind: ContentKind.Album,
      payload: {
        items: [0, 1, 2].map((i) => ({ kind: "image", url: `u${i}`, width: 1, height: 1, size_bytes: 1 })),
      },
    });
    const messages = [message(1), album];

    let selection = toggleItem(null, album.id, 1, 3);
    expect(isPicked(selection, album.id, 1)).toBe(true);
    expect(isPicked(selection, album.id, 0)).toBe(false);
    selection = toggleMessage(selection, "m1");
    expect(selectedCount(selection, messages)).toBe(2);
    expect(forwardRequest(selection, messages)).toEqual({
      messageIds: ["m1", album.id],
      parts: [{ messageId: album.id, positions: [1] }],
    });

    selection = toggleItem(toggleItem(selection, album.id, 0, 3), album.id, 2, 3);
    expect(selection?.get(album.id)).toBeNull();
    expect(selectedCount(selection, messages)).toBe(4);
    expect(forwardRequest(selection, messages).parts).toEqual([]);

    selection = toggleItem(selection, album.id, 0, 3);
    selection = toggleItem(toggleItem(selection, album.id, 1, 3), album.id, 2, 3);
    expect(selection?.has(album.id)).toBe(false);
  });

  it("copies one message as its text and several with author and time", () => {
    expect(selectionText([message(1)], names)).toBe("text 1");
    const text = selectionText([message(1), message(2)], names);
    expect(text.split("\n\n")).toHaveLength(2);
    expect(text).toMatch(/^Ada, .+\ntext 1\n\nBob, .+\ntext 2$/);
  });
});
