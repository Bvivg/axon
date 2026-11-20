import { describe, expect, it } from "vitest";

import { fromSnapshot, isLegal, startPosition } from "@/lib/games/quoridor";
import { chooseQuoridorMove, QuoridorBotLevel } from "@/lib/games/quoridor-bot";

describe("quoridor bot", () => {
  it.each(Object.values(QuoridorBotLevel))("plays a legal opening move on %s", level => {
    const move = chooseQuoridorMove(startPosition(), level, () => 0.5);
    expect(move && isLegal(startPosition(), move)).toBe(true);
  });

  it("steps onto the goal row when it can", () => {
    expect(chooseQuoridorMove(fromSnapshot(["e8", "a5"], [], [10, 10], 0), QuoridorBotLevel.Medium, () => 0.5)).toBe("e9");
  });

  it("blocks an opponent who is one step from winning", () => {
    const position = fromSnapshot(["e5", "e2"], [], [10, 10], 0);
    const move = chooseQuoridorMove(position, QuoridorBotLevel.Medium, () => 0.5);
    expect(move).toMatch(/^[de]1h$/);
  });
});
