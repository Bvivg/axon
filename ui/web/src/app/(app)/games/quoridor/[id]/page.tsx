import { QuoridorGame } from "@/components/games/quoridor/match";

export default async function QuoridorGamePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <QuoridorGame id={id} />;
}
