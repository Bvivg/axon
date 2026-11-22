import { SessionStatus, type Session } from "@/gen/axon/game/v1/game_pb";

export const feedLimit = 3;

export type Feed = {
  order: string[];
  sessions: Map<string, Session>;
};

export const emptyFeed: Feed = { order: [], sessions: new Map() };

export const revisionOf = (game: Session) => game.revision ?? game.ply;

export function latest(...candidates: (Session | undefined)[]): Session | undefined {
  return candidates.reduce<Session | undefined>((best, item) => !item ? best : !best || revisionOf(item) >= revisionOf(best) ? item : best, undefined);
}

export function mergeFeed(feed: Feed, live: readonly Session[], me?: string): Feed {
  const order = [...feed.order];
  const sessions = new Map(feed.sessions);
  for (const item of live) {
    if (me && item.playerIds.includes(me)) continue;
    if (!sessions.has(item.id)) order.push(item.id);
    sessions.set(item.id, latest(sessions.get(item.id), item)!);
  }
  return { order, sessions };
}

export function watchWindow(order: readonly string[], active: number): string[] {
  return [active, active + 1, active - 1].filter(index => index >= 0 && index < order.length).slice(0, feedLimit).map(index => order[index]!);
}

export const finished = (game: Session) => game.status === SessionStatus.WON || game.status === SessionStatus.DRAW;

export function nextLive(order: readonly string[], sessions: (id: string) => Session | undefined, from: number): number {
  for (let index = from + 1; index < order.length; index++) {
    const game = sessions(order[index]!);
    if (!game || !finished(game)) return index;
  }
  return -1;
}
