import { GameMatch } from "@/components/games/tictactoe/match";

export default async function TicTacToePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <GameMatch id={id} />;
}
