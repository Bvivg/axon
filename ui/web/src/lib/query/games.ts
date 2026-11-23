import { useQuery, useQueryClient } from "@tanstack/react-query";

import { SessionStatus, type Rating, type Session } from "@/gen/axon/game/v1/game_pb";
import { gameClient } from "@/lib/connect/clients";
import { emptyFeed, mergeFeed, type Feed } from "@/lib/games/tv";

export const sessionsKey = ["game", "sessions"] as const;
export const sessionKey = (id: string) => ["game", "session", id] as const;
export { gameHref } from "@/lib/games/catalog";

export function useGameSessions() {
  return useQuery({
    queryKey: sessionsKey,
    queryFn: async ({ signal }) => (await gameClient.listSessions({}, { signal })).sessions,
    refetchOnMount: "always",
  });
}

export const liveSessionsKey = (game: string) => ["game", "live", game] as const;
export const historyKey = (id: string) => ["game", "history", id] as const;
export const openTablesKey = (game: string) => ["game", "open", game] as const;

export function useOpenTables(game: string, enabled = true) {
  return useQuery({
    queryKey: openTablesKey(game),
    queryFn: async ({ signal }) => (await gameClient.listOpenTables({ game }, { signal })).sessions,
    refetchInterval: 5_000,
    enabled,
  });
}

export function useLiveSessions(game: string) {
  return useQuery({
    queryKey: liveSessionsKey(game),
    queryFn: async ({ signal }) => (await gameClient.listLiveSessions({ game }, { signal })).sessions,
    refetchInterval: 15_000,
  });
}

export const liveFeedKey = (user?: string) => ["game", "tv", user ?? ""] as const;

export function useLiveFeed(user?: string) {
  const client = useQueryClient();
  return useQuery({
    queryKey: liveFeedKey(user),
    queryFn: async ({ signal }) => {
      const { sessions } = await gameClient.listLiveSessions({}, { signal });
      return mergeFeed(client.getQueryData<Feed>(liveFeedKey(user)) ?? emptyFeed, sessions, user);
    },
    refetchInterval: 10_000,
    enabled: Boolean(user),
  });
}

export function useSessionHistory(id: string) {
  return useQuery({
    queryKey: historyKey(id),
    queryFn: async ({ signal }) => await gameClient.getSessionHistory({ sessionId: id }, { signal }),
    staleTime: 5_000,
  });
}

export function gameStatusLabel(status: SessionStatus, winner?: string, user?: string) {
  switch (status) {
    case SessionStatus.WAITING: return "Waiting for opponent";
    case SessionStatus.PLAYING: return "In progress";
    case SessionStatus.DRAW: return "Draw";
    case SessionStatus.WON: return winner === user ? "You won" : "You lost";
    default: return "Unavailable";
  }
}

export const ratingsKey = (game?: string, ids: string[] = []) => game ? ["game", "ratings", game, ...ids.toSorted()] : ["game", "ratings"];

export function useRatings(game: string, ids: string[]) {
  const unique = [...new Set(ids.filter(Boolean))];
  return useQuery({
    queryKey: ratingsKey(game, unique),
    queryFn: async ({ signal }) => {
      const { ratings } = await gameClient.getRatings({ game, userIds: unique }, { signal });
      return new Map<string, Rating>(ratings.map(rating => [rating.userId, rating]));
    },
    enabled: unique.length > 0,
    staleTime: 30_000,
  });
}

export function ratingDelta(session: Session, userId?: string): number | undefined {
  const change = session.ratingChanges.find(item => item.userId === userId);
  return change ? change.after - change.before : undefined;
}

export function formatDelta(delta: number): string {
  return delta > 0 ? `+${delta}` : delta < 0 ? `−${Math.abs(delta)}` : "±0";
}
