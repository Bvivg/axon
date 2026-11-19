import { Chess } from "chess.js";
import { describe, expect, it } from "vitest";

import { BotLevel, chooseMove } from "@/lib/games/chess-bot";

describe("chess bot", () => {
  it("plays a legal move from the start", () => {
    const move = chooseMove(new Chess().fen(), BotLevel.Hard, () => 0.5);
    expect(new Chess().moves({ verbose: true }).map(m => m.from + m.to)).toContain(move);
  });

  it("finds mate in one on every level above easy", () => {
    const fen = "rnbqkbnr/pppp1ppp/8/4p3/2B1P3/5Q2/PPPP1PPP/RNB1K1NR w KQkq - 0 1";
    for (const level of [BotLevel.Medium, BotLevel.Hard]) expect(chooseMove(fen, level, () => 0.5)).toBe("f3f7");
  });

  it("takes a free queen", () => {
    expect(chooseMove("4k3/8/8/3q4/8/8/8/3QK3 w - - 0 1", BotLevel.Medium, () => 0.5)).toBe("d1d5");
  });

  it("promotes with a legal piece letter", () => {
    const move = chooseMove("7k/4P3/8/8/8/8/8/K7 w - - 0 1", BotLevel.Hard, () => 0.5);
    expect(move).toMatch(/^e7e8[qrbn]$/);
  });

  it("returns nothing when the game is over", () => {
    expect(chooseMove("rnb1kbnr/pppp1ppp/8/4p3/6Pq/5P2/PPPPP2P/RNBQKBNR w KQkq - 1 3", BotLevel.Easy)).toBeNull();
  });
});
