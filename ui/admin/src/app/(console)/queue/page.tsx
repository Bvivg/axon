import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { MatchQueue } from "@/components/admin/play/match-queue";
import { Permission } from "@/lib/admin/permissions";

export default function QueuePage() {
  return (
    <RequirePermission permission={Permission.GamesRead}>
      <Suspense><MatchQueue /></Suspense>
    </RequirePermission>
  );
}
