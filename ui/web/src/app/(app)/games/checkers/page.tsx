import { GameLobby } from "@/components/games/lobby";
import { GameKey } from "@/lib/games/catalog";

export default async function CheckersLobbyPage({ searchParams }: { searchParams: Promise<{ play?: string }> }) {
  const { play } = await searchParams;
  return <GameLobby game={GameKey.Checkers} quick={play === "quick"} />;
}
