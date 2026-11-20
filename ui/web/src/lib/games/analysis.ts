import { Chess } from "chess.js";

import { MoveClass, type PositionAnalysis } from "@/gen/axon/analysis/v1/analysis_pb";

type Evaluation = Pick<PositionAnalysis, "centipawns" | "mate">;

export enum MoveMark {
  Inaccuracy = "?!",
  Mistake = "?",
  Blunder = "??",
}

const marks: Partial<Record<MoveClass, MoveMark>> = {
  [MoveClass.INACCURACY]: MoveMark.Inaccuracy,
  [MoveClass.MISTAKE]: MoveMark.Mistake,
  [MoveClass.BLUNDER]: MoveMark.Blunder,
};

const labels: Partial<Record<MoveClass, string>> = {
  [MoveClass.BEST]: "Best move",
  [MoveClass.GOOD]: "Good move",
  [MoveClass.INACCURACY]: "Inaccuracy",
  [MoveClass.MISTAKE]: "Mistake",
  [MoveClass.BLUNDER]: "Blunder",
};

export const moveMark = (moveClass: MoveClass) => marks[moveClass];

export const markClass = (mark?: MoveMark) => mark === MoveMark.Blunder ? "text-game-error" : "text-game-warning";

export const moveClassLabel = (moveClass: MoveClass) => labels[moveClass] ?? "";

export function evaluationLabel({ centipawns, mate }: Evaluation) {
  if (mate !== undefined) {
    if (mate === 0) return centipawns > 0 ? "1-0" : "0-1";
    return `${mate > 0 ? "+" : "−"}M${Math.abs(mate)}`;
  }
  if (centipawns === 0) return "0.0";
  return `${centipawns > 0 ? "+" : "−"}${(Math.abs(centipawns) / 100).toFixed(1)}`;
}

const winSlope = 0.00368208;

export function whiteShare({ centipawns, mate }: Evaluation) {
  if (mate !== undefined) return centipawns > 0 ? 1 : 0;
  const clamped = Math.max(-1500, Math.min(1500, centipawns));
  return 1 / (1 + Math.exp(-winSlope * clamped));
}

export function moveMarks(positions: PositionAnalysis[]) {
  return positions.slice(1).map(position => moveMark(position.moveClass));
}

export function sanOf(fen: string, uci?: string) {
  if (!fen || !uci || uci.length < 4) return undefined;
  try {
    return new Chess(fen).move({ from: uci.slice(0, 2), to: uci.slice(2, 4), promotion: uci[4] }).san;
  } catch {
    return undefined;
  }
}
