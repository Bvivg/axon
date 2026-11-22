import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { CallsTable } from "@/components/admin/calls-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminCallsPage() {
  return (
    <RequirePermission permission={Permission.ChatRead}>
      <Suspense><CallsTable /></Suspense>
    </RequirePermission>
  );
}
