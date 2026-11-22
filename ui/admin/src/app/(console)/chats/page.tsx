import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { ChatsTable } from "@/components/admin/chats-table";
import { Permission } from "@/lib/admin/permissions";

export default function AdminChatsPage() {
  return (
    <RequirePermission permission={Permission.ChatRead}>
      <Suspense><ChatsTable /></Suspense>
    </RequirePermission>
  );
}
