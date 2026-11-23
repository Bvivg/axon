"use client";

import { CheckersBoard, seatNames as checkersSeats } from "@/components/games/checkers/board";
import { ChessBoard } from "@/components/games/chess/board";
import { Side } from "@/components/games/chess/pieces";
import { DurakTable, seatNames as durakSeats } from "@/components/games/durak/board";
import { QuoridorBoard, seatNames as quoridorSeats } from "@/components/games/quoridor/board";
import { Board } from "@/components/games/tictactoe/board";
import type { HistoryFrame } from "@/gen/axon/game/v1/game_pb";
import { boardFrom } from "@/lib/games/checkers";
import { GameKey } from "@/lib/games/catalog";

export type BoardFrame = Pick<HistoryFrame, "ticTacToe" | "chess" | "quoridor" | "checkers" | "durak">;

export const sides: Record<GameKey, string[]> = {
  [GameKey.TicTacToe]: ["X", "O"],
  [GameKey.Chess]: ["White", "Black"],
  [GameKey.Quoridor]: [...quoridorSeats],
  [GameKey.Checkers]: [...checkersSeats],
  [GameKey.Durak]: [...durakSeats],
};

const idle = () => undefined;

function changedCell(frame: BoardFrame, previous?: BoardFrame) {
  if (!previous) return undefined;
  const before = previous.ticTacToe?.cells ?? [];
  const index = frame.ticTacToe?.cells.findIndex((cell, at) => cell !== before[at]) ?? -1;
  return index < 0 ? undefined : index;
}

export function Position({ game, frame, previous }: { game: GameKey; frame: BoardFrame; previous?: BoardFrame }) {
  switch (game) {
    case GameKey.Chess:
      return <ChessBoard fen={frame.chess?.fen ?? ""} orientation={Side.White} legal={[]} lastMove={frame.chess?.lastMove} check={frame.chess?.check ?? false} playable={false} onMove={idle} />;
    case GameKey.Quoridor:
      return <QuoridorBoard pawns={frame.quoridor?.pawns ?? ["e1", "e9"]} walls={frame.quoridor?.walls ?? []} seat={0} legal={[]} lastMove={frame.quoridor?.lastMove} playable={false} onMove={idle} />;
    case GameKey.Checkers:
      return <CheckersBoard squares={boardFrom(frame.checkers?.squares)} seat={1} legal={[]} lastMove={frame.checkers?.lastMove} playable={false} onMove={idle} />;
    case GameKey.Durak:
      return <DurakTable view={frame.durak} seat={0} legal={[]} playable={false} onMove={idle} />;
    default:
      return <Board board={frame.ticTacToe} playable={false} move={idle} lastMove={changedCell(frame, previous)} />;
  }
}
