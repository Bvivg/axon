import { chooseMove, type BotLevel } from "@/lib/games/chess-bot";

export type BotRequest = { id: number; fen: string; level: BotLevel };
export type BotReply = { id: number; move: string | null };

self.onmessage = (event: MessageEvent<BotRequest>) => {
  const { id, fen, level } = event.data;
  self.postMessage({ id, move: chooseMove(fen, level) } satisfies BotReply);
};
