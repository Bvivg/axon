import { GameLobby } from "@/components/games/tictactoe/lobby";

export default async function TicTacToeLobbyPage({ searchParams }: { searchParams: Promise<{ play?: string }> }) {
  const { play } = await searchParams;
  return <GameLobby quick={play === "quick"} />;
}
