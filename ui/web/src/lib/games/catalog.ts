export enum GameKey {
  TicTacToe = "tictactoe",
  Chess = "chess",
  Quoridor = "quoridor",
}

type GameInfo = { title: string; slug: string };

export const games: Record<GameKey, GameInfo> = {
  [GameKey.TicTacToe]: { title: "Tic-Tac-Toe", slug: "tic-tac-toe" },
  [GameKey.Chess]: { title: "Chess", slug: "chess" },
  [GameKey.Quoridor]: { title: "Quoridor", slug: "quoridor" },
};

export const gameKeys = Object.values(GameKey);

export function gameKey(value: string): GameKey {
  return gameKeys.find(key => key === value) ?? GameKey.TicTacToe;
}

export const lobbyHref = (game: GameKey) => `/games/${games[game].slug}`;
export const practiceHref = (game: GameKey) => `${lobbyHref(game)}/practice`;
export const gameHref = (id: string, game: string) => `${lobbyHref(gameKey(game))}/${id}`;
