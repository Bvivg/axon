import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { GamesTable } from "@/components/admin/games-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminGamesPage() {
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <Suspense><GamesTable /></Suspense>
    </RequirePermission>
  );
}
