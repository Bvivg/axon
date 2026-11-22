import { RequirePermission } from "@/components/admin/admin-ui";
import { RoleEdit } from "@/components/admin/roles/role-detail";
import { Permission } from "@/lib/admin/permissions";

export default async function EditRolePage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequirePermission permission={Permission.RolesMutate}>
      <RoleEdit id={id} />
    </RequirePermission>
  );
}
