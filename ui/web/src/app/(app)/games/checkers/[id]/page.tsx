import { CheckersGame } from "@/components/games/checkers/match";

export default async function CheckersGamePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <CheckersGame id={id} />;
}
