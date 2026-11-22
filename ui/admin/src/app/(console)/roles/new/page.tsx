import { RequirePermission } from "@/components/admin/admin-ui";
import { RoleCreate } from "@/components/admin/roles/role-detail";
import { Permission } from "@/lib/admin/permissions";

export default function NewRolePage() {
  return (
    <RequirePermission permission={Permission.RolesMutate}>
      <RoleCreate />
    </RequirePermission>
  );
}
