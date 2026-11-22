import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { AuditTable } from "@/components/admin/audit-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminAuditPage() {
  return (
    <RequirePermission permission={Permission.AuditRead}>
      <Suspense><AuditTable /></Suspense>
    </RequirePermission>
  );
}
