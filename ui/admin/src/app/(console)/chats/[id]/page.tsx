import { RequirePermission } from "@/components/admin/admin-ui";
import { ChatDetail } from "@/components/admin/chat-detail";
import { Permission } from "@/lib/admin/permissions";

export default async function AdminChatPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <RequirePermission permission={Permission.ChatRead}>
      <ChatDetail id={id} />
    </RequirePermission>
  );
}
