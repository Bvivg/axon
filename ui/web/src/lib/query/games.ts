export const sessionsKey = ["game", "sessions"] as const;
export const sessionKey = (id: string) => ["game", "session", id] as const;
export const gameHref = (id: string) => `/games/tic-tac-toe/${id}`;
