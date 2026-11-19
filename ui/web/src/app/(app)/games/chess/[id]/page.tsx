import { ChessGame } from "@/components/games/chess/match";

export default async function ChessGamePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <ChessGame id={id} />;
}
