import { fromSnapshot } from "@/lib/games/quoridor";
import { chooseQuoridorMove, type QuoridorBotLevel } from "@/lib/games/quoridor-bot";

export type QuoridorBotRequest = { id: number; pawns: string[]; walls: string[]; left: number[]; turn: 0 | 1; level: QuoridorBotLevel };
export type QuoridorBotReply = { id: number; move: string | null };

self.onmessage = (event: MessageEvent<QuoridorBotRequest>) => {
  const { id, pawns, walls, left, turn, level } = event.data;
  self.postMessage({ id, move: chooseQuoridorMove(fromSnapshot(pawns, walls, left, turn), level) ?? null } satisfies QuoridorBotReply);
};
