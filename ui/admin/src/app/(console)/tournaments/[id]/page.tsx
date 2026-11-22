import { RequirePermission } from "@/components/admin/admin-ui";
import { TournamentDetail } from "@/components/admin/tournaments/tournament-detail";
import { Permission } from "@/lib/admin/permissions";

export default async function TournamentPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <TournamentDetail id={id} />
    </RequirePermission>
  );
}
