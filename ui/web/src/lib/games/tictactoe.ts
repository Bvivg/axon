export type LocalMark = "X" | "O";
export type LocalMode = "easy" | "medium" | "hard" | "local";
export type Cells = (LocalMark | null)[];
export const timeLimit = 300_000;
const lines = [[0, 1, 2], [3, 4, 5], [6, 7, 8], [0, 3, 6], [1, 4, 7], [2, 5, 8], [0, 4, 8], [2, 4, 6]];

export function outcome(cells: Cells): { winner: LocalMark | "draw"; line: number[] } | null {
  for (const line of lines) {
    const [a, b, c] = line;
    if (cells[a] && cells[a] === cells[b] && cells[a] === cells[c]) return { winner: cells[a], line };
  }
  return cells.every(Boolean) ? { winner: "draw", line: [] } : null;
}

function minimax(cells: Cells, turn: LocalMark, depth: number, alpha: number, beta: number): number {
  const result = outcome(cells);
  if (result) return result.winner === "O" ? 10 - depth : result.winner === "X" ? depth - 10 : 0;
  let best = turn === "O" ? -Infinity : Infinity;
  for (let i = 0; i < cells.length; i++) {
    if (cells[i]) continue;
    cells[i] = turn;
    const score = minimax(cells, turn === "O" ? "X" : "O", depth + 1, alpha, beta);
    cells[i] = null;
    best = turn === "O" ? Math.max(best, score) : Math.min(best, score);
    if (turn === "O") alpha = Math.max(alpha, best);
    else beta = Math.min(beta, best);
    if (beta <= alpha) break;
  }
  return best;
}

export function aiMove(board: Cells, mode: LocalMode, random = Math.random, mark: LocalMark = "O"): number | null {
  if (mark === "X") return aiMove(board.map(cell => cell === "X" ? "O" : cell === "O" ? "X" : null), mode, random);
  if (outcome(board)) return null;
  const free = board.flatMap((mark, i) => mark ? [] : [i]);
  if (mode === "easy" && random() < .55 || mode === "medium" && random() < .25) return free[Math.floor(random() * free.length)];
  const cells = [...board];
  let best = -Infinity;
  let choices: number[] = [];
  for (const i of free) {
    cells[i] = "O";
    const score = minimax(cells, "X", 0, -Infinity, Infinity);
    cells[i] = null;
    if (score > best) { best = score; choices = [i]; }
    else if (score === best) choices.push(i);
  }
  return choices[Math.floor(random() * choices.length)];
}

type Snapshot = { cells: Cells; turn: LocalMark; left: Record<LocalMark, number> };
export type LocalGame = Snapshot & {
  mode: LocalMode;
  clock?: { started: boolean; paused: boolean; human: LocalMark };
  tick: number;
  result: ReturnType<typeof outcome>;
  history: Snapshot[];
};
export type Action = { type: "tick"; now: number } | { type: "undo"; now: number }
  | { type: "move"; cell: number; mark: LocalMark; now: number }
  | { type: "reset"; mode: LocalMode; now: number; options?: ClockOptions }
  | { type: "pause"; paused: boolean; now: number };

type ClockOptions = { duration: number; human: LocalMark };

export function newGame(mode: LocalMode, now: number, options?: ClockOptions): LocalGame {
  const duration = options?.duration ?? timeLimit;
  return { ...(options ? { clock: { started: false, paused: false, human: options.human } } : {}), mode, cells: Array(9).fill(null), turn: "X", left: { X: duration, O: duration }, tick: now, result: null, history: [] };
}

function settle(state: LocalGame, now: number): LocalGame {
  if (state.result) return state;
  if (state.clock && (!state.clock.started || state.clock.paused)) return { ...state, tick: now };
  const remaining = Math.max(0, state.left[state.turn] - Math.max(0, now - state.tick));
  return { ...state, tick: Math.max(now, state.tick), left: { ...state.left, [state.turn]: remaining },
    result: remaining === 0 ? { winner: state.turn === "X" ? "O" : "X", line: [] } : null };
}

export function localGameReducer(previous: LocalGame, action: Action): LocalGame {
  if (action.type === "reset") return newGame(action.mode, action.now, action.options);
  const state = settle(previous, action.now);
  if (action.type === "pause") return { ...state, clock: { started: state.clock?.started ?? true, human: state.clock?.human ?? "X", paused: action.paused } };
  if (action.type === "tick") return state;
  if (action.type === "undo") {
    if (!state.history.length || state.clock && state.result) return state;
    const human = state.clock?.human ?? "X";
    const count = state.mode === "local" || state.turn !== human ? 1 : 2;
    const at = Math.max(0, state.history.length - count);
    return { ...state, ...state.history[at], tick: action.now, result: null, history: state.history.slice(0, at), ...(state.clock ? { clock: { ...state.clock, started: at > 0 } } : {}) };
  }
  if (state.clock?.paused || state.result || action.mark !== state.turn || !Number.isInteger(action.cell) || action.cell < 0 || action.cell > 8 || state.cells[action.cell]) return state;
  const cells = [...state.cells];
  cells[action.cell] = state.turn;
  return { ...state, ...(state.clock ? { clock: { ...state.clock, started: true } } : {}), cells, turn: state.turn === "X" ? "O" : "X", result: outcome(cells),
    history: [...state.history, { cells: state.cells, turn: state.turn, left: state.left }] };
}

export function formatClock(ms: number) {
  const seconds = Math.ceil(Math.max(0, ms) / 1000);
  return `${String(Math.floor(seconds / 60)).padStart(2, "0")}:${String(seconds % 60).padStart(2, "0")}`;
}
