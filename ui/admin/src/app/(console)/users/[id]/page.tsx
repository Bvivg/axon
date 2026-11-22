import { RequirePermission } from "@/components/admin/admin-ui";
import { UserDetail } from "@/components/admin/user-detail";
import { Permission } from "@/lib/admin/permissions";

export default async function AdminUserPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequirePermission permission={Permission.UsersRead}>
      <UserDetail id={id} />
    </RequirePermission>
  );
}
