export enum GameKey {
  TicTacToe = "tictactoe",
  Chess = "chess",
  Quoridor = "quoridor",
  Checkers = "checkers",
  Durak = "durak",
}

type GameInfo = { title: string; slug: string };

export const games: Record<GameKey, GameInfo> = {
  [GameKey.TicTacToe]: { title: "Tic-Tac-Toe", slug: "tic-tac-toe" },
  [GameKey.Chess]: { title: "Chess", slug: "chess" },
  [GameKey.Quoridor]: { title: "Quoridor", slug: "quoridor" },
  [GameKey.Checkers]: { title: "Checkers", slug: "checkers" },
  [GameKey.Durak]: { title: "Durak", slug: "durak" },
};

export const gameKeys = Object.values(GameKey);

export const tournamentGameKeys = gameKeys.filter(key => key !== GameKey.Durak);

export function gameKey(value: string): GameKey {
  return gameKeys.find(key => key === value) ?? GameKey.TicTacToe;
}

export const lobbyHref = (game: GameKey) => `/games/${games[game].slug}`;
export const practiceHref = (game: GameKey) => `${lobbyHref(game)}/practice`;
export const gameHref = (id: string, game: string) => `${lobbyHref(gameKey(game))}/${id}`;
export const replayHref = (id: string, game: string) => `${gameHref(id, game)}/replay`;
export const tournamentHref = (id: string) => `/games/tournaments/${id}`;
