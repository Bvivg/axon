import { describe, expect, it } from "vitest";

import {
  ChatAddressKind,
  chatHref,
  isRoomAt,
  normalizeChatAddress,
  parseChatAddress,
  roomAddress,
} from "@/lib/chat/address";

const roomID = "0b7e7a52-3c1d-4f0e-9a4b-6d2f1c8e5a90";

describe("normalizeChatAddress", () => {
  it.each([
    { hash: "", want: "" },
    { hash: "#", want: "" },
    { hash: "#bvivg", want: "bvivg" },
    { hash: "#@Bvivg", want: "bvivg" },
    { hash: "#%40Ada_L", want: "ada_l" },
    { hash: `#${roomID.toUpperCase()}`, want: roomID },
    { hash: "#%E0%A4%A", want: "%e0%a4%a" },
  ])("$hash", ({ hash, want }) => {
    expect(normalizeChatAddress(hash)).toBe(want);
  });
});

describe("parseChatAddress", () => {
  it.each([
    { address: "", want: { kind: ChatAddressKind.None } },
    { address: roomID, want: { kind: ChatAddressKind.Room, roomId: roomID } },
    { address: "bvivg", want: { kind: ChatAddressKind.Person, username: "bvivg" } },
    { address: "ada_lovelace_1f2e3d", want: { kind: ChatAddressKind.Person, username: "ada_lovelace_1f2e3d" } },
    { address: "ad", want: { kind: ChatAddressKind.Unknown } },
    { address: "1ada", want: { kind: ChatAddressKind.Unknown } },
    { address: "ada-l", want: { kind: ChatAddressKind.Unknown } },
    { address: "0b7e7a52", want: { kind: ChatAddressKind.Unknown } },
  ])("$address", ({ address, want }) => {
    expect(parseChatAddress(address)).toEqual(want);
  });

  it("reads back every address it builds", () => {
    expect(parseChatAddress(normalizeChatAddress(chatHref("bvivg").split("#")[1]))).toEqual({
      kind: ChatAddressKind.Person,
      username: "bvivg",
    });
    expect(parseChatAddress(normalizeChatAddress(chatHref(roomID).split("#")[1]))).toEqual({
      kind: ChatAddressKind.Room,
      roomId: roomID,
    });
  });
});

describe("chatHref", () => {
  it("points at the chat list without an address", () => {
    expect(chatHref()).toBe("/chat");
    expect(chatHref("")).toBe("/chat");
  });

  it("puts the address into the hash", () => {
    expect(chatHref("bvivg")).toBe("/chat#bvivg");
  });
});

describe("room addresses", () => {
  const room = { id: roomID };

  it("names a direct room after the peer and a group after its id", () => {
    expect(roomAddress(room, { username: "bvivg" })).toBe("bvivg");
    expect(roomAddress(room, {})).toBe(roomID);
    expect(roomAddress(room)).toBe(roomID);
  });

  it("recognises a room by either of its addresses", () => {
    expect(isRoomAt("bvivg", room, { username: "bvivg" })).toBe(true);
    expect(isRoomAt(roomID, room, { username: "bvivg" })).toBe(true);
    expect(isRoomAt("ada", room, { username: "bvivg" })).toBe(false);
    expect(isRoomAt("", room, {})).toBe(false);
  });
});
