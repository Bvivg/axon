import { describe, expect, it } from "vitest";

import { defaultDurakSettings, deckFits, describeDurak, durakOptions } from "@/components/games/durak/table-settings";
import { DurakMode, DurakThrowIn } from "@/gen/axon/game/v1/game_pb";

describe("durak table settings", () => {
  it("fits six cards per seat into the deck", () => {
    expect(deckFits(4, 24)).toBe(true);
    expect(deckFits(5, 24)).toBe(false);
    expect(deckFits(6, 36)).toBe(true);
    expect(deckFits(6, 52)).toBe(true);
  });

  it("maps settings to game options", () => {
    const options = durakOptions({ ...defaultDurakSettings, seats: 4, mode: DurakMode.TRANSFER, deck: 52, throwIn: DurakThrowIn.NEIGHBORS, cheats: true, draw: false, firstBoutFive: true });
    expect(options.game.case).toBe("durak");
    expect(options.game.value).toMatchObject({ mode: DurakMode.TRANSFER, deck: 52, throwIn: DurakThrowIn.NEIGHBORS, cheats: true, draw: false, firstBoutLimit: 5 });
  });

  it("describes a table in one line", () => {
    expect(describeDurak({ seats: 3, mode: DurakMode.TRANSFER, cheats: false, deck: 36 })).toBe("3 players · transfer · 36 cards");
    expect(describeDurak({ seats: 2, mode: DurakMode.THROW_IN, cheats: true, deck: 24 })).toBe("2 players · throw-in · 24 cards · cheating");
  });
});
