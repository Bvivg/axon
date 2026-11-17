import { describe, expect, it } from "vitest";

import { aiMove, formatClock, localGameReducer, newGame, outcome, timeLimit, type Cells, type LocalGame } from "./tictactoe";

const move = (game: LocalGame, cell: number, now = game.tick + 100) => localGameReducer(game, { type: "move", cell, mark: game.turn, now });

describe("local tic-tac-toe", () => {
  it("detects wins and draws", () => {
    let game = newGame("local", 0);
    for (const cell of [0, 3, 1, 4, 2]) game = move(game, cell);
    expect(game.result).toEqual({ winner: "X", line: [0, 1, 2] });
    expect(move(game, 8).cells[8]).toBeNull();
    game = newGame("local", 0);
    for (const cell of [0, 1, 2, 4, 3, 5, 7, 6, 8]) game = move(game, cell);
    expect(game.result?.winner).toBe("draw");
  });
  it("settles time before moves and awards timeout to the opponent", () => {
    const game = move(newGame("local", 1000), 0, timeLimit + 1000);
    expect(game.result?.winner).toBe("O");
    expect(game.cells[0]).toBeNull();
    expect(game.left.X).toBe(0);
    expect(localGameReducer(game, { type: "tick", now: 999999 }).left).toEqual(game.left);
  });
  it("charges only the player on turn and handles inactive tabs", () => {
    const game = localGameReducer(move(newGame("local", 1000), 0, 2500), { type: "tick", now: 12_500 });
    expect(game.left).toEqual({ X: 298500, O: 290000 });
    expect(localGameReducer(game, { type: "tick", now: 100 }).left).toEqual(game.left);
  });
  it("undoes one move locally and a full turn against AI, restoring clocks", () => {
    for (const mode of ["local", "hard"] as const) {
      let game = move(move(newGame(mode, 0), 0), 4);
      game = localGameReducer(game, { type: "undo", now: 900 });
      expect(game.cells.filter(Boolean)).toHaveLength(mode === "local" ? 1 : 0);
      expect(game.turn).toBe(mode === "local" ? "O" : "X");
      expect(game.left.X).toBe(299900);
    }
    const pending = localGameReducer(move(newGame("easy", 0), 0), { type: "undo", now: 200 });
    expect(pending.cells.filter(Boolean)).toHaveLength(0);
    expect(pending.turn).toBe("X");
  });
  it("can undo wins and timeouts and resets all state when mode changes", () => {
    let game = newGame("local", 0);
    for (const cell of [0, 3, 1, 4, 2]) game = move(game, cell);
    expect(localGameReducer(game, { type: "undo", now: 1000 }).result).toBeNull();
    game = localGameReducer(move(newGame("easy", 0), 0), { type: "tick", now: 400000 });
    expect(game.result?.winner).toBe("X");
    const undone = localGameReducer(game, { type: "undo", now: 400001 });
    expect(undone.result).toBeNull();
    expect(undone.turn).toBe("X");
    expect(localGameReducer(game, { type: "reset", mode: "hard", now: 500000 })).toEqual(newGame("hard", 500000));
  });
  it("rejects invalid, occupied and out-of-turn moves", () => {
    const game = move(newGame("local", 0), 0);
    for (const cell of [-1, 9, 0, 1.5]) expect(move(game, cell).cells).toEqual(game.cells);
    expect(localGameReducer(game, { type: "move", cell: 1, mark: "X", now: 200 }).cells).toEqual(game.cells);
  });
  it("formats clocks with ceiling and clamps at zero", () => {
    expect([300000, 299001, 1001, 1, 0, -1].map(formatClock)).toEqual(["05:00", "05:00", "00:02", "00:01", "00:00", "00:00"]);
  });
  it("easy AI can pick random moves; hard AI blocks and takes wins without mutating input", () => {
    const board: Cells = ["X", "X", null, "O", null, null, null, null, null];
    const original = [...board];
    expect(aiMove(board, "hard", () => 0)).toBe(2);
    expect(aiMove(board, "easy", () => 0)).toBe(2);
    expect(aiMove(["O", "O", null, "X", "X", null, "X", null, null], "hard", () => 0)).toBe(2);
    expect(board).toEqual(original);
  });
  it("hard AI cannot lose against any sequence of human moves", () => {
    function explore(cells: Cells) {
      const result = outcome(cells);
      if (result) { expect(result.winner).not.toBe("X"); return; }
      for (let i = 0; i < 9; i++) {
        if (cells[i]) continue;
        const next = [...cells];
        next[i] = "X";
        const result = outcome(next);
        if (result) { expect(result.winner).not.toBe("X"); continue; }
        const reply = aiMove(next, "hard", () => 0);
        expect(reply).not.toBeNull();
        next[reply!] = "O";
        explore(next);
      }
    }
    explore(Array(9).fill(null));
  });
});
