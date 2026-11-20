import { GameReplay } from "@/components/games/replay";
import { GameKey } from "@/lib/games/catalog";

export default async function TicTacToeReplayPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <GameReplay id={id} game={GameKey.TicTacToe} />;
}
