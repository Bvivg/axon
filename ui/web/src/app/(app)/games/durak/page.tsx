import { GameLobby } from "@/components/games/lobby";
import { GameKey } from "@/lib/games/catalog";

export default async function DurakLobbyPage({ searchParams }: { searchParams: Promise<{ play?: string }> }) {
  const { play } = await searchParams;
  return <GameLobby game={GameKey.Durak} quick={play === "quick"} />;
}
