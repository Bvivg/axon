import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { RatingsTable } from "@/components/admin/ratings-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminRatingsPage() {
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <Suspense><RatingsTable /></Suspense>
    </RequirePermission>
  );
}
