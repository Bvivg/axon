import { Suspense } from "react";

import { RequirePermission } from "@/components/admin/admin-ui";
import { ChatUploads } from "@/components/admin/play/chat-uploads";
import { Permission } from "@/lib/admin/permissions";

export default function UploadsPage() {
  return (
    <RequirePermission permission={Permission.ChatRead}>
      <Suspense><ChatUploads /></Suspense>
    </RequirePermission>
  );
}
