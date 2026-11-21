import { DurakGame } from "@/components/games/durak/match";

export default async function DurakGamePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <DurakGame id={id} />;
}
