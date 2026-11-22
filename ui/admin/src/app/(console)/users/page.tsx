import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { UsersTable } from "@/components/admin/users-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminUsersPage() {
  return (
    <RequirePermission permission={Permission.UsersRead}>
      <Suspense><UsersTable /></Suspense>
    </RequirePermission>
  );
}
