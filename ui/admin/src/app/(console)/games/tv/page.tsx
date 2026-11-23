import { RequirePermission } from "@/components/admin/admin-ui";
import { LiveTv } from "@/components/admin/play/live-tv";
import { Permission } from "@/lib/admin/permissions";

export default function AdminTvPage() {
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <LiveTv />
    </RequirePermission>
  );
}
