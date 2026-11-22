import { RequirePermission } from "@/components/admin/admin-ui";
import { GameDetail } from "@/components/admin/play/game-detail";
import { Permission } from "@/lib/admin/permissions";

export default async function GamePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <GameDetail id={id} />
    </RequirePermission>
  );
}
