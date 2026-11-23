import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import { SessionSchema, SessionStatus } from "@/gen/axon/game/v1/game_pb";
import { emptyFeed, latest, mergeFeed, nextLive, watchWindow } from "@/lib/games/tv";

const game = (id: string, revision: number, players = ["a", "b"], status = SessionStatus.PLAYING) =>
  create(SessionSchema, { id, game: "chess", playerIds: players, revision, ply: revision, status });

describe("mergeFeed", () => {
  it("keeps the order it already showed and appends new games at the end", () => {
    const first = mergeFeed(emptyFeed, [game("2", 1), game("1", 1)]);
    const second = mergeFeed(first, [game("3", 1), game("1", 4)]);
    expect(second.order).toEqual(["2", "1", "3"]);
    expect(second.sessions.get("1")?.revision).toBe(4);
    expect(second.sessions.get("2")?.revision).toBe(1);
  });

  it("never rolls a game back to an older revision", () => {
    const fresh = mergeFeed(emptyFeed, [game("1", 7)]);
    expect(mergeFeed(fresh, [game("1", 3)]).sessions.get("1")?.revision).toBe(7);
  });

  it("leaves out the viewer's own games", () => {
    expect(mergeFeed(emptyFeed, [game("1", 1, ["me", "b"]), game("2", 1)], "me").order).toEqual(["2"]);
  });
});

describe("watchWindow", () => {
  it("watches the visible game first, then its neighbours", () => {
    expect(watchWindow(["a", "b", "c", "d"], 1)).toEqual(["b", "c", "a"]);
    expect(watchWindow(["a", "b"], 0)).toEqual(["a", "b"]);
    expect(watchWindow(["a"], 0)).toEqual(["a"]);
    expect(watchWindow([], 0)).toEqual([]);
  });
});

describe("nextLive", () => {
  it("skips games that already ended", () => {
    const sessions = new Map([["a", game("a", 1)], ["b", game("b", 1, ["a", "b"], SessionStatus.WON)], ["c", game("c", 1)]]);
    expect(nextLive(["a", "b", "c"], id => sessions.get(id), 0)).toBe(2);
    expect(nextLive(["a", "b", "c"], id => sessions.get(id), 2)).toBe(-1);
  });
});

describe("latest", () => {
  it("prefers the newest revision", () => {
    expect(latest(game("a", 2), undefined, game("a", 5))?.revision).toBe(5);
    expect(latest(undefined)).toBeUndefined();
  });
});
