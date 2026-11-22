import { RequirePermission } from "@/components/admin/admin-ui";
import { RolesList } from "@/components/admin/roles/roles-list";
import { Permission } from "@/lib/admin/permissions";

export default function AdminRolesPage() {
  return (
    <RequirePermission permission={Permission.RolesRead}>
      <RolesList />
    </RequirePermission>
  );
}
