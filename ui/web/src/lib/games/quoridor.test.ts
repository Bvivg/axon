import { describe, expect, it } from "vitest";

import { canPlace, cellName, distance, fromSnapshot, goalRow, isLegal, legalMoves, pawnMoves, play, startPosition, winner } from "@/lib/games/quoridor";

const at = (white: string, black: string, walls: string[] = [], turn: 0 | 1 = 0) => fromSnapshot([white, black], walls, [10, 10], turn);

describe("quoridor rules", () => {
  it.each([
    ["opening", at("e1", "e9"), 0, ["d1", "f1", "e2"]],
    ["straight jump", at("e5", "e6"), 0, ["e4", "d5", "f5", "e7"]],
    ["wall behind the opponent gives diagonals", at("e5", "e6", ["e6h"]), 0, ["e4", "d5", "f5", "d6", "f6"]],
    ["board edge gives diagonals", at("e8", "e9"), 0, ["e7", "d8", "f8", "d9", "f9"]],
    ["diagonal closed by a wall", at("e5", "e6", ["e6h", "d6v"]), 0, ["e4", "d5", "f5", "f6"]],
    ["sideways jump", at("e5", "f5"), 0, ["e4", "d5", "g5", "e6"]],
    ["wall covers the second cell", at("e1", "e9", ["d1h"]), 0, ["d1", "f1"]],
  ] as const)("pawn moves: %s", (_, position, seat, want) => {
    expect(pawnMoves(position, seat).map(cellName)).toEqual(want);
  });

  it.each([
    ["e3h", false], ["d3h", false], ["f3h", false], ["e3v", false], ["c3h", true], ["g3h", true], ["e2v", true], ["e4v", true],
  ] as const)("wall %s next to e3h legal = %s", (wall, want) => {
    expect(canPlace(at("e1", "e9", ["e3h"]), 0, wall)).toBe(want);
  });

  it("never seals a path", () => {
    const sealed = at("e1", "e9", ["a1h", "c1h", "e1h", "g1h"]);
    expect(canPlace(sealed, 0, "h2v")).toBe(true);
    expect(canPlace(sealed, 0, "h1v")).toBe(false);
    expect(canPlace(at("e1", "a9", ["a8h"]), 0, "b8v")).toBe(false);
  });

  it("measures distances like the server", () => {
    expect(distance(new Set(), startPosition().pawns[0], goalRow(0))).toBe(8);
    expect(distance(new Set(["d1h"]), at("e1", "e9").pawns[0], goalRow(0))).toBe(9);
    expect(distance(new Set(["a1h", "c1h", "e1h", "g1h"]), at("e1", "e9").pawns[0], goalRow(0))).toBe(12);
  });

  it("counts every opening move", () => {
    expect(legalMoves(startPosition())).toHaveLength(3 + 128);
  });

  it("spends walls, passes the turn and finds the winner", () => {
    const next = play(startPosition(), "e8h");
    expect(next.left).toEqual([9, 10]);
    expect(next.turn).toBe(1);
    expect(isLegal(next, "e8")).toBe(false);
    expect(winner(play(at("e8", "a9"), "e9"))).toBe(0);
    expect(winner(play(at("a5", "c2", [], 1), "c1"))).toBe(1);
  });

  it("refuses walls once they run out", () => {
    const empty = fromSnapshot(["e1", "e9"], [], [0, 10], 0);
    expect(canPlace(empty, 0, "a1h")).toBe(false);
    expect(legalMoves(empty)).toEqual(["d1", "f1", "e2"]);
  });
});
