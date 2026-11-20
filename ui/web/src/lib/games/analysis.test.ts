import { create } from "@bufbuild/protobuf";
import { describe, expect, it } from "vitest";

import { MoveClass, PositionAnalysisSchema } from "@/gen/axon/analysis/v1/analysis_pb";
import { evaluationLabel, MoveMark, moveClassLabel, moveMark, moveMarks, sanOf, whiteShare } from "@/lib/games/analysis";

const position = (fields: { centipawns: number; mate?: number; moveClass?: MoveClass }) => create(PositionAnalysisSchema, fields);

describe("evaluationLabel", () => {
  it.each([
    [{ centipawns: 38 }, "+0.4"],
    [{ centipawns: -552 }, "−5.5"],
    [{ centipawns: 0 }, "0.0"],
    [{ centipawns: 10000, mate: 1 }, "+M1"],
    [{ centipawns: -10000, mate: -2 }, "−M2"],
    [{ centipawns: 10000, mate: 0 }, "1-0"],
    [{ centipawns: -10000, mate: 0 }, "0-1"],
  ])("%o reads %s", (evaluation, label) => {
    expect(evaluationLabel(evaluation)).toBe(label);
  });
});

describe("whiteShare", () => {
  it("is even at zero and grows with white's advantage", () => {
    expect(whiteShare({ centipawns: 0 })).toBe(0.5);
    expect(whiteShare({ centipawns: 300 })).toBeGreaterThan(0.7);
    expect(whiteShare({ centipawns: -300 })).toBeLessThan(0.3);
  });

  it("caps centipawns and fills the bar on mate", () => {
    expect(whiteShare({ centipawns: 9000 })).toBe(whiteShare({ centipawns: 1500 }));
    expect(whiteShare({ centipawns: 10000, mate: 1 })).toBe(1);
    expect(whiteShare({ centipawns: -10000, mate: 0 })).toBe(0);
  });
});

describe("move marks", () => {
  it("marks only inaccuracies, mistakes and blunders", () => {
    expect(moveMark(MoveClass.BLUNDER)).toBe(MoveMark.Blunder);
    expect(moveMark(MoveClass.MISTAKE)).toBe(MoveMark.Mistake);
    expect(moveMark(MoveClass.INACCURACY)).toBe(MoveMark.Inaccuracy);
    expect(moveMark(MoveClass.BEST)).toBeUndefined();
    expect(moveMark(MoveClass.GOOD)).toBeUndefined();
    expect(moveClassLabel(MoveClass.BLUNDER)).toBe("Blunder");
    expect(moveClassLabel(MoveClass.UNSPECIFIED)).toBe("");
  });

  it("aligns marks with the moves, skipping the start position", () => {
    const positions = [position({ centipawns: 20 }), position({ centipawns: 30, moveClass: MoveClass.BEST }), position({ centipawns: 400, moveClass: MoveClass.BLUNDER })];
    expect(moveMarks(positions)).toEqual([undefined, MoveMark.Blunder]);
  });
});

describe("sanOf", () => {
  it("writes the engine move in algebraic notation", () => {
    expect(sanOf("r1bqkbnr/pppp1ppp/2n5/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR b KQkq - 3 3", "g7g6")).toBe("g6");
    expect(sanOf("r1bqkb1r/pppp1ppp/2n2n2/4p2Q/2B1P3/8/PPPP1PPP/RNB1K1NR w KQkq - 4 4", "h5f7")).toBe("Qxf7#");
  });

  it("gives up on missing or illegal moves", () => {
    expect(sanOf("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1", "e2e5")).toBeUndefined();
    expect(sanOf("", "e2e4")).toBeUndefined();
    expect(sanOf("rnbqkbnr/pppppppp/8/8/8/8/PPPPPPPP/RNBQKBNR w KQkq - 0 1")).toBeUndefined();
  });
});
