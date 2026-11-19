import { GameLobby } from "@/components/games/lobby";
import { GameKey } from "@/lib/games/catalog";

export default async function TicTacToeLobbyPage({ searchParams }: { searchParams: Promise<{ play?: string }> }) {
  const { play } = await searchParams;
  return <GameLobby game={GameKey.TicTacToe} quick={play === "quick"} />;
}
