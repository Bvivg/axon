import { RequirePermission } from "@/components/admin/admin-ui";
import { RoleDetail } from "@/components/admin/roles/role-detail";
import { Permission } from "@/lib/admin/permissions";

export default async function RolePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequirePermission permission={Permission.RolesRead}>
      <RoleDetail id={id} />
    </RequirePermission>
  );
}
