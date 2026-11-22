import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { TournamentsTable } from "@/components/admin/tournaments-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminTournamentsPage() {
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <Suspense><TournamentsTable /></Suspense>
    </RequirePermission>
  );
}
